package pte

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

type Engine struct {
	rawTemplateRoot   string
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
	return func(engine *Engine) {
		engine.suffix = suffix
	}
}

func WithMinify(minify bool) EngineOption {
	return func(engine *Engine) {
		engine.minify = minify
	}
}

func WithPrettify(prettify bool) EngineOption {
	return func(engine *Engine) {
		engine.prettify = prettify
	}
}

func WithInMemoryTemplates(templates map[string]string) EngineOption {
	return func(engine *Engine) {
		if templates != nil {
			for templateName, templateSource := range templates {
				engine.includedTemplates[engine.normalizeTemplateName(templateName)] = templateSource
			}
		}
	}
}

func WithFS(filesystem fs.FS) EngineOption {
	return func(engine *Engine) {
		engine.fsys = filesystem
	}
}

func NewEngine(templateRoot string, options ...EngineOption) *Engine {
	rawRoot := templateRoot
	root := templateRoot
	if root == "" {
		root = "pte-templates"
		rawRoot = root
	}
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		absRoot = root
	}

	engine := &Engine{
		rawTemplateRoot:   rawRoot,
		templateRoot:      absRoot,
		suffix:            ".pte",
		includedTemplates: make(map[string]string),
		lexer:             NewLexer(),
		parser:            NewParser(),
	}

	for _, option := range options {
		option(engine)
	}

	return engine
}

func (engine *Engine) Compile(template string) (*CompiledTemplate, error) {
	if cachedVal, isCached := engine.cache.Load(template); isCached {
		return cachedVal.(*CompiledTemplate), nil
	}

	tokens, err := engine.lexer.Tokenize(template)
	if err != nil {
		return nil, err
	}

	compiled, err := engine.parser.Parse(tokens)
	if err != nil {
		return nil, err
	}

	engine.cache.Store(template, compiled)
	return compiled, nil
}

func (engine *Engine) CompileTemplate(templateOrTemplateName string) (*CompiledTemplate, error) {
	source, err := engine.loadTemplateSource(templateOrTemplateName)
	if err != nil {
		return nil, err
	}
	return engine.Compile(source)
}

func (engine *Engine) Render(writer io.Writer, templateOrTemplateName string, values map[string]any) error {
	context := NewContext(values)
	context.PushLocal("_engine", engine)

	var buffer bytes.Buffer
	if engine.isTemplateReference(templateOrTemplateName) {
		if err := engine.renderNamedTemplate(&buffer, templateOrTemplateName, context); err != nil {
			return err
		}
	} else {
		compiled, err := engine.Compile(templateOrTemplateName)
		if err != nil {
			return err
		}
		if err := compiled.RootNode.Render(context, &buffer); err != nil {
			return err
		}
	}

	result := buffer.String()
	if engine.minify {
		result = MinifyHTML(result)
	} else if engine.prettify {
		result = PrettifyHTML(result)
	}

	_, err := io.WriteString(writer, result)
	return err
}

func (engine *Engine) RenderString(writer io.Writer, template string, values map[string]any) error {
	context := NewContext(values)
	context.PushLocal("_engine", engine)

	compiled, err := engine.Compile(template)
	if err != nil {
		return err
	}

	var buffer bytes.Buffer
	if err := compiled.RootNode.Render(context, &buffer); err != nil {
		return err
	}

	result := buffer.String()
	if engine.minify {
		result = MinifyHTML(result)
	} else if engine.prettify {
		result = PrettifyHTML(result)
	}

	_, err = io.WriteString(writer, result)
	return err
}

func (engine *Engine) renderRawFragment(buffer *bytes.Buffer, templateOrTemplateName, fragmentName string, values map[string]any) error {
	source, err := engine.loadTemplateSource(templateOrTemplateName)
	if err != nil {
		return err
	}

	compiled, err := engine.Compile(source)
	if err != nil {
		return err
	}

	fragNode := engine.findFragmentNode(compiled.RootNode, fragmentName)
	if fragNode == nil {
		return fmt.Errorf("fragment %q not found in template", fragmentName)
	}

	context := NewContext(values)
	context.PushLocal("_engine", engine)

	return fragNode.Render(context, buffer)
}

func (engine *Engine) RenderFragment(writer io.Writer, templateOrTemplateName, fragmentName string, values map[string]any) error {
	var buffer bytes.Buffer
	if err := engine.renderRawFragment(&buffer, templateOrTemplateName, fragmentName, values); err != nil {
		return err
	}

	result := buffer.String()
	if engine.minify {
		result = MinifyHTML(result)
	} else if engine.prettify {
		result = PrettifyHTML(result)
	}

	_, err := io.WriteString(writer, result)
	return err
}

// Streaming/Goroutine API

