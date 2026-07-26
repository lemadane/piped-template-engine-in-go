package pte

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"regexp"
	"strings"
)

type Node interface {
	Render(ctx *Context, w io.Writer) error
}

var errConditionalAttributeSkipped = errors.New("conditional attribute skipped")

// BlockNode holds a list of child nodes
type BlockNode struct {
	Children []Node
}

func (n *BlockNode) Render(ctx *Context, w io.Writer) error {
	for i, child := range n.Children {
		err := child.Render(ctx, w)
		if err == errConditionalAttributeSkipped {
			if buf, ok := w.(*bytes.Buffer); ok {
				data := buf.Bytes()
				for len(data) > 0 && isWhitespaceChar(data[len(data)-1]) {
					data = data[:len(data)-1]
				}
				buf.Reset()
				buf.Write(data)
			}

			if i+1 < len(n.Children) {
				if nextTextNode, ok := n.Children[i+1].(*TextNode); ok {
					text := string(nextTextNode.Value)
					trimmed := strings.TrimLeft(text, " \t\r\n")
					if strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "/>") {
						nextTextNode.Value = []byte(trimmed)
					}
				}
			}
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// TextNode renders plain HTML/text
type TextNode struct {
	Value []byte
}

func NewTextNode(val string) *TextNode {
	return &TextNode{Value: []byte(val)}
}

func (n *TextNode) Render(ctx *Context, w io.Writer) error {
	_, err := w.Write(n.Value)
	return err
}

// Escaping Modes
type OutputMode string

const (
	ModeHtmlEscaped      OutputMode = "HTML_ESCAPED"
	ModeTrustedHtml      OutputMode = "TRUSTED_HTML"
	ModeAttributeEscaped OutputMode = "ATTRIBUTE_ESCAPED"
	ModeJsonEncoded      OutputMode = "JSON_ENCODED"
	ModeUrlEncoded       OutputMode = "URL_ENCODED"
)

// ExpressionNode evaluates and escapes an expression
type ExpressionNode struct {
	Expression string
	Mode       OutputMode
	Evaluator  *Evaluator
	Condition  string
}

func (n *ExpressionNode) Render(ctx *Context, w io.Writer) error {
	if n.Condition != "" {
		cond, err := n.Evaluator.EvaluateBoolean(n.Condition, ctx)
		if err != nil {
			return err
		}
		if !cond {
			if n.Mode == ModeAttributeEscaped {
				return errConditionalAttributeSkipped
			}
			return nil
		}

		if n.Mode == ModeAttributeEscaped {
			attrOutput, err := n.renderConditionalAttributeOutput(n.Expression, ctx)
			if err != nil {
				return err
			}
			if attrOutput != "" {
				_, err = io.WriteString(w, attrOutput)
				return err
			}
		}
	}

	val, err := n.Evaluator.Evaluate(n.Expression, ctx)
	if err != nil {
		return err
	}

	formatted := n.formatValue(val)
	_, err = io.WriteString(w, formatted)
	return err
}

func (n *ExpressionNode) renderConditionalAttributeOutput(expression string, ctx *Context) (string, error) {
	trimmedExpression := strings.TrimSpace(expression)

	if isConditionalAttributeLiteral(trimmedExpression) {
		return attributeEscape(trimmedExpression), nil
	}

	equalsIndex := findTopLevelEqualsIndex(trimmedExpression)
	if equalsIndex == -1 {
		return "", nil
	}

	attributeName := strings.TrimSpace(trimmedExpression[:equalsIndex])
	valueExpression := strings.TrimSpace(trimmedExpression[equalsIndex+1:])

	if !isValidAttributeName(attributeName) {
		return "", nil
	}

	if valueExpression == "" {
		return "", fmt.Errorf("conditional attribute value must not be empty")
	}

	value, err := n.Evaluator.Evaluate(valueExpression, ctx)
	if err != nil {
		return "", err
	}

	return attributeName + "=\"" + attributeEscape(value) + "\"", nil
}

func isConditionalAttributeLiteral(expr string) bool {
	return isValidAttributeName(expr) && !strings.Contains(expr, "=")
}

var attrNameRegex = regexp.MustCompile(`^[A-Za-z_:][A-Za-z0-9_:.\-]*$`)

func isValidAttributeName(name string) bool {
	return attrNameRegex.MatchString(name)
}

func findTopLevelEqualsIndex(expression string) int {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0

	for index := 0; index < len(expression); index++ {
		current := expression[index]

		if current == '\'' && !insideDoubleQuote {
			insideSingleQuote = !insideSingleQuote
			continue
		}
		if current == '"' && !insideSingleQuote {
			insideDoubleQuote = !insideDoubleQuote
			continue
		}
		if insideSingleQuote || insideDoubleQuote {
			continue
		}

		if current == '(' {
			parenthesisDepth++
			continue
		}
		if current == ')' {
			parenthesisDepth--
			continue
		}

		if parenthesisDepth == 0 && current == '=' {
			return index
		}
	}
	return -1
}

func (n *ExpressionNode) formatValue(value any) string {
	if value == nil {
		return ""
	}

	switch n.Mode {
	case ModeHtmlEscaped:
		return htmlEscape(value)
	case ModeTrustedHtml:
		return fmt.Sprintf("%v", value)
	case ModeAttributeEscaped:
		return attributeEscape(value)
	case ModeJsonEncoded:
		data, err := json.Marshal(value)
		if err != nil {
			return "null"
		}
		return string(data)
	case ModeUrlEncoded:
		return url.QueryEscape(fmt.Sprintf("%v", value))
	default:
		return htmlEscape(value)
	}
}

func htmlEscape(val any) string {
	s := fmt.Sprintf("%v", val)
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		case '\'':
			sb.WriteString("&#039;")
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

func attributeEscape(val any) string {
	s := fmt.Sprintf("%v", val)
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		case '\'':
			sb.WriteString("&#039;")
		case '`':
			sb.WriteString("&#096;")
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// ElseIfBranch represents a single else-if conditional branch
type ElseIfBranch struct {
	Condition string
	Block     Node
}

// IfNode renders conditional blocks
type IfNode struct {
	Condition      string
	ThenBlock      Node
	ElseIfBranches []ElseIfBranch
	ElseBlock      Node
	Evaluator      *Evaluator
}

func (n *IfNode) Render(ctx *Context, w io.Writer) error {
	cond, err := n.Evaluator.EvaluateBoolean(n.Condition, ctx)
	if err != nil {
		return err
	}
	if cond {
		return n.ThenBlock.Render(ctx, w)
	}

	for _, branch := range n.ElseIfBranches {
		bCond, err := n.Evaluator.EvaluateBoolean(branch.Condition, ctx)
		if err != nil {
			return err
		}
		if bCond {
			return branch.Block.Render(ctx, w)
		}
	}

	if n.ElseBlock != nil {
		return n.ElseBlock.Render(ctx, w)
	}
	return nil
}

// EachNode renders loops
type EachNode struct {
	ItemName             string
	KeyName              string
	ValueName            string
	CollectionExpression string
	BodyBlock            Node
	ElseBlock            Node
	SeparatorNode        Node
	Evaluator            *Evaluator
	IsMapLoop            bool
}

func (n *EachNode) Render(ctx *Context, w io.Writer) error {
	rawVal, err := n.Evaluator.Evaluate(n.CollectionExpression, ctx)
	if err != nil {
		return err
	}

	items, isMap, total := n.toIterable(rawVal)
	if total > 0 {
		for i, item := range items {
			isLast := (i == total-1)
			loopMeta := map[string]any{
				"index": i,
				"count": i + 1,
				"first": i == 0,
				"last":  isLast,
				"total": total,
			}

			scope := make(map[string]any)
			if isMap && n.IsMapLoop {
				entry := item.(map[string]any)
				scope[n.KeyName] = entry["key"]
				scope[n.ValueName] = entry["value"]
			} else if isMap {
				// Map treated as list of entries if not explicit map loop
				entry := item.(map[string]any)
				scope[n.ItemName] = entry
			} else {
				scope[n.ItemName] = item
			}
			scope["each"] = loopMeta

			subContext := ctx.SubContext(scope)
			if err := n.BodyBlock.Render(subContext, w); err != nil {
				return err
			}

			if n.SeparatorNode != nil && !isLast {
				if err := n.SeparatorNode.Render(subContext, w); err != nil {
					return err
				}
			}
		}
	} else if n.ElseBlock != nil {
		return n.ElseBlock.Render(ctx, w)
	}

	return nil
}

func (n *EachNode) toIterable(value any) ([]any, bool, int) {
	if value == nil {
		return nil, false, 0
	}

	val := reflect.ValueOf(value)
	for val.Kind() == reflect.Ptr || val.Kind() == reflect.Interface {
		if val.IsNil() {
			return nil, false, 0
		}
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Slice, reflect.Array:
		length := val.Len()
		items := make([]any, length)
		for i := 0; i < length; i++ {
			items[i] = val.Index(i).Interface()
		}
		return items, false, length

	case reflect.Map:
		length := val.Len()
		items := make([]any, 0, length)
		for _, key := range val.MapKeys() {
			items = append(items, map[string]any{
				"key":   key.Interface(),
				"value": val.MapIndex(key).Interface(),
			})
		}
		return items, true, length
	}

	return []any{value}, false, 1
}

// CaseBlock represents a switch case branch
type CaseBlock struct {
	Expression  string
	Body        Node
	Fallthrough bool
}

// SwitchNode renders switch structures
type SwitchNode struct {
	Expression   string
	Cases        []CaseBlock
	DefaultBlock Node
	Evaluator    *Evaluator
}

func (n *SwitchNode) Render(ctx *Context, w io.Writer) error {
	switchVal, err := n.Evaluator.Evaluate(n.Expression, ctx)
	if err != nil {
		return err
	}

	matched := false
	shouldFallthrough := false

	for _, caseBlock := range n.Cases {
		caseVal, err := n.Evaluator.Evaluate(caseBlock.Expression, ctx)
		if err != nil {
			return err
		}

		caseMatches := shouldFallthrough || n.Evaluator.ValuesEqual(switchVal, caseVal)
		if !caseMatches {
			continue
		}

		matched = true
		if err := caseBlock.Body.Render(ctx, w); err != nil {
			return err
		}

		if !caseBlock.Fallthrough {
			return nil
		}
		shouldFallthrough = true
	}

	if (shouldFallthrough || !matched) && n.DefaultBlock != nil {
		return n.DefaultBlock.Render(ctx, w)
	}

	return nil
}

// IncludeNode represents |include template with data|
type IncludeNode struct {
	TemplateName    string
	ModelExpression string
	Evaluator       *Evaluator
}

func (n *IncludeNode) Render(ctx *Context, w io.Writer) error {
	engineObj := ctx.Get("_engine")
	if engineObj == nil {
		return fmt.Errorf("PTE template engine not found in context")
	}

	// We can assert or dynamically call Render Named Template on the engine
	// Let's use reflection or type assert if the type is accessible (yes, it is inside package pte!)
	engine, ok := engineObj.(interface {
		renderNamedTemplate(w io.Writer, name string, ctx *Context) error
		createContextFromValue(value any) *Context
	})
	if !ok {
		return fmt.Errorf("invalid template engine in context")
	}

	subContext := ctx
	if n.ModelExpression != "" {
		val, err := n.Evaluator.Evaluate(n.ModelExpression, ctx)
		if err != nil {
			return err
		}
		subContext = engine.createContextFromValue(val)
		// Inherit local variables like macros and slots
		for k, v := range ctx.localValues {
			subContext.PushLocal(k, v)
		}
	}

	return engine.renderNamedTemplate(w, n.TemplateName, subContext)
}

// YieldNode renders layouts yields
type YieldNode struct {
	SectionName string
}

func (n *YieldNode) Render(ctx *Context, w io.Writer) error {
	sectionsObj := ctx.Get("_sections")
	if sectionsObj == nil {
		return fmt.Errorf("|yield| can only be used inside a layout template")
	}

	sections, ok := sectionsObj.(map[string]string)
	if !ok {
		return fmt.Errorf("invalid layout sections in context")
	}

	val := sections[n.SectionName]
	_, err := io.WriteString(w, val)
	return err
}

// ComponentNode renders component blocks
type ComponentNode struct {
	ComponentName string
	Slots         map[string]Node
}

func (n *ComponentNode) Render(ctx *Context, w io.Writer) error {
	engineObj := ctx.Get("_engine")
	if engineObj == nil {
		return fmt.Errorf("PTE template engine not found in context")
	}

	engine, ok := engineObj.(interface {
		renderNamedTemplate(w io.Writer, name string, ctx *Context) error
	})
	if !ok {
		return fmt.Errorf("invalid template engine in context")
	}

	// Evaluate slots to string buffers in component context
	slotValues := make(map[string]string)
	for slotName, slotBlock := range n.Slots {
		var buf bytes.Buffer
		if err := slotBlock.Render(ctx, &buf); err != nil {
			return err
		}
		slotValues[slotName] = buf.String()
	}

	// Create subcontext for the component rendering, pushing slot values
	subContext := ctx.With("_slots", slotValues)
	return engine.renderNamedTemplate(w, n.ComponentName, subContext)
}

// SlotNode renders component slots
type SlotNode struct {
	SlotName string
}

func (n *SlotNode) Render(ctx *Context, w io.Writer) error {
	slotsObj := ctx.Get("_slots")
	if slotsObj == nil {
		return fmt.Errorf("|slot| can only be rendered inside a component template")
	}

	slots, ok := slotsObj.(map[string]string)
	if !ok {
		return fmt.Errorf("invalid component slots in context")
	}

	val := slots[n.SlotName]
	_, err := io.WriteString(w, val)
	return err
}

// MacroNode registers a macro function in the context
type MacroNode struct {
	Name       string
	Parameters []string
	Body       Node
}

func (n *MacroNode) Render(ctx *Context, w io.Writer) error {
	ctx.PushLocal("_macro_"+n.Name, n)
	return nil
}

// CallNode calls a previously registered macro
type CallNode struct {
	MacroName           string
	ArgumentExpressions []string
	Evaluator           *Evaluator
}

func (n *CallNode) Render(ctx *Context, w io.Writer) error {
	macroObj := ctx.Get("_macro_" + n.MacroName)
	if macroObj == nil {
		return fmt.Errorf("undefined macro %q", n.MacroName)
	}

	macroNode, ok := macroObj.(*MacroNode)
	if !ok {
		return fmt.Errorf("invalid macro %q in context", n.MacroName)
	}

	macroScope := make(map[string]any)
	for i, paramName := range macroNode.Parameters {
		var argVal any
		if i < len(n.ArgumentExpressions) {
			var err error
			argVal, err = n.Evaluator.Evaluate(n.ArgumentExpressions[i], ctx)
			if err != nil {
				return err
			}
		}
		if argVal == nil {
			argVal = ""
		}
		macroScope[paramName] = argVal
	}

	subContext := ctx.SubContext(macroScope)
	return macroNode.Body.Render(subContext, w)
}

// SeparatorNode renders loop separators
type SeparatorNode struct {
	Body Node
}

func (n *SeparatorNode) Render(ctx *Context, w io.Writer) error {
	return n.Body.Render(ctx, w)
}

// FragmentNode encapsulates fragment bounds
type FragmentNode struct {
	Name string
	Body Node
}

func (n *FragmentNode) Render(ctx *Context, w io.Writer) error {
	return n.Body.Render(ctx, w)
}

// MinifyNode minifies raw HTML inside its scope
type MinifyNode struct {
	Body Node
}

func (n *MinifyNode) Render(ctx *Context, w io.Writer) error {
	var buf bytes.Buffer
	if err := n.Body.Render(ctx, &buf); err != nil {
		return err
	}
	_, err := io.WriteString(w, MinifyHTML(buf.String()))
	return err
}

// ModelNode stores model declarations
type ModelNode struct {
	ModelType string
}

func (n *ModelNode) Render(ctx *Context, w io.Writer) error {
	return nil // type declaration, renders nothing
}

// FieldNode renders name, id, value, and danger class for HTMX forms
type FieldNode struct {
	PropertyPath string
	Evaluator    *Evaluator
}

func (n *FieldNode) Render(ctx *Context, w io.Writer) error {
	name := deriveFieldName(n.PropertyPath)
	rawVal, err := n.Evaluator.Evaluate(n.PropertyPath, ctx)
	if err != nil {
		return err
	}

	valStr := ""
	if rawVal != nil {
		valStr = fmt.Sprintf("%v", rawVal)
	}

	output := fmt.Sprintf(`name="%s" id="%s" value="%s"`, name, name, valStr)

	errorsObj := ctx.Get("errors")
	if errorsObj != nil {
		if errorsMap, ok := errorsObj.(map[string]any); ok && errorsMap[name] != nil {
			output += ` class="input is-danger"`
		} else if errorsMap, ok := errorsObj.(map[string]string); ok && errorsMap[name] != "" {
			output += ` class="input is-danger"`
		}
	}

	_, err = io.WriteString(w, output)
	return err
}

func deriveFieldName(path string) string {
	idx := strings.LastIndexByte(path, '.')
	if idx == -1 {
		return path
	}
	return path[idx+1:]
}

// DisplayNode renders unescaped model output
type DisplayNode struct {
	PropertyPath string
	Evaluator    *Evaluator
}

func (n *DisplayNode) Render(ctx *Context, w io.Writer) error {
	rawVal, err := n.Evaluator.Evaluate(n.PropertyPath, ctx)
	if err != nil {
		return err
	}
	if rawVal != nil {
		_, err = io.WriteString(w, fmt.Sprintf("%v", rawVal))
		return err
	}
	return nil
}

// EditorNode renders generic form input field helper
type EditorNode struct {
	PropertyPath string
	Evaluator    *Evaluator
}

func (n *EditorNode) Render(ctx *Context, w io.Writer) error {
	name := deriveFieldName(n.PropertyPath)
	rawVal, err := n.Evaluator.Evaluate(n.PropertyPath, ctx)
	if err != nil {
		return err
	}

	valStr := ""
	if rawVal != nil {
		valStr = fmt.Sprintf("%v", rawVal)
	}

	inputHtml := fmt.Sprintf(`<input type="text" name="%s" id="%s" value="%s" class="input">`, name, name, valStr)
	_, err = io.WriteString(w, inputHtml)
	return err
}

// AttemptNode catches and recovers from render errors
type AttemptNode struct {
	Body         Node
	RecoverBlock Node
	ErrorVarName string
}

func (n *AttemptNode) Render(ctx *Context, w io.Writer) error {
	var buf bytes.Buffer
	err := n.Body.Render(ctx, &buf)
	if err != nil {
		if n.RecoverBlock != nil {
			nextContext := ctx
			if n.ErrorVarName != "" {
				nextContext = ctx.With(n.ErrorVarName, err.Error())
			}
			return n.RecoverBlock.Render(nextContext, w)
		}
		return nil
	}

	_, writeErr := w.Write(buf.Bytes())
	return writeErr
}

type fallthroughNode struct{}

func (n *fallthroughNode) Render(ctx *Context, w io.Writer) error {
	return nil
}
