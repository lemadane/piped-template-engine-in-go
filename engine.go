package pte

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

type Engine struct {
	templateRoot      string
	suffix            string
	minify            bool
	prettify          bool
	cache             sync.Map
	includedTemplates map[string]string
	lexer             *Lexer
	parser            *Parser
	fsys              fs.FS
}

type EngineOption func(*Engine)

func WithSuffix(suffix string) EngineOption {
	return func(e *Engine) {
		e.suffix = suffix
	}
}

func WithMinify(minify bool) EngineOption {
	return func(e *Engine) {
		e.minify = minify
	}
}

func WithPrettify(prettify bool) EngineOption {
	return func(e *Engine) {
		e.prettify = prettify
	}
}

func WithInMemoryTemplates(templates map[string]string) EngineOption {
	return func(e *Engine) {
		if templates != nil {
			for k, v := range templates {
				e.includedTemplates[e.normalizeTemplateName(k)] = v
			}
		}
	}
}

func WithFS(fsys fs.FS) EngineOption {
	return func(e *Engine) {
		e.fsys = fsys
	}
}

func NewEngine(templateRoot string, opts ...EngineOption) *Engine {
	root := templateRoot
	if root == "" {
		root = "pte-templates"
	}
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		absRoot = root
	}

	e := &Engine{
		templateRoot:      absRoot,
		suffix:            ".pte",
		includedTemplates: make(map[string]string),
		lexer:             NewLexer(),
		parser:            NewParser(),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

func (e *Engine) Compile(template string) (*CompiledTemplate, error) {
	if val, ok := e.cache.Load(template); ok {
		return val.(*CompiledTemplate), nil
	}

	tokens, err := e.lexer.Tokenize(template)
	if err != nil {
		return nil, err
	}

	compiled, err := e.parser.Parse(tokens)
	if err != nil {
		return nil, err
	}

	e.cache.Store(template, compiled)
	return compiled, nil
}

func (e *Engine) CompileTemplate(templateOrTemplateName string) (*CompiledTemplate, error) {
	source, err := e.loadTemplateSource(templateOrTemplateName)
	if err != nil {
		return nil, err
	}
	return e.Compile(source)
}

func (e *Engine) Render(w io.Writer, templateOrTemplateName string, values map[string]any) error {
	ctx := NewContext(values)
	ctx.PushLocal("_engine", e)

	var buf bytes.Buffer
	if e.isTemplateReference(templateOrTemplateName) {
		if err := e.renderNamedTemplate(&buf, templateOrTemplateName, ctx); err != nil {
			return err
		}
	} else {
		compiled, err := e.Compile(templateOrTemplateName)
		if err != nil {
			return err
		}
		if err := compiled.RootNode.Render(ctx, &buf); err != nil {
			return err
		}
	}

	result := buf.String()
	if e.minify {
		result = MinifyHTML(result)
	} else if e.prettify {
		result = PrettifyHTML(result)
	}

	_, err := io.WriteString(w, result)
	return err
}

func (e *Engine) RenderString(w io.Writer, template string, values map[string]any) error {
	return e.Render(w, template, values)
}

func (e *Engine) RenderFragment(w io.Writer, templateOrTemplateName, fragmentName string, values map[string]any) error {
	source, err := e.loadTemplateSource(templateOrTemplateName)
	if err != nil {
		return err
	}

	compiled, err := e.Compile(source)
	if err != nil {
		return err
	}

	fragNode := e.findFragmentNode(compiled.RootNode, fragmentName)
	if fragNode == nil {
		return fmt.Errorf("fragment %q not found in template", fragmentName)
	}

	ctx := NewContext(values)
	ctx.PushLocal("_engine", e)

	return fragNode.Render(ctx, w)
}

// Streaming/Goroutine API

func (e *Engine) RenderStream(name string, data map[string]any) io.Reader {
	r, w := io.Pipe()
	go func() {
		err := e.Render(w, name, data)
		_ = w.CloseWithError(err)
	}()
	return r
}

func (e *Engine) RenderStringStream(templateSource string, data map[string]any) io.Reader {
	r, w := io.Pipe()
	go func() {
		err := e.RenderString(w, templateSource, data)
		_ = w.CloseWithError(err)
	}()
	return r
}

func (e *Engine) RenderFragmentStream(name, fragment string, data map[string]any) io.Reader {
	r, w := io.Pipe()
	go func() {
		err := e.RenderFragment(w, name, fragment, data)
		_ = w.CloseWithError(err)
	}()
	return r
}

func (e *Engine) RenderFragments(w io.Writer, name string, fragmentNames []string, data map[string]any) error {
	for _, fragName := range fragmentNames {
		if err := e.RenderFragment(w, name, fragName, data); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) RenderFragmentsStream(name string, fragmentNames []string, data map[string]any) io.Reader {
	r, w := io.Pipe()
	go func() {
		err := e.RenderFragments(w, name, fragmentNames, data)
		_ = w.CloseWithError(err)
	}()
	return r
}

// Internal methods used by nodes

func (e *Engine) renderNamedTemplate(w io.Writer, templateName string, ctx *Context) error {
	normalizedName := e.normalizeTemplateName(templateName)

	var stack []string
	if stackObj := ctx.Get("_templateStack"); stackObj != nil {
		stack = stackObj.([]string)
	}

	for _, s := range stack {
		if s == normalizedName {
			return fmt.Errorf("circular include detected: %s -> %s", strings.Join(stack, " -> "), normalizedName)
		}
	}

	newStack := append([]string(nil), stack...)
	newStack = append(newStack, normalizedName)
	subCtx := ctx.With("_templateStack", newStack)

	compiled, err := e.CompileTemplate(normalizedName)
	if err != nil {
		return err
	}

	return compiled.RootNode.Render(subCtx, w)
}

func (e *Engine) loadTemplateSource(templateOrTemplateName string) (string, error) {
	if e.isTemplateReference(templateOrTemplateName) {
		normalized := e.normalizeTemplateName(templateOrTemplateName)
		if source, ok := e.includedTemplates[normalized]; ok {
			return source, nil
		}

		if e.fsys != nil {
			relPath := normalized + e.suffix
			relPath = strings.TrimPrefix(relPath, "/")
			data, err := fs.ReadFile(e.fsys, relPath)
			if err == nil {
				return string(data), nil
			}
		}

		path, err := e.resolveTemplatePath(normalized)
		if err != nil {
			return "", err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("failed to load template %q: %w", normalized, err)
		}
		return string(data), nil
	}
	return templateOrTemplateName, nil
}

func (e *Engine) isTemplateReference(value string) bool {
	return !strings.Contains(value, "|") &&
		!strings.Contains(value, "\n") &&
		!strings.Contains(value, "<")
}

func (e *Engine) normalizeTemplateName(name string) string {
	normalized := strings.TrimSpace(name)
	normalized = strings.ReplaceAll(normalized, "\\", "/")
	if strings.HasSuffix(normalized, e.suffix) {
		normalized = normalized[:len(normalized)-len(e.suffix)]
	}
	return normalized
}

func (e *Engine) resolveTemplatePath(name string) (string, error) {
	relPath := name + e.suffix
	if filepath.IsAbs(relPath) || strings.HasPrefix(relPath, "/") {
		return "", fmt.Errorf("template name must not be absolute: %s", name)
	}

	// Avoid escaping templateRoot
	resolved := filepath.Join(e.templateRoot, relPath)
	cleanResolved, err := filepath.Abs(filepath.Clean(resolved))
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(cleanResolved, e.templateRoot) {
		return "", fmt.Errorf("template name must not escape template root: %s", name)
	}

	return cleanResolved, nil
}

func (e *Engine) createContextFromValue(value any) *Context {
	if value == nil {
		return NewContext(nil)
	}

	if m, ok := value.(map[string]any); ok {
		return NewContext(m)
	}

	val := reflect.ValueOf(value)
	for val.Kind() == reflect.Ptr || val.Kind() == reflect.Interface {
		if val.IsNil() {
			return NewContext(nil)
		}
		val = val.Elem()
	}

	if val.Kind() == reflect.Map {
		m := make(map[string]any)
		for _, key := range val.MapKeys() {
			m[fmt.Sprintf("%v", key.Interface())] = val.MapIndex(key).Interface()
		}
		return NewContext(m)
	}

	return NewContext(map[string]any{"it": value})
}

func (e *Engine) findFragmentNode(node Node, name string) Node {
	if node == nil {
		return nil
	}

	if frag, ok := node.(*FragmentNode); ok {
		if frag.Name == name {
			return frag
		}
		if found := e.findFragmentNode(frag.Body, name); found != nil {
			return found
		}
	}

	if block, ok := node.(*BlockNode); ok {
		for _, child := range block.Children {
			if found := e.findFragmentNode(child, name); found != nil {
				return found
			}
		}
	}

	if ifNode, ok := node.(*IfNode); ok {
		if found := e.findFragmentNode(ifNode.ThenBlock, name); found != nil {
			return found
		}
		for _, branch := range ifNode.ElseIfBranches {
			if found := e.findFragmentNode(branch.Block, name); found != nil {
				return found
			}
		}
		if ifNode.ElseBlock != nil {
			if found := e.findFragmentNode(ifNode.ElseBlock, name); found != nil {
				return found
			}
		}
	}

	if eachNode, ok := node.(*EachNode); ok {
		if found := e.findFragmentNode(eachNode.BodyBlock, name); found != nil {
			return found
		}
		if eachNode.SeparatorNode != nil {
			if found := e.findFragmentNode(eachNode.SeparatorNode, name); found != nil {
				return found
			}
		}
	}

	if attemptNode, ok := node.(*AttemptNode); ok {
		if found := e.findFragmentNode(attemptNode.Body, name); found != nil {
			return found
		}
		if attemptNode.RecoverBlock != nil {
			if found := e.findFragmentNode(attemptNode.RecoverBlock, name); found != nil {
				return found
			}
		}
	}

	return nil
}
