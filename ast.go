package pte

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"reflect"
	"regexp"
	"strings"
)

type Node interface {
	Render(context *Context, writer io.Writer) error
}

var (
	errConditionalAttributeSkipped = errors.New("conditional attribute skipped")
	errBreak                       = errors.New("break signal")
	errContinue                    = errors.New("continue signal")
)

// BlockNode holds a list of child nodes
type BlockNode struct {
	Children []Node
}

func (node *BlockNode) Render(context *Context, writer io.Writer) error {
	skipNextLeadingWhitespace := false
	for _, child := range node.Children {
		if skipNextLeadingWhitespace {
			if textNode, isTextNode := child.(*TextNode); isTextNode {
				text := string(textNode.Value)
				trimmed := strings.TrimLeft(text, " \t\r\n")
				if strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "/>") {
					_, err := io.WriteString(writer, trimmed)
					skipNextLeadingWhitespace = false
					if err != nil {
						return err
					}
					continue
				}
			}
			skipNextLeadingWhitespace = false
		}

		err := child.Render(context, writer)
		if err == errConditionalAttributeSkipped {
			if buffer, isBuffer := writer.(*bytes.Buffer); isBuffer {
				bufferBytes := buffer.Bytes()
				for len(bufferBytes) > 0 && isWhitespaceChar(bufferBytes[len(bufferBytes)-1]) {
					bufferBytes = bufferBytes[:len(bufferBytes)-1]
				}
				buffer.Reset()
				buffer.Write(bufferBytes)
			}
			skipNextLeadingWhitespace = true
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

func (node *TextNode) Render(context *Context, writer io.Writer) error {
	_, err := writer.Write(node.Value)
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

func (node *ExpressionNode) Render(context *Context, writer io.Writer) error {
	if node.Condition != "" {
		isConditionTrue, err := node.Evaluator.EvaluateBoolean(node.Condition, context)
		if err != nil {
			return err
		}
		if !isConditionTrue {
			if node.Mode == ModeAttributeEscaped {
				return errConditionalAttributeSkipped
			}
			return nil
		}

		if node.Mode == ModeAttributeEscaped {
			attrOutput, err := node.renderConditionalAttributeOutput(node.Expression, context)
			if err != nil {
				return err
			}
			if attrOutput != "" {
				_, err = io.WriteString(writer, attrOutput)
				return err
			}
		}
	}

	evaluatedValue, err := node.Evaluator.Evaluate(node.Expression, context)
	if err != nil {
		return err
	}

	formatted := node.formatValue(evaluatedValue)
	_, err = io.WriteString(writer, formatted)
	return err
}

func (node *ExpressionNode) renderConditionalAttributeOutput(expression string, context *Context) (string, error) {
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

	evaluatedValue, err := node.Evaluator.Evaluate(valueExpression, context)
	if err != nil {
		return "", err
	}

	return attributeName + "=\"" + attributeEscape(evaluatedValue) + "\"", nil
}

func isConditionalAttributeLiteral(expression string) bool {
	return isValidAttributeName(expression) && !strings.Contains(expression, "=")
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

func (node *ExpressionNode) formatValue(value any) string {
	if value == nil {
		return ""
	}

	switch node.Mode {
	case ModeHtmlEscaped:
		return htmlEscape(value)
	case ModeTrustedHtml:
		return fmt.Sprintf("%v", value)
	case ModeAttributeEscaped:
		return attributeEscape(value)
	case ModeJsonEncoded:
		jsonData, err := json.Marshal(value)
		if err != nil {
			return "null"
		}
		return string(jsonData)
	case ModeUrlEncoded:
		return url.QueryEscape(fmt.Sprintf("%v", value))
	default:
		return htmlEscape(value)
	}
}

func htmlEscape(val any) string {
	stringValue := fmt.Sprintf("%v", val)
	var stringBuilder strings.Builder
	for characterIndex := 0; characterIndex < len(stringValue); characterIndex++ {
		character := stringValue[characterIndex]
		switch character {
		case '&':
			stringBuilder.WriteString("&amp;")
		case '<':
			stringBuilder.WriteString("&lt;")
		case '>':
			stringBuilder.WriteString("&gt;")
		case '"':
			stringBuilder.WriteString("&quot;")
		case '\'':
			stringBuilder.WriteString("&#039;")
		default:
			stringBuilder.WriteByte(character)
		}
	}
	return stringBuilder.String()
}

func attributeEscape(val any) string {
	stringValue := fmt.Sprintf("%v", val)
	var stringBuilder strings.Builder
	for characterIndex := 0; characterIndex < len(stringValue); characterIndex++ {
		character := stringValue[characterIndex]
		switch character {
		case '&':
			stringBuilder.WriteString("&amp;")
		case '<':
			stringBuilder.WriteString("&lt;")
		case '>':
			stringBuilder.WriteString("&gt;")
		case '"':
			stringBuilder.WriteString("&quot;")
		case '\'':
			stringBuilder.WriteString("&#039;")
		case '`':
			stringBuilder.WriteString("&#096;")
		default:
			stringBuilder.WriteByte(character)
		}
	}
	return stringBuilder.String()
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

func (node *IfNode) Render(context *Context, writer io.Writer) error {
	isConditionTrue, err := node.Evaluator.EvaluateBoolean(node.Condition, context)
	if err != nil {
		return err
	}
	if isConditionTrue {
		return node.ThenBlock.Render(context, writer)
	}

	for _, branch := range node.ElseIfBranches {
		branchConditionTrue, err := node.Evaluator.EvaluateBoolean(branch.Condition, context)
		if err != nil {
			return err
		}
		if branchConditionTrue {
			return branch.Block.Render(context, writer)
		}
	}

	if node.ElseBlock != nil {
		return node.ElseBlock.Render(context, writer)
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

func (node *EachNode) Render(context *Context, writer io.Writer) error {
	rawValue, err := node.Evaluator.Evaluate(node.CollectionExpression, context)
	if err != nil {
		return err
	}

	items, isMap, totalItems := node.toIterable(rawValue)
	executedAtLeastOnce := false
	hasCommittedPreviousIter := false

	if totalItems > 0 {
		for itemIndex, item := range items {
			executedAtLeastOnce = true
			isLastItem := (itemIndex == totalItems-1)
			loopMetadata := map[string]any{
				"index": itemIndex,
				"count": itemIndex + 1,
				"first": itemIndex == 0,
				"last":  isLastItem,
				"total": totalItems,
			}

			loopScope := make(map[string]any)
			if isMap && node.IsMapLoop {
				entryMap := item.(map[string]any)
				loopScope[node.KeyName] = entryMap["key"]
				loopScope[node.ValueName] = entryMap["value"]
			} else if isMap {
				entryMap := item.(map[string]any)
				loopScope[node.ItemName] = entryMap
			} else {
				loopScope[node.ItemName] = item
			}
			loopScope["each"] = loopMetadata

			subContext := context.SubContext(loopScope)

			var iterBuf bytes.Buffer
			bodyErr := node.BodyBlock.Render(subContext, &iterBuf)

			if errors.Is(bodyErr, errBreak) {
				break
			}
			if errors.Is(bodyErr, errContinue) {
				continue
			}
			if bodyErr != nil {
				return bodyErr
			}

			if hasCommittedPreviousIter && node.SeparatorNode != nil {
				var sepBuf bytes.Buffer
				sepErr := node.SeparatorNode.Render(subContext, &sepBuf)
				if errors.Is(sepErr, errBreak) {
					break
				}
				if errors.Is(sepErr, errContinue) {
					// suppress separator
				} else if sepErr != nil {
					return sepErr
				} else {
					if _, err := writer.Write(sepBuf.Bytes()); err != nil {
						return err
					}
				}
			}

			if _, err := writer.Write(iterBuf.Bytes()); err != nil {
				return err
			}
			hasCommittedPreviousIter = true
		}
	}

	if !executedAtLeastOnce && node.ElseBlock != nil {
		return node.ElseBlock.Render(context, writer)
	}

	return nil
}

func (node *EachNode) toIterable(value any) ([]any, bool, int) {
	if value == nil {
		return nil, false, 0
	}

	reflectValue := reflect.ValueOf(value)
	for reflectValue.Kind() == reflect.Ptr || reflectValue.Kind() == reflect.Interface {
		if reflectValue.IsNil() {
			return nil, false, 0
		}
		reflectValue = reflectValue.Elem()
	}

	switch reflectValue.Kind() {
	case reflect.Slice, reflect.Array:
		length := reflectValue.Len()
		items := make([]any, length)
		for index := 0; index < length; index++ {
			items[index] = reflectValue.Index(index).Interface()
		}
		return items, false, length

	case reflect.Map:
		length := reflectValue.Len()
		items := make([]any, 0, length)
		for _, key := range reflectValue.MapKeys() {
			items = append(items, map[string]any{
				"key":   key.Interface(),
				"value": reflectValue.MapIndex(key).Interface(),
			})
		}
		return items, true, length
	}

	return []any{value}, false, 1
}

type SwitchClauseKind string

const (
	SwitchCaseClause    SwitchClauseKind = "CASE"
	SwitchDefaultClause SwitchClauseKind = "DEFAULT"
)

// SwitchClause represents a single case or default branch in a switch statement
type SwitchClause struct {
	Kind              SwitchClauseKind
	Expression        string
	Body              Node
	AllowsFallthrough bool
	FallthroughPos    int
	SourcePosition    int
}

type fallthroughNode struct {
	Position int
}

func (node *fallthroughNode) Render(context *Context, writer io.Writer) error {
	return nil
}

// SwitchNode renders switch structures
type SwitchNode struct {
	Expression     string
	Clauses        []SwitchClause
	Evaluator      *Evaluator
	SourcePosition int
}

func (node *SwitchNode) Render(context *Context, writer io.Writer) error {
	switchValue, err := node.Evaluator.Evaluate(node.Expression, context)
	if err != nil {
		return err
	}

	targetIndex := -1
	defaultIndex := -1

	for clauseIndex, clause := range node.Clauses {
		if clause.Kind == SwitchCaseClause {
			caseValue, err := node.Evaluator.Evaluate(clause.Expression, context)
			if err != nil {
				return err
			}
			if node.Evaluator.ValuesEqual(switchValue, caseValue) {
				targetIndex = clauseIndex
				break
			}
		} else if clause.Kind == SwitchDefaultClause && defaultIndex == -1 {
			defaultIndex = clauseIndex
		}
	}

	if targetIndex == -1 {
		targetIndex = defaultIndex
	}

	if targetIndex == -1 {
		return nil
	}

	for clauseIndex := targetIndex; clauseIndex < len(node.Clauses); clauseIndex++ {
		clause := node.Clauses[clauseIndex]
		if err := clause.Body.Render(context, writer); err != nil {
			return err
		}
		if !clause.AllowsFallthrough {
			break
		}
	}

	return nil
}

// IncludeNode represents |include template with data|
type IncludeNode struct {
	TemplateName    string
	ModelExpression string
	Evaluator       *Evaluator
}

func (node *IncludeNode) Render(context *Context, writer io.Writer) error {
	engineObj := context.Get("_engine")
	if engineObj == nil {
		return fmt.Errorf("PTE template engine not found in context")
	}

	engine, isEngine := engineObj.(interface {
		renderNamedTemplate(writer io.Writer, name string, context *Context) error
		createContextFromValue(value any) *Context
	})
	if !isEngine {
		return fmt.Errorf("invalid template engine in context")
	}

	subContext := context
	if node.ModelExpression != "" {
		evaluatedModel, err := node.Evaluator.Evaluate(node.ModelExpression, context)
		if err != nil {
			return err
		}
		subContext = engine.createContextFromValue(evaluatedModel)
		// Inherit local variables like macros and slots
		for key, val := range context.localValues {
			subContext.PushLocal(key, val)
		}
	}

	return engine.renderNamedTemplate(writer, node.TemplateName, subContext)
}

// YieldNode renders layouts yields
type YieldNode struct {
	SectionName string
}

func (node *YieldNode) Render(context *Context, writer io.Writer) error {
	sectionsObj := context.Get("_sections")
	if sectionsObj == nil {
		return fmt.Errorf("|yield| can only be used inside a layout template")
	}

	sectionsMap, isMap := sectionsObj.(map[string]string)
	if !isMap {
		return fmt.Errorf("invalid layout sections in context")
	}

	sectionContent := sectionsMap[node.SectionName]
	_, err := io.WriteString(writer, sectionContent)
	return err
}

// ComponentNode renders component blocks
type ComponentNode struct {
	ComponentName string
	Slots         map[string]Node
}

func (node *ComponentNode) Render(context *Context, writer io.Writer) error {
	engineObj := context.Get("_engine")
	if engineObj == nil {
		return fmt.Errorf("PTE template engine not found in context")
	}

	engine, isEngine := engineObj.(interface {
		renderNamedTemplate(writer io.Writer, name string, context *Context) error
	})
	if !isEngine {
		return fmt.Errorf("invalid template engine in context")
	}

	// Evaluate slots to string buffers in component context
	slotValues := make(map[string]string)
	for slotName, slotBlock := range node.Slots {
		var buffer bytes.Buffer
		if err := slotBlock.Render(context, &buffer); err != nil {
			return err
		}
		slotValues[slotName] = buffer.String()
	}

	// Create subcontext for the component rendering, pushing slot values
	subContext := context.With("_slots", slotValues)
	return engine.renderNamedTemplate(writer, node.ComponentName, subContext)
}

// SlotNode renders component slots
type SlotNode struct {
	SlotName string
}

func (node *SlotNode) Render(context *Context, writer io.Writer) error {
	slotsObj := context.Get("_slots")
	if slotsObj == nil {
		return fmt.Errorf("|slot| can only be rendered inside a component template")
	}

	slotsMap, isMap := slotsObj.(map[string]string)
	if !isMap {
		return fmt.Errorf("invalid component slots in context")
	}

	slotContent := slotsMap[node.SlotName]
	_, err := io.WriteString(writer, slotContent)
	return err
}

// MacroNode registers a macro function in the context
type MacroNode struct {
	Name       string
	Parameters []string
	Body       Node
}

func (node *MacroNode) Render(context *Context, writer io.Writer) error {
	context.PushLocal("_macro_"+node.Name, node)
	return nil
}

// CallNode calls a previously registered macro
type CallNode struct {
	MacroName           string
	ArgumentExpressions []string
	Evaluator           *Evaluator
}

func (node *CallNode) Render(context *Context, writer io.Writer) error {
	macroObj := context.Get("_macro_" + node.MacroName)
	if macroObj == nil {
		return fmt.Errorf("undefined macro %q", node.MacroName)
	}

	macroNode, isMacro := macroObj.(*MacroNode)
	if !isMacro {
		return fmt.Errorf("invalid macro %q in context", node.MacroName)
	}

	macroScope := make(map[string]any)
	for paramIndex, paramName := range macroNode.Parameters {
		var argumentValue any
		if paramIndex < len(node.ArgumentExpressions) {
			var err error
			argumentValue, err = node.Evaluator.Evaluate(node.ArgumentExpressions[paramIndex], context)
			if err != nil {
				return err
			}
		}
		if argumentValue == nil {
			argumentValue = ""
		}
		macroScope[paramName] = argumentValue
	}

	subContext := context.SubContext(macroScope)
	return macroNode.Body.Render(subContext, writer)
}

// SeparatorNode renders loop separators
type SeparatorNode struct {
	Body Node
}

func (node *SeparatorNode) Render(context *Context, writer io.Writer) error {
	return node.Body.Render(context, writer)
}

// FragmentNode encapsulates fragment bounds
type FragmentNode struct {
	Name string
	Body Node
}

func (node *FragmentNode) Render(context *Context, writer io.Writer) error {
	return node.Body.Render(context, writer)
}

// MinifyNode minifies raw HTML inside its scope
type MinifyNode struct {
	Body Node
}

func (node *MinifyNode) Render(context *Context, writer io.Writer) error {
	var buffer bytes.Buffer
	if err := node.Body.Render(context, &buffer); err != nil {
		return err
	}
	_, err := io.WriteString(writer, MinifyHTML(buffer.String()))
	return err
}

// JSBlockNode renders template content inside <script>...</script>
type JSBlockNode struct {
	Body Node
}

func (node *JSBlockNode) Render(context *Context, writer io.Writer) error {
	var buffer bytes.Buffer
	if node.Body != nil {
		if err := node.Body.Render(context, &buffer); err != nil {
			return err
		}
	}
	output := fmt.Sprintf("<script>%s</script>", buffer.String())
	_, err := io.WriteString(writer, output)
	return err
}

// JSExpressionNode evaluates an expression and wraps it in <script>...</script>
type JSExpressionNode struct {
	Expression string
	Evaluator  *Evaluator
}

func (node *JSExpressionNode) Render(context *Context, writer io.Writer) error {
	val, err := node.Evaluator.Evaluate(node.Expression, context)
	if err != nil {
		return err
	}
	valStr := ""
	if val != nil {
		valStr = fmt.Sprintf("%v", val)
	}
	output := fmt.Sprintf("<script>%s</script>", valStr)
	_, err = io.WriteString(writer, output)
	return err
}

// CSSBlockNode renders template content inside <style>...</style>
type CSSBlockNode struct {
	Body Node
}

func (node *CSSBlockNode) Render(context *Context, writer io.Writer) error {
	var buffer bytes.Buffer
	if node.Body != nil {
		if err := node.Body.Render(context, &buffer); err != nil {
			return err
		}
	}
	output := fmt.Sprintf("<style>%s</style>", buffer.String())
	_, err := io.WriteString(writer, output)
	return err
}

// CSSExpressionNode evaluates an expression and wraps it in <style>...</style>
type CSSExpressionNode struct {
	Expression string
	Evaluator  *Evaluator
}

func (node *CSSExpressionNode) Render(context *Context, writer io.Writer) error {
	val, err := node.Evaluator.Evaluate(node.Expression, context)
	if err != nil {
		return err
	}
	valStr := ""
	if val != nil {
		valStr = fmt.Sprintf("%v", val)
	}
	output := fmt.Sprintf("<style>%s</style>", valStr)
	_, err = io.WriteString(writer, output)
	return err
}

// ModelNode stores model declarations
type ModelNode struct {
	ModelType string
}

func (node *ModelNode) Render(context *Context, writer io.Writer) error {
	return nil // type declaration, renders nothing
}

// FieldNode renders name, id, value, and danger class for HTMX forms
type FieldNode struct {
	PropertyPath string
	Evaluator    *Evaluator
}

func (node *FieldNode) Render(context *Context, writer io.Writer) error {
	name := deriveFieldName(node.PropertyPath)
	rawValue, err := node.Evaluator.Evaluate(node.PropertyPath, context)
	if err != nil {
		return err
	}

	valueString := ""
	if rawValue != nil {
		valueString = fmt.Sprintf("%v", rawValue)
	}

	escapedName := attributeEscape(name)
	escapedVal := attributeEscape(valueString)

	output := fmt.Sprintf(`name="%s" id="%s" value="%s"`, escapedName, escapedName, escapedVal)

	errorsObj := context.Get("errors")
	if errorsObj != nil {
		if errorsMap, isMap := errorsObj.(map[string]any); isMap && errorsMap[name] != nil {
			output += ` class="input is-danger"`
		} else if errorsMap, isMap := errorsObj.(map[string]string); isMap && errorsMap[name] != "" {
			output += ` class="input is-danger"`
		}
	}

	_, err = io.WriteString(writer, output)
	return err
}

func deriveFieldName(path string) string {
	dotIndex := strings.LastIndexByte(path, '.')
	if dotIndex == -1 {
		return path
	}
	return path[dotIndex+1:]
}

// DisplayNode renders unescaped model output
type DisplayNode struct {
	PropertyPath string
	Evaluator    *Evaluator
}

func (node *DisplayNode) Render(context *Context, writer io.Writer) error {
	rawValue, err := node.Evaluator.Evaluate(node.PropertyPath, context)
	if err != nil {
		return err
	}
	if rawValue != nil {
		_, err = io.WriteString(writer, fmt.Sprintf("%v", rawValue))
		return err
	}
	return nil
}

// EditorNode renders generic form input field helper
type EditorNode struct {
	PropertyPath string
	Evaluator    *Evaluator
}

func (node *EditorNode) Render(context *Context, writer io.Writer) error {
	name := deriveFieldName(node.PropertyPath)
	rawValue, err := node.Evaluator.Evaluate(node.PropertyPath, context)
	if err != nil {
		return err
	}

	valueString := ""
	if rawValue != nil {
		valueString = fmt.Sprintf("%v", rawValue)
	}

	escapedName := attributeEscape(name)
	escapedVal := attributeEscape(valueString)

	inputHtml := fmt.Sprintf(`<input type="text" name="%s" id="%s" value="%s" class="input">`, escapedName, escapedName, escapedVal)
	_, err = io.WriteString(writer, inputHtml)
	return err
}

// AttemptNode catches and recovers from render errors
type AttemptNode struct {
	Body         Node
	RecoverBlock Node
	ErrorVarName string
}

func (node *AttemptNode) Render(context *Context, writer io.Writer) error {
	var buffer bytes.Buffer
	var renderError error

	func() {
		defer func() {
			if recoveredPanic := recover(); recoveredPanic != nil {
				if panicErr, isError := recoveredPanic.(error); isError {
					renderError = panicErr
				} else {
					renderError = fmt.Errorf("%v", recoveredPanic)
				}
			}
		}()
		renderError = node.Body.Render(context, &buffer)
	}()

	if renderError != nil {
		if errors.Is(renderError, errBreak) || errors.Is(renderError, errContinue) {
			return renderError
		}
		if node.RecoverBlock != nil {
			nextContext := context
			if node.ErrorVarName != "" {
				nextContext = context.With(node.ErrorVarName, renderError.Error())
			}
			return node.RecoverBlock.Render(nextContext, writer)
		}
		return nil
	}

	_, writeErr := writer.Write(buffer.Bytes())
	return writeErr
}

// ForNode renders range-based loops
type ForNode struct {
	VarName       string
	StartExpr     string
	EndExpr       string
	StepExpr      string
	BodyBlock     Node
	ElseBlock     Node
	SeparatorNode Node
	Evaluator     *Evaluator
	Position      int
}

func (node *ForNode) Render(context *Context, writer io.Writer) error {
	startVal, err := evaluateInt(node.Evaluator, node.StartExpr, context)
	if err != nil {
		return fmt.Errorf("invalid start expression in for loop at %d: %w", node.Position, err)
	}
	endVal, err := evaluateInt(node.Evaluator, node.EndExpr, context)
	if err != nil {
		return fmt.Errorf("invalid end expression in for loop at %d: %w", node.Position, err)
	}
	stepVal := int64(1)
	if node.StepExpr != "" {
		stepVal, err = evaluateInt(node.Evaluator, node.StepExpr, context)
		if err != nil {
			return fmt.Errorf("invalid step expression in for loop at %d: %w", node.Position, err)
		}
	}

	if stepVal <= 0 {
		return fmt.Errorf("zero or negative step in for loop at %d: %d", node.Position, stepVal)
	}

	step := uint64(stepVal)
	executedAtLeastOnce := false
	hasCommittedPreviousIter := false

	currentVal := startVal
	isAscending := startVal <= endVal

	for {
		executedAtLeastOnce = true

		var distance uint64
		if isAscending {
			distance = uint64(endVal) - uint64(currentVal)
		} else {
			distance = uint64(currentVal) - uint64(endVal)
		}

		isLast := step > distance

		loopScope := map[string]any{
			node.VarName: currentVal,
		}
		subContext := context.SubContext(loopScope)

		var iterBuf bytes.Buffer
		bodyErr := node.BodyBlock.Render(subContext, &iterBuf)

		if errors.Is(bodyErr, errBreak) {
			break
		}
		if errors.Is(bodyErr, errContinue) {
			if isLast {
				break
			}
			if isAscending {
				currentVal += int64(step)
			} else {
				currentVal -= int64(step)
			}
			continue
		}
		if bodyErr != nil {
			return bodyErr
		}

		if hasCommittedPreviousIter && node.SeparatorNode != nil {
			var sepBuf bytes.Buffer
			sepErr := node.SeparatorNode.Render(subContext, &sepBuf)
			if errors.Is(sepErr, errBreak) {
				break
			}
			if errors.Is(sepErr, errContinue) {
				// Suppress separator
			} else if sepErr != nil {
				return sepErr
			} else {
				if _, err := writer.Write(sepBuf.Bytes()); err != nil {
					return err
				}
			}
		}

		if _, err := writer.Write(iterBuf.Bytes()); err != nil {
			return err
		}
		hasCommittedPreviousIter = true

		if isLast {
			break
		}

		if isAscending {
			currentVal += int64(step)
		} else {
			currentVal -= int64(step)
		}
	}

	if !executedAtLeastOnce && node.ElseBlock != nil {
		return node.ElseBlock.Render(context, writer)
	}

	return nil
}

func evaluateInt(evaluator *Evaluator, expression string, context *Context) (int64, error) {
	evaluatedVal, err := evaluator.Evaluate(expression, context)
	if err != nil {
		return 0, err
	}
	if evaluatedVal == nil {
		return 0, fmt.Errorf("expression %q evaluated to null", expression)
	}

	num, isNum := parseNumberValue(evaluatedVal)
	if !isNum {
		return 0, fmt.Errorf("expression %q did not evaluate to an integer", expression)
	}

	switch num.kind {
	case numberKindSignedInt:
		return num.intVal, nil
	case numberKindUnsignedInt:
		if num.uintVal > math.MaxInt64 {
			return 0, fmt.Errorf("integer boundary overflow in expression %q", expression)
		}
		return int64(num.uintVal), nil
	case numberKindFloat:
		if num.floatVal == math.Trunc(num.floatVal) && !math.IsNaN(num.floatVal) && !math.IsInf(num.floatVal, 0) {
			if num.floatVal >= math.MinInt64 && num.floatVal <= math.MaxInt64 {
				return int64(num.floatVal), nil
			}
		}
		return 0, fmt.Errorf("expression %q did not evaluate to an exact integer: %v", expression, evaluatedVal)
	}
	return 0, fmt.Errorf("expression %q did not evaluate to an integer", expression)
}

// RawNode represents un-evaluated raw template text (|raw|...|/raw|)
type RawNode struct {
	Content string
}

func (node *RawNode) Render(context *Context, writer io.Writer) error {
	_, err := io.WriteString(writer, node.Content)
	return err
}

// ContinueNode represents a |continue| directive
type ContinueNode struct {
	Position int
}

func (node *ContinueNode) Render(context *Context, writer io.Writer) error {
	return errContinue
}

// BreakNode represents a |break| directive
type BreakNode struct {
	Position int
}

func (node *BreakNode) Render(context *Context, writer io.Writer) error {
	return errBreak
}

// PWANode generates PWA manifest, theme, mobile viewport, icons, and service worker registration tags
type PWANode struct {
	Name        string
	Manifest    string
	Theme       string
	Icon        string
	SW          string
	StatusColor string
}

func (node *PWANode) Render(context *Context, writer io.Writer) error {
	manifest := node.Manifest
	if manifest == "" {
		manifest = "/manifest.json"
	}
	theme := node.Theme
	if theme == "" {
		theme = "#000000"
	}
	statusColor := node.StatusColor
	if statusColor == "" {
		statusColor = "default"
	}

	var metaTags []string
	metaTags = append(metaTags, `<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">`)
	metaTags = append(metaTags, fmt.Sprintf(`<meta name="theme-color" content="%s">`, htmlEscape(theme)))
	metaTags = append(metaTags, `<meta name="mobile-web-app-capable" content="yes">`)
	metaTags = append(metaTags, `<meta name="apple-mobile-web-app-capable" content="yes">`)
	metaTags = append(metaTags, fmt.Sprintf(`<meta name="apple-mobile-web-app-status-bar-style" content="%s">`, htmlEscape(statusColor)))

	if node.Name != "" {
		metaTags = append(metaTags, fmt.Sprintf(`<meta name="apple-mobile-web-app-title" content="%s">`, htmlEscape(node.Name)))
		metaTags = append(metaTags, fmt.Sprintf(`<meta name="application-name" content="%s">`, htmlEscape(node.Name)))
	}

	if node.Manifest != "none" && node.Manifest != "false" {
		metaTags = append(metaTags, fmt.Sprintf(`<link rel="manifest" href="%s">`, htmlEscape(manifest)))
	}

	if node.Icon != "" && node.Icon != "none" && node.Icon != "false" {
		metaTags = append(metaTags, fmt.Sprintf(`<link rel="apple-touch-icon" href="%s">`, htmlEscape(node.Icon)))
		metaTags = append(metaTags, fmt.Sprintf(`<link rel="icon" href="%s">`, htmlEscape(node.Icon)))
	}

	if node.SW != "" && node.SW != "none" && node.SW != "false" {
		encodedSW, err := json.Marshal(node.SW)
		if err != nil {
			encodedSW = []byte(fmt.Sprintf("%q", node.SW))
		}
		script := fmt.Sprintf(`<script>if('serviceWorker' in navigator){window.addEventListener('load',function(){navigator.serviceWorker.register(%s);});}</script>`, string(encodedSW))
		metaTags = append(metaTags, script)
	}

	output := strings.Join(metaTags, "\n")
	_, err := io.WriteString(writer, output)
	return err
}

// HTMXNode generates script tags, config meta, extension scripts, and indicator styles for HTMX
type HTMXNode struct {
	Src        string
	Extensions []string
	Config     string
	Indicator  bool
}

func (node *HTMXNode) Render(context *Context, writer io.Writer) error {
	src := node.Src
	if src == "" {
		src = "https://unpkg.com/htmx.org@1.9.10"
	}

	var scriptTags []string
	if node.Config != "" {
		scriptTags = append(scriptTags, fmt.Sprintf(`<meta name="htmx-config" content="%s">`, htmlEscape(node.Config)))
	}

	scriptTags = append(scriptTags, fmt.Sprintf(`<script src="%s"></script>`, htmlEscape(src)))

	for _, extensionName := range node.Extensions {
		trimmedExtName := strings.TrimSpace(extensionName)
		if trimmedExtName != "" {
			extensionURL := fmt.Sprintf("https://unpkg.com/htmx.org@1.9.10/dist/ext/%s.js", htmlEscape(trimmedExtName))
			scriptTags = append(scriptTags, fmt.Sprintf(`<script src="%s"></script>`, extensionURL))
		}
	}

	if node.Indicator {
		scriptTags = append(scriptTags, `<style>.htmx-indicator{display:none;}.htmx-request .htmx-indicator,.htmx-request.htmx-indicator{display:inline-block;}</style>`)
	}

	output := strings.Join(scriptTags, "\n")
	_, err := io.WriteString(writer, output)
	return err
}

// HXAttrNode renders concise HTMX element attributes
type HXAttrNode struct {
	Method    string
	URL       string
	Target    string
	Swap      string
	Indicator string
	Trigger   string
}

func (node *HXAttrNode) Render(context *Context, writer io.Writer) error {
	var attributeStrings []string
	if node.Method != "" && node.URL != "" {
		attributeStrings = append(attributeStrings, fmt.Sprintf(`hx-%s="%s"`, node.Method, htmlEscape(node.URL)))
	}
	if node.Target != "" {
		attributeStrings = append(attributeStrings, fmt.Sprintf(`hx-target="%s"`, htmlEscape(node.Target)))
	}
	if node.Swap != "" {
		attributeStrings = append(attributeStrings, fmt.Sprintf(`hx-swap="%s"`, htmlEscape(node.Swap)))
	}
	if node.Indicator != "" {
		attributeStrings = append(attributeStrings, fmt.Sprintf(`hx-indicator="%s"`, htmlEscape(node.Indicator)))
	}
	if node.Trigger != "" {
		attributeStrings = append(attributeStrings, fmt.Sprintf(`hx-trigger="%s"`, htmlEscape(node.Trigger)))
	}

	output := strings.Join(attributeStrings, " ")
	_, err := io.WriteString(writer, output)
	return err
}

// AlpineNode generates script tags, plugin CDN references, and cloak styles for Alpine.js
type AlpineNode struct {
	Src     string
	Plugins []string
	Cloak   bool
	Version string
	Build   string
}

func (node *AlpineNode) Render(context *Context, writer io.Writer) error {
	targetVersion := node.Version
	if targetVersion == "" {
		targetVersion = DefaultAlpineVersion
	}

	var scriptTags []string
	for _, pluginName := range node.Plugins {
		trimmedPluginName := strings.TrimSpace(pluginName)
		if trimmedPluginName != "" {
			pluginURL := fmt.Sprintf("https://cdn.jsdelivr.net/npm/@alpinejs/%s@%s/dist/cdn.min.js", htmlEscape(trimmedPluginName), htmlEscape(targetVersion))
			scriptTags = append(scriptTags, fmt.Sprintf(`<script defer src="%s"></script>`, pluginURL))
		}
	}

	var coreURL string
	if node.Src != "" {
		coreURL = node.Src
	} else if node.Build == "csp" {
		coreURL = fmt.Sprintf("https://cdn.jsdelivr.net/npm/@alpinejs/csp@%s/dist/cdn.min.js", htmlEscape(targetVersion))
	} else {
		coreURL = fmt.Sprintf("https://cdn.jsdelivr.net/npm/alpinejs@%s/dist/cdn.min.js", htmlEscape(targetVersion))
	}

	scriptTags = append(scriptTags, fmt.Sprintf(`<script defer src="%s"></script>`, htmlEscape(coreURL)))

	if node.Cloak {
		scriptTags = append(scriptTags, `<style>[x-cloak]{display:none !important;}</style>`)
	}

	renderedOutput := strings.Join(scriptTags, "\n")
	_, writeError := io.WriteString(writer, renderedOutput)
	return writeError
}

// StateNode generates Alpine.js x-data reactive component state declarations
type StateNode struct {
	StateMap map[string]any
}

func (node *StateNode) Render(context *Context, writer io.Writer) error {
	jsonBytes, marshalErr := json.Marshal(node.StateMap)
	if marshalErr != nil {
		return fmt.Errorf("failed to serialize Alpine state to JSON: %w", marshalErr)
	}

	jsonString := string(jsonBytes)
	escapedJSON := strings.ReplaceAll(jsonString, `"`, "&quot;")

	renderedOutput := fmt.Sprintf(`x-data="%s"`, escapedJSON)
	_, writeError := io.WriteString(writer, renderedOutput)
	return writeError
}

// AlpineAttrNode renders generic Alpine.js x-* element attributes (e.g. alpine-show, alpine-text, alpine-cloak)
type AlpineAttrNode struct {
	Directive string
	Value     string
}

func (node *AlpineAttrNode) Render(context *Context, writer io.Writer) error {
	directiveName := strings.TrimPrefix(node.Directive, "alpine-")
	if node.Value == "" {
		_, writeError := io.WriteString(writer, fmt.Sprintf(`x-%s`, directiveName))
		return writeError
	}
	escapedValue := htmlEscape(node.Value)
	_, writeError := io.WriteString(writer, fmt.Sprintf(`x-%s="%s"`, directiveName, escapedValue))
	return writeError
}