func (engine *Engine) RenderStream(name string, data map[string]any) io.Reader {
	pipeReader, pipeWriter := io.Pipe()
	go func() {
		err := engine.Render(pipeWriter, name, data)
		_ = pipeWriter.CloseWithError(err)
	}()
	return pipeReader
}

func (engine *Engine) RenderStringStream(templateSource string, data map[string]any) io.Reader {
	pipeReader, pipeWriter := io.Pipe()
	go func() {
		err := engine.RenderString(pipeWriter, templateSource, data)
		_ = pipeWriter.CloseWithError(err)
	}()
	return pipeReader
}

func (engine *Engine) RenderFragmentStream(name, fragment string, data map[string]any) io.Reader {
	pipeReader, pipeWriter := io.Pipe()
	go func() {
		err := engine.RenderFragment(pipeWriter, name, fragment, data)
		_ = pipeWriter.CloseWithError(err)
	}()
	return pipeReader
}

func (engine *Engine) RenderFragments(writer io.Writer, name string, fragmentNames []string, data map[string]any) error {
	var combinedBuffer bytes.Buffer
	for _, fragName := range fragmentNames {
		if err := engine.renderRawFragment(&combinedBuffer, name, fragName, data); err != nil {
			return err
		}
	}

	result := combinedBuffer.String()
	if engine.minify {
		result = MinifyHTML(result)
	} else if engine.prettify {
		result = PrettifyHTML(result)
	}

	_, err := io.WriteString(writer, result)
	return err
}

func (engine *Engine) RenderFragmentsStream(name string, fragmentNames []string, data map[string]any) io.Reader {
	pipeReader, pipeWriter := io.Pipe()
	go func() {
		err := engine.RenderFragments(pipeWriter, name, fragmentNames, data)
		_ = pipeWriter.CloseWithError(err)
	}()
	return pipeReader
}

// Internal methods used by nodes

func (engine *Engine) renderNamedTemplate(writer io.Writer, templateName string, context *Context) error {
	normalizedName := engine.normalizeTemplateName(templateName)

	var stack []string
	if stackObj := context.Get("_templateStack"); stackObj != nil {
		stack = stackObj.([]string)
	}

	for _, stackEntry := range stack {
		if stackEntry == normalizedName {
			return fmt.Errorf("circular include detected: %s -> %s", strings.Join(stack, " -> "), normalizedName)
		}
	}

	newStack := append([]string(nil), stack...)
	newStack = append(newStack, normalizedName)
	subCtx := context.With("_templateStack", newStack)

	compiled, err := engine.CompileTemplate(normalizedName)
	if err != nil {
		return err
	}

	return compiled.RootNode.Render(subCtx, writer)
}

func (engine *Engine) loadTemplateSource(templateOrTemplateName string) (string, error) {
	if engine.isTemplateReference(templateOrTemplateName) {
		normalized := engine.normalizeTemplateName(templateOrTemplateName)
		if source, isFound := engine.includedTemplates[normalized]; isFound {
			return source, nil
		}

		if engine.fsys != nil {
			relPath := normalized + engine.suffix
			relPath = strings.TrimPrefix(relPath, "/")

			if strings.HasPrefix(normalized, "/") || strings.HasPrefix(normalized, "\\") || !fs.ValidPath(relPath) {
				return "", fmt.Errorf("template name must not escape template root: %s", normalized)
			}

			pathsToTry := []string{relPath}
			if engine.rawTemplateRoot != "" && engine.rawTemplateRoot != "." {
				rootClean := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(engine.rawTemplateRoot)), "/")
				if rootClean != "" && rootClean != "." {
					combined := path.Join(rootClean, relPath)
					if fs.ValidPath(combined) {
						pathsToTry = append([]string{combined}, relPath)
					}
				}
			}

			for _, pathCandidate := range pathsToTry {
				data, err := fs.ReadFile(engine.fsys, pathCandidate)
				if err == nil {
					return string(data), nil
				}
			}
		}

		filePath, err := engine.resolveTemplatePath(normalized)
		if err != nil {
			return "", err
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to load template %q: %w", normalized, err)
		}
		return string(data), nil
	}
	return templateOrTemplateName, nil
}

func (engine *Engine) isTemplateReference(value string) bool {
	return !strings.Contains(value, "|") &&
		!strings.Contains(value, "\n") &&
		!strings.Contains(value, "<")
}

func (engine *Engine) normalizeTemplateName(name string) string {
	normalized := strings.TrimSpace(name)
	normalized = strings.ReplaceAll(normalized, "\\", "/")
	if strings.HasSuffix(normalized, engine.suffix) {
		normalized = normalized[:len(normalized)-len(engine.suffix)]
	}
	return normalized
}

func (engine *Engine) resolveTemplatePath(name string) (string, error) {
	relPath := name + engine.suffix
	if filepath.IsAbs(relPath) || strings.HasPrefix(relPath, "/") || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("template name must not escape template root: %s", name)
	}

	// 1. Lexical containment check
	resolved := filepath.Join(engine.templateRoot, relPath)
	cleanResolved, err := filepath.Abs(filepath.Clean(resolved))
	if err != nil {
		return "", err
	}

	cleanRoot, err := filepath.Abs(filepath.Clean(engine.templateRoot))
	if err != nil {
		cleanRoot = engine.templateRoot
	}

	withinLexical, err := isPathWithinRoot(cleanRoot, cleanResolved)
	if err != nil || !withinLexical {
		return "", fmt.Errorf("template name must not escape template root: %s", name)
	}

	// 2. Symlink / Canonical containment check
	// Lexical containment checks alone are insufficient because symbolic links inside
	// the template root can point to files or directories outside the template root.
	evalRoot, err := filepath.EvalSymlinks(cleanRoot)
	if err != nil {
		evalRoot = cleanRoot
	}

	evalTarget, err := filepath.EvalSymlinks(cleanResolved)
	if err != nil {
		// Target file does not exist (or intermediate components missing).
		// Check if the parent directory exists and escapes root via symlink.
		dir := filepath.Dir(cleanResolved)
		if evalDir, dirErr := filepath.EvalSymlinks(dir); dirErr == nil {
			withinDir, relErr := isPathWithinRoot(evalRoot, evalDir)
			if relErr != nil || !withinDir {
				return "", fmt.Errorf("template name must not escape template root: %s", name)
			}
		}
		// File is missing within valid root bounds
		return cleanResolved, nil
	}

	withinCanonical, err := isPathWithinRoot(evalRoot, evalTarget)
	if err != nil || !withinCanonical {
		return "", fmt.Errorf("template name must not escape template root: %s", name)
	}

	return evalTarget, nil
}

func isPathWithinRoot(root, target string) (bool, error) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false, err
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.HasPrefix(rel, "../") {
		return false, nil
	}

	return true, nil
}

func (engine *Engine) createContextFromValue(value any) *Context {
	if value == nil {
		return NewContext(nil)
	}

	if mapVal, isMap := value.(map[string]any); isMap {
		return NewContext(mapVal)
	}

	reflectVal := reflect.ValueOf(value)
	for reflectVal.Kind() == reflect.Ptr || reflectVal.Kind() == reflect.Interface {
		if reflectVal.IsNil() {
			return NewContext(nil)
		}
		reflectVal = reflectVal.Elem()
	}

	if reflectVal.Kind() == reflect.Map {
		modelMap := make(map[string]any)
		for _, key := range reflectVal.MapKeys() {
			modelMap[fmt.Sprintf("%v", key.Interface())] = reflectVal.MapIndex(key).Interface()
		}
		return NewContext(modelMap)
	}

	return NewContext(map[string]any{"it": value})
}

func (engine *Engine) findFragmentNode(node Node, name string) Node {
	if node == nil {
		return nil
	}

	if layout, ok := node.(*LayoutNode); ok {
		for _, sec := range layout.Sections {
			if found := engine.findFragmentNode(sec, name); found != nil {
				return found
			}
		}
	}

	if frag, ok := node.(*FragmentNode); ok {
		if frag.Name == name {
			return frag
		}
		if found := engine.findFragmentNode(frag.Body, name); found != nil {
			return found
		}
	}

	if block, ok := node.(*BlockNode); ok {
		for _, child := range block.Children {
			if found := engine.findFragmentNode(child, name); found != nil {
				return found
			}
		}
	}

	if ifNode, ok := node.(*IfNode); ok {
		if found := engine.findFragmentNode(ifNode.ThenBlock, name); found != nil {
			return found
		}
		for _, branch := range ifNode.ElseIfBranches {
			if found := engine.findFragmentNode(branch.Block, name); found != nil {
				return found
			}
		}
		if ifNode.ElseBlock != nil {
			if found := engine.findFragmentNode(ifNode.ElseBlock, name); found != nil {
				return found
			}
		}
	}

	if eachNode, ok := node.(*EachNode); ok {
		if found := engine.findFragmentNode(eachNode.BodyBlock, name); found != nil {
			return found
		}
		if eachNode.SeparatorNode != nil {
			if found := engine.findFragmentNode(eachNode.SeparatorNode, name); found != nil {
				return found
			}
		}
	}

	if attemptNode, ok := node.(*AttemptNode); ok {
		if found := engine.findFragmentNode(attemptNode.Body, name); found != nil {
			return found
		}
		if attemptNode.RecoverBlock != nil {
			if found := engine.findFragmentNode(attemptNode.RecoverBlock, name); found != nil {
				return found
			}
		}
	}

	return nil
}
