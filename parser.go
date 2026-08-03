package pte

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Parser struct {
	evaluator *Evaluator
}

func NewParser() *Parser {
	return &Parser{
		evaluator: NewEvaluator(),
	}
}

type CompiledTemplate struct {
	RootNode Node
	Metadata map[string]any
}

type LayoutNode struct {
	LayoutName string
	Sections   map[string]Node
}

func (node *LayoutNode) Render(context *Context, writer io.Writer) error {
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

	// Render sections to string buffers
	sectionValues := make(map[string]string)
	for name, sectionBlock := range node.Sections {
		var buffer bytes.Buffer
		if err := sectionBlock.Render(context, &buffer); err != nil {
			return err
		}
		sectionValues[name] = buffer.String()
	}

	subContext := context.With("_sections", sectionValues)
	return engine.renderNamedTemplate(writer, node.LayoutName, subContext)
}

func (parser *Parser) Parse(tokens []Token) (*CompiledTemplate, error) {
	cursor := &parserCursor{tokens: tokens}
	metadata := make(map[string]any)

	// Skip leading whitespace/comments
	parser.skipWhitespaceAndComments(cursor)

	// Check if the template starts with |layout templateName|
	if cursor.hasNext() && cursor.peek().Type == TokenLayout {
		layoutToken := cursor.next()
		layoutName := strings.TrimSpace(layoutToken.Value[len("layout "):])
		if layoutName == "" {
			return nil, fmt.Errorf("layout template name must not be empty at %d", layoutToken.Position)
		}

		sections := make(map[string]Node)
		for cursor.hasNext() {
			parser.skipWhitespaceAndComments(cursor)
			if !cursor.hasNext() {
				break
			}

			token := cursor.peek()
			if token.Type != TokenSection {
				return nil, fmt.Errorf("unexpected token %q outside section blocks in layout page at %d", token.Value, token.Position)
			}
			cursor.next()

			secName := strings.TrimSpace(token.Value[len("section "):])
			if secName == "" {
				return nil, fmt.Errorf("section name must not be empty at %d", token.Position)
			}

			secBody, err := parser.parseBlock(cursor, TokenEndSection, metadata)
			if err != nil {
				return nil, err
			}

			if cursor.hasNext() && cursor.peek().Type == TokenEndSection {
				cursor.next()
			} else {
				return nil, fmt.Errorf("missing closing |/section| for section %q", secName)
			}

			sections[secName] = secBody
		}

		return &CompiledTemplate{
			RootNode: &LayoutNode{
				LayoutName: layoutName,
				Sections:   sections,
			},
			Metadata: metadata,
		}, nil
	}

	rootBlock, err := parser.parseBlockWithLoopDepthDirect(cursor, "", metadata, 0, false)
	if err != nil {
		return nil, err
	}
	if cursor.hasNext() {
		token := cursor.peek()
		if token.Type == TokenElse {
			return nil, fmt.Errorf("|else| outside for or each at position %d", token.Position)
		}
		if token.Type == TokenCase {
			return nil, fmt.Errorf("misplaced |case| directive at position %d", token.Position)
		}
		if token.Type == TokenDefault {
			return nil, fmt.Errorf("misplaced |default| directive at position %d", token.Position)
		}
		return nil, fmt.Errorf("unexpected directive |%s| at position %d", token.Value, token.Position)
	}

	return &CompiledTemplate{
		RootNode: rootBlock,
		Metadata: metadata,
	}, nil
}

type parserCursor struct {
	tokens []Token
	index  int
}

func (cursor *parserCursor) hasNext() bool {
	return cursor.index < len(cursor.tokens)
}

func (cursor *parserCursor) peek() Token {
	if !cursor.hasNext() {
		return Token{}
	}
	return cursor.tokens[cursor.index]
}

func (cursor *parserCursor) next() Token {
	token := cursor.peek()
	cursor.index++
	return token
}

func (parser *Parser) parseBlock(cursor *parserCursor, stopToken TokenType, metadata map[string]any) (Node, error) {
	return parser.parseBlockWithLoopDepthDirect(cursor, stopToken, metadata, 0, false)
}

func (parser *Parser) parseBlockWithLoopDepth(cursor *parserCursor, stopToken TokenType, metadata map[string]any, loopDepth int) (Node, error) {
	return parser.parseBlockWithLoopDepthDirect(cursor, stopToken, metadata, loopDepth, false)
}

func (parser *Parser) parseBlockWithLoopDepthDirect(cursor *parserCursor, stopToken TokenType, metadata map[string]any, loopDepth int, inSwitchClauseDirect bool) (Node, error) {
	var nodes []Node

	for cursor.hasNext() {
		token := cursor.peek()

		if stopToken != "" && token.Type == stopToken {
			break
		}

		if token.Type == TokenElse || token.Type == TokenElseIf || token.Type == TokenRecover {
			break
		}

		if inSwitchClauseDirect && (token.Type == TokenCase || token.Type == TokenDefault) {
			break
		}

		cursor.next()

		switch token.Type {
		case TokenText:
			nodes = append(nodes, NewTextNode(token.Value))
		case TokenRaw:
			nodes = append(nodes, &RawNode{Content: token.Value})
		case TokenComment:
			// Ignore comments
		case TokenExpression:
			exprNode, err := parser.parseExpression(token.Value)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, exprNode)
		case TokenIf:
			ifNode, err := parser.parseIf(token, cursor, metadata, loopDepth)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, ifNode)
		case TokenEach:
			eachNode, err := parser.parseEach(token, cursor, metadata, loopDepth)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, eachNode)
		case TokenFor:
			forNode, err := parser.parseFor(token, cursor, metadata, loopDepth)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, forNode)
		case TokenContinue:
			if loopDepth <= 0 {
				return nil, fmt.Errorf("|continue| outside a loop at %d", token.Position)
			}
			nodes = append(nodes, &ContinueNode{Position: token.Position})
		case TokenBreak:
			if loopDepth <= 0 {
				return nil, fmt.Errorf("|break| outside a loop at %d", token.Position)
			}
			nodes = append(nodes, &BreakNode{Position: token.Position})
		case TokenCase, TokenDefault, TokenEndRaw:
			return nil, fmt.Errorf("misplaced |%s| directive at position %d", token.Value, token.Position)
		case TokenEndFor, TokenEndEach, TokenEndIf, TokenEndSection, TokenEndComponent, TokenEndSlot, TokenEndMacro, TokenEndFragment, TokenEndMinify, TokenEndAttempt, TokenEndSwitch, TokenEndSeparator:
			return nil, fmt.Errorf("misplaced loop or block directive |%s| at %d", token.Value, token.Position)
		case TokenModel:
			modelType := strings.TrimSpace(token.Value[len("model "):])
			nodes = append(nodes, &ModelNode{ModelType: modelType})
		case TokenField:
			propertyPath := strings.TrimSpace(token.Value[len("field "):])
			nodes = append(nodes, &FieldNode{PropertyPath: propertyPath, Evaluator: parser.evaluator})
		case TokenDisplay:
			propertyPath := strings.TrimSpace(token.Value[len("display "):])
			nodes = append(nodes, &DisplayNode{PropertyPath: propertyPath, Evaluator: parser.evaluator})
		case TokenEditor:
			propertyPath := strings.TrimSpace(token.Value[len("editor "):])
			nodes = append(nodes, &EditorNode{PropertyPath: propertyPath, Evaluator: parser.evaluator})
		case TokenMacro:
			macroNode, err := parser.parseMacro(token, cursor, metadata)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, macroNode)
		case TokenCall:
			callNode, err := parser.parseCallMacro(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, callNode)
		case TokenSeparator:
			sepBody, err := parser.parseBlockWithLoopDepthDirect(cursor, TokenEndSeparator, metadata, loopDepth, false)
			if err != nil {
				return nil, err
			}
			if cursor.hasNext() && cursor.peek().Type == TokenEndSeparator {
				cursor.next()
			}
			nodes = append(nodes, &SeparatorNode{Body: sepBody})
		case TokenFragment:
			fragNode, err := parser.parseFragment(token, cursor, metadata, loopDepth)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, fragNode)
		case TokenMinify:
			minNode, err := parser.parseMinify(token, cursor, metadata, loopDepth)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, minNode)
		case TokenPage:
			parser.parsePageMetadata(token, metadata)
		case TokenAttempt:
			attNode, err := parser.parseAttempt(token, cursor, metadata, loopDepth)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, attNode)
		case TokenInclude:
			incNode, err := parser.parseInclude(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, incNode)
		case TokenYield:
			secName := strings.TrimSpace(token.Value[len("yield "):])
			nodes = append(nodes, &YieldNode{SectionName: secName})
		case TokenComponent:
			compNode, err := parser.parseComponent(token, cursor, metadata, loopDepth)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, compNode)
		case TokenSlot:
			slotName := strings.TrimSpace(token.Value[len("slot "):])
			nodes = append(nodes, &SlotNode{SlotName: slotName})
		case TokenSwitch:
			swNode, err := parser.parseSwitch(token, cursor, metadata, loopDepth)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, swNode)
		case TokenFallthrough:
			if !inSwitchClauseDirect {
				return nil, fmt.Errorf("fallthrough is only allowed as the final directive of a switch clause at position %d", token.Position)
			}
			nodes = append(nodes, &fallthroughNode{Position: token.Position})
		case TokenPWA:
			pwaNode, err := parser.parsePWA(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, pwaNode)
		case TokenHTMX:
			htmxNode, err := parser.parseHTMX(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, htmxNode)
		case TokenHXAttr:
			hxAttrNode, err := parser.parseHXAttr(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, hxAttrNode)
		case TokenAlpine:
			alpineNode, err := parser.parseAlpine(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, alpineNode)
		case TokenState:
			stateNode, err := parser.parseState(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, stateNode)
		case TokenAlpineAttr:
			alpineAttrNode, err := parser.parseAlpineAttr(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, alpineAttrNode)
		default:
			exprNode, err := parser.parseExpression(token.Value)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, exprNode)
		}
	}

	return &BlockNode{Children: nodes}, nil
}

func (parser *Parser) parseExpression(rawValue string) (Node, error) {
	trimmed := strings.TrimSpace(rawValue)

	var condition string
	ifIndex := findOutputIfIndex(trimmed)
	if ifIndex != -1 {
		condition = strings.TrimSpace(trimmed[ifIndex+2:])
		trimmed = strings.TrimSpace(trimmed[:ifIndex])
	}

	var mode OutputMode
	var expressionText string

	if strings.HasPrefix(trimmed, "html ") {
		mode = ModeTrustedHtml
		expressionText = strings.TrimSpace(trimmed[len("html "):])
	} else if strings.HasPrefix(trimmed, "attr ") {
		mode = ModeAttributeEscaped
		expressionText = strings.TrimSpace(trimmed[len("attr "):])
	} else if strings.HasPrefix(trimmed, "url ") {
		mode = ModeUrlEncoded
		expressionText = strings.TrimSpace(trimmed[len("url "):])
	} else if strings.HasPrefix(trimmed, "json ") {
		mode = ModeJsonEncoded
		expressionText = strings.TrimSpace(trimmed[len("json "):])
	} else {
		mode = ModeHtmlEscaped
		expressionText = trimmed
	}

	return &ExpressionNode{
		Expression: expressionText,
		Mode:       mode,
		Evaluator:  parser.evaluator,
		Condition:  condition,
	}, nil
}

func (parser *Parser) parseIf(ifToken Token, cursor *parserCursor, metadata map[string]any, loopDepth int) (Node, error) {
	condition := strings.TrimSpace(ifToken.Value[len("if "):])
	thenBlock, err := parser.parseBlockWithLoopDepth(cursor, TokenEndIf, metadata, loopDepth)
	if err != nil {
		return nil, err
	}

	var elseIfBranches []ElseIfBranch
	var elseBlock Node

	for cursor.hasNext() && cursor.peek().Type != TokenEndIf {
		current := cursor.peek()
		if current.Type == TokenElseIf {
			cursor.next()
			cond := strings.TrimSpace(current.Value[len("else if "):])
			body, err := parser.parseBlockWithLoopDepth(cursor, TokenEndIf, metadata, loopDepth)
			if err != nil {
				return nil, err
			}
			elseIfBranches = append(elseIfBranches, ElseIfBranch{Condition: cond, Block: body})
		} else if current.Type == TokenElse {
			cursor.next()
			var err error
			elseBlock, err = parser.parseBlockWithLoopDepth(cursor, TokenEndIf, metadata, loopDepth)
			if err != nil {
				return nil, err
			}
			break
		} else {
			break
		}
	}

	if cursor.hasNext() && cursor.peek().Type == TokenEndIf {
		cursor.next()
	} else {
		return nil, fmt.Errorf("missing closing |/if| for statement at %d", ifToken.Position)
	}

	return &IfNode{
		Condition:      condition,
		ThenBlock:      thenBlock,
		ElseIfBranches: elseIfBranches,
		ElseBlock:      elseBlock,
		Evaluator:      parser.evaluator,
	}, nil
}

func (parser *Parser) parseEach(eachToken Token, cursor *parserCursor, metadata map[string]any, loopDepth int) (Node, error) {
	statement := strings.TrimSpace(eachToken.Value[len("each "):])
	inIndex := strings.Index(statement, " in ")
	if inIndex == -1 {
		return nil, fmt.Errorf("invalid each statement format. Expected '|each item in items|' at %d", eachToken.Position)
	}

	leftSide := strings.TrimSpace(statement[:inIndex])
	collectionExpr := strings.TrimSpace(statement[inIndex+4:])

	bodyBlock, err := parser.parseBlockWithLoopDepth(cursor, TokenEndEach, metadata, loopDepth+1)
	if err != nil {
		return nil, err
	}

	var elseBlock Node
	if cursor.hasNext() && cursor.peek().Type == TokenElse {
		cursor.next()
		var err error
		elseBlock, err = parser.parseBlockWithLoopDepth(cursor, TokenEndEach, metadata, loopDepth)
		if err != nil {
			return nil, err
		}
		if cursor.hasNext() && cursor.peek().Type == TokenElse {
			return nil, fmt.Errorf("multiple |else| blocks in one loop at position %d", cursor.peek().Position)
		}
	}

	if cursor.hasNext() && cursor.peek().Type == TokenEndEach {
		cursor.next()
	} else {
		return nil, fmt.Errorf("missing closing |/each| for loop at %d", eachToken.Position)
	}

	// Separate SeparatorNode from body block children if present
	var separatorNode Node
	if block, isBlockNode := bodyBlock.(*BlockNode); isBlockNode {
		var bodyChildren []Node
		for _, child := range block.Children {
			if sep, isSeparator := child.(*SeparatorNode); isSeparator {
				separatorNode = sep
			} else {
				bodyChildren = append(bodyChildren, child)
			}
		}
		bodyBlock = &BlockNode{Children: bodyChildren}
	}

	var itemName, keyName, valueName string
	isMapLoop := false
	if strings.Contains(leftSide, ",") {
		parts := strings.SplitN(leftSide, ",", 2)
		keyName = strings.TrimSpace(parts[0])
		valueName = strings.TrimSpace(parts[1])
		isMapLoop = true
	} else {
		itemName = leftSide
	}

	return &EachNode{
		ItemName:             itemName,
		KeyName:              keyName,
		ValueName:            valueName,
		CollectionExpression: collectionExpr,
		BodyBlock:            bodyBlock,
		ElseBlock:            elseBlock,
		SeparatorNode:        separatorNode,
		Evaluator:            parser.evaluator,
		IsMapLoop:            isMapLoop,
	}, nil
}

func (parser *Parser) parseFor(forToken Token, cursor *parserCursor, metadata map[string]any, loopDepth int) (Node, error) {
	statement := strings.TrimSpace(forToken.Value[len("for "):])
	varName, startExpr, endExpr, stepExpr, err := parser.parseForHeader(statement, forToken.Position)
	if err != nil {
		return nil, err
	}

	bodyBlock, err := parser.parseBlockWithLoopDepth(cursor, TokenEndFor, metadata, loopDepth+1)
	if err != nil {
		return nil, err
	}

	var elseBlock Node
	if cursor.hasNext() && cursor.peek().Type == TokenElse {
		cursor.next()
		var err error
		elseBlock, err = parser.parseBlockWithLoopDepth(cursor, TokenEndFor, metadata, loopDepth)
		if err != nil {
			return nil, err
		}
		if cursor.hasNext() && cursor.peek().Type == TokenElse {
			return nil, fmt.Errorf("multiple |else| blocks in one loop at position %d", cursor.peek().Position)
		}
	}

	if cursor.hasNext() && cursor.peek().Type == TokenEndFor {
		cursor.next()
	} else {
		return nil, fmt.Errorf("missing closing |/for| for loop at position %d", forToken.Position)
	}

	var separatorNode Node
	if block, isBlockNode := bodyBlock.(*BlockNode); isBlockNode {
		var bodyChildren []Node
		for _, child := range block.Children {
			if sep, isSeparator := child.(*SeparatorNode); isSeparator {
				separatorNode = sep
			} else {
				bodyChildren = append(bodyChildren, child)
			}
		}
		bodyBlock = &BlockNode{Children: bodyChildren}
	}

	return &ForNode{
		VarName:       varName,
		StartExpr:     startExpr,
		EndExpr:       endExpr,
		StepExpr:      stepExpr,
		BodyBlock:     bodyBlock,
		ElseBlock:     elseBlock,
		SeparatorNode: separatorNode,
		Evaluator:     parser.evaluator,
		Position:      forToken.Position,
	}, nil
}

func (parser *Parser) parseForHeader(statement string, position int) (varName, startExpr, endExpr, stepExpr string, err error) {
	if statement == "" {
		return "", "", "", "", fmt.Errorf("missing loop variable at position %d", position)
	}

	fromIndex := parser.findTopLevelKeyword(statement, "from")
	if fromIndex == -1 {
		return "", "", "", "", fmt.Errorf("missing from in for loop at position %d", position)
	}

	varName = strings.TrimSpace(statement[:fromIndex])
	if varName == "" {
		return "", "", "", "", fmt.Errorf("missing loop variable at position %d", position)
	}

	restStatement := strings.TrimSpace(statement[fromIndex+len("from"):])

	toIndex := parser.findTopLevelKeyword(restStatement, "to")
	if toIndex == -1 {
		return "", "", "", "", fmt.Errorf("missing to in for loop at position %d", position)
	}

	startExpr = strings.TrimSpace(restStatement[:toIndex])
	if startExpr == "" {
		return "", "", "", "", fmt.Errorf("invalid start expression at position %d", position)
	}

	stepStatement := strings.TrimSpace(restStatement[toIndex+len("to"):])

	stepIndex := parser.findTopLevelKeyword(stepStatement, "step")
	if stepIndex != -1 {
		endExpr = strings.TrimSpace(stepStatement[:stepIndex])
		stepExpr = strings.TrimSpace(stepStatement[stepIndex+len("step"):])
		if stepExpr == "" {
			return "", "", "", "", fmt.Errorf("invalid step expression at position %d", position)
		}
	} else {
		endExpr = stepStatement
	}

	if endExpr == "" {
		return "", "", "", "", fmt.Errorf("invalid end expression at position %d", position)
	}

	return varName, startExpr, endExpr, stepExpr, nil
}

func (parser *Parser) findTopLevelKeyword(sourceString string, keyword string) int {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0
	bracketDepth := 0

	keywordLength := len(keyword)
	for characterIndex := 0; characterIndex <= len(sourceString)-keywordLength; characterIndex++ {
		character := sourceString[characterIndex]
		if character == '\'' && !insideDoubleQuote {
			insideSingleQuote = !insideSingleQuote
			continue
		}
		if character == '"' && !insideSingleQuote {
			insideDoubleQuote = !insideDoubleQuote
			continue
		}
		if insideSingleQuote || insideDoubleQuote {
			continue
		}

		if character == '(' {
			parenthesisDepth++
			continue
		}
		if character == ')' {
			parenthesisDepth--
			continue
		}
		if character == '[' {
			bracketDepth++
			continue
		}
		if character == ']' {
			bracketDepth--
			continue
		}
		if parenthesisDepth > 0 || bracketDepth > 0 {
			continue
		}

		if strings.HasPrefix(sourceString[characterIndex:], keyword) {
			beforeIsBoundary := characterIndex == 0 || isWhitespaceChar(sourceString[characterIndex-1])
			afterIndex := characterIndex + keywordLength
			afterIsBoundary := afterIndex >= len(sourceString) || isWhitespaceChar(sourceString[afterIndex])

			if beforeIsBoundary && afterIsBoundary {
				return characterIndex
			}
		}
	}
	return -1
}

func (parser *Parser) parseMacro(macroToken Token, cursor *parserCursor, metadata map[string]any) (Node, error) {
	rawValue := strings.TrimSpace(macroToken.Value[len("macro "):])
	openParenIndex := strings.IndexByte(rawValue, '(')
	closeParenIndex := strings.IndexByte(rawValue, ')')

	var macroName string
	var macroParameters []string

	if openParenIndex != -1 && closeParenIndex > openParenIndex {
		macroName = strings.TrimSpace(rawValue[:openParenIndex])
		argumentsString := strings.TrimSpace(rawValue[openParenIndex+1 : closeParenIndex])
		if argumentsString != "" {
			for _, argument := range strings.Split(argumentsString, ",") {
				macroParameters = append(macroParameters, strings.TrimSpace(argument))
			}
		}
	} else {
		macroName = rawValue
	}

	body, err := parser.parseBlock(cursor, TokenEndMacro, metadata)
	if err != nil {
		return nil, err
	}

	if cursor.hasNext() && cursor.peek().Type == TokenEndMacro {
		cursor.next()
	} else {
		return nil, fmt.Errorf("missing closing |/macro| for macro %q", macroName)
	}

	return &MacroNode{
		Name:       macroName,
		Parameters: macroParameters,
		Body:       body,
	}, nil
}

func (parser *Parser) parseCallMacro(callToken Token) (Node, error) {
	rawValue := strings.TrimSpace(callToken.Value[len("call "):])
	openParenIndex := strings.IndexByte(rawValue, '(')
	closeParenIndex := strings.LastIndexByte(rawValue, ')')

	var macroName string
	var argumentExpressions []string

	if openParenIndex != -1 && closeParenIndex > openParenIndex {
		macroName = strings.TrimSpace(rawValue[:openParenIndex])
		argumentsString := strings.TrimSpace(rawValue[openParenIndex+1 : closeParenIndex])
		if argumentsString != "" {
			argumentExpressions = parser.evaluator.splitByTopLevelComma(argumentsString)
		}
	} else {
		macroName = rawValue
	}

	return &CallNode{
		MacroName:           macroName,
		ArgumentExpressions: argumentExpressions,
		Evaluator:           parser.evaluator,
	}, nil
}

func (parser *Parser) parseFragment(fragmentToken Token, cursor *parserCursor, metadata map[string]any, loopDepth int) (Node, error) {
	fragmentName := strings.TrimSpace(fragmentToken.Value[len("fragment "):])
	body, err := parser.parseBlockWithLoopDepth(cursor, TokenEndFragment, metadata, loopDepth)
	if err != nil {
		return nil, err
	}

	if cursor.hasNext() && cursor.peek().Type == TokenEndFragment {
		cursor.next()
	} else {
		return nil, fmt.Errorf("missing closing |/fragment| for fragment %q", fragmentName)
	}

	return &FragmentNode{
		Name: fragmentName,
		Body: body,
	}, nil
}

func (parser *Parser) parseMinify(minifyToken Token, cursor *parserCursor, metadata map[string]any, loopDepth int) (Node, error) {
	body, err := parser.parseBlockWithLoopDepth(cursor, TokenEndMinify, metadata, loopDepth)
	if err != nil {
		return nil, err
	}

	if cursor.hasNext() && cursor.peek().Type == TokenEndMinify {
		cursor.next()
	} else {
		return nil, fmt.Errorf("missing closing |/minify|")
	}

	return &MinifyNode{Body: body}, nil
}

func (parser *Parser) parsePageMetadata(token Token, metadata map[string]any) {
	rawValue := strings.TrimSpace(token.Value[len("page "):])
	equalsIndex := strings.IndexByte(rawValue, '=')
	if equalsIndex != -1 {
		key := strings.TrimSpace(rawValue[:equalsIndex])
		valueString := strings.TrimSpace(rawValue[equalsIndex+1:])
		parsedValue := parser.parseMetadataValue(valueString)
		metadata[key] = parsedValue
	}
}

func (parser *Parser) parseMetadataValue(str string) any {
	if (strings.HasPrefix(str, "\"") && strings.HasSuffix(str, "\"")) ||
		(strings.HasPrefix(str, "'") && strings.HasSuffix(str, "'")) {
		return str[1 : len(str)-1]
	}
	if strings.EqualFold(str, "true") {
		return true
	}
	if strings.EqualFold(str, "false") {
		return false
	}
	if strings.HasPrefix(str, "[") && strings.HasSuffix(str, "]") {
		innerString := strings.TrimSpace(str[1 : len(str)-1])
		if innerString == "" {
			return []string{}
		}
		var parsedItems []string
		for _, item := range strings.Split(innerString, ",") {
			trimmedItem := strings.TrimSpace(item)
			if (strings.HasPrefix(trimmedItem, "\"") && strings.HasSuffix(trimmedItem, "\"")) ||
				(strings.HasPrefix(trimmedItem, "'") && strings.HasSuffix(trimmedItem, "'")) {
				trimmedItem = trimmedItem[1 : len(trimmedItem)-1]
			}
			parsedItems = append(parsedItems, trimmedItem)
		}
		return parsedItems
	}
	if parsedInt, err := strconv.Atoi(str); err == nil {
		return parsedInt
	}
	return str
}

func (parser *Parser) parseAttempt(attemptToken Token, cursor *parserCursor, metadata map[string]any, loopDepth int) (Node, error) {
	body, err := parser.parseBlockWithLoopDepth(cursor, TokenRecover, metadata, loopDepth)
	if err != nil {
		return nil, err
	}

	if !cursor.hasNext() || cursor.peek().Type != TokenRecover {
		return nil, fmt.Errorf("missing matching |recover| block inside |attempt|")
	}

	recoverToken := cursor.next()
	var errorVarName string
	rawValue := strings.TrimSpace(recoverToken.Value[len("recover"):])
	if strings.HasPrefix(rawValue, "as ") {
		errorVarName = strings.TrimSpace(rawValue[len("as "):])
	}

	recoverBlock, err := parser.parseBlockWithLoopDepth(cursor, TokenEndAttempt, metadata, loopDepth)
	if err != nil {
		return nil, err
	}

	if cursor.hasNext() && cursor.peek().Type == TokenEndAttempt {
		cursor.next()
	} else {
		return nil, fmt.Errorf("missing closing |/attempt|")
	}

	return &AttemptNode{
		Body:         body,
		RecoverBlock: recoverBlock,
		ErrorVarName: errorVarName,
	}, nil
}

func (parser *Parser) skipWhitespaceAndComments(cursor *parserCursor) {
	for cursor.hasNext() {
		token := cursor.peek()
		if token.Type == TokenText && strings.TrimSpace(token.Value) == "" {
			cursor.next()
		} else if token.Type == TokenComment {
			cursor.next()
		} else {
			break
		}
	}
}

func (parser *Parser) parseInclude(token Token) (Node, error) {
	rawValue := strings.TrimSpace(token.Value[len("include "):])
	if rawValue == "" {
		return nil, fmt.Errorf("|include| template name must not be empty at %d", token.Position)
	}

	withIndex := parser.findIncludeWithIndex(rawValue)
	if withIndex == -1 {
		return &IncludeNode{
			TemplateName: rawValue,
			Evaluator:    parser.evaluator,
		}, nil
	}

	templateName := strings.TrimSpace(rawValue[:withIndex])
	modelExpression := strings.TrimSpace(rawValue[withIndex+len(" with "):])
	if templateName == "" {
		return nil, fmt.Errorf("|include| template name must not be empty at %d", token.Position)
	}
	if modelExpression == "" {
		return nil, fmt.Errorf("|include ... with| expression must not be empty at %d", token.Position)
	}

	return &IncludeNode{
		TemplateName:    templateName,
		ModelExpression: modelExpression,
		Evaluator:       parser.evaluator,
	}, nil
}

func (parser *Parser) findIncludeWithIndex(sourceString string) int {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0

	for characterIndex := 0; characterIndex <= len(sourceString)-len(" with "); characterIndex++ {
		character := sourceString[characterIndex]
		if character == '\'' && !insideDoubleQuote {
			insideSingleQuote = !insideSingleQuote
			continue
		}
		if character == '"' && !insideSingleQuote {
			insideDoubleQuote = !insideDoubleQuote
			continue
		}
		if insideSingleQuote || insideDoubleQuote {
			continue
		}
		if character == '(' {
			parenthesisDepth++
			continue
		}
		if character == ')' {
			parenthesisDepth--
			continue
		}

		if parenthesisDepth == 0 && strings.HasPrefix(sourceString[characterIndex:], " with ") {
			return characterIndex
		}
	}
	return -1
}

func (parser *Parser) parseComponent(compToken Token, cursor *parserCursor, metadata map[string]any, loopDepth int) (Node, error) {
	compName := strings.TrimSpace(compToken.Value[len("component "):])
	if compName == "" {
		return nil, fmt.Errorf("component template name must not be empty at %d", compToken.Position)
	}

	slots := make(map[string]Node)
	for cursor.hasNext() && cursor.peek().Type != TokenEndComponent {
		parser.skipWhitespaceAndComments(cursor)
		if !cursor.hasNext() || cursor.peek().Type == TokenEndComponent {
			break
		}

		token := cursor.peek()
		if token.Type != TokenSlot {
			return nil, fmt.Errorf("unexpected token %q outside slot blocks in component at %d", token.Value, token.Position)
		}
		cursor.next()

		slotName := strings.TrimSpace(token.Value[len("slot "):])
		if slotName == "" {
			return nil, fmt.Errorf("slot name must not be empty at %d", token.Position)
		}

		slotBody, err := parser.parseBlockWithLoopDepth(cursor, TokenEndSlot, metadata, loopDepth)
		if err != nil {
			return nil, err
		}

		if cursor.hasNext() && cursor.peek().Type == TokenEndSlot {
			cursor.next()
		} else {
			return nil, fmt.Errorf("missing closing |/slot| for slot %q", slotName)
		}

		slots[slotName] = slotBody
	}

	if cursor.hasNext() && cursor.peek().Type == TokenEndComponent {
		cursor.next()
	} else {
		return nil, fmt.Errorf("missing closing |/component| for component %q", compName)
	}

	return &ComponentNode{
		ComponentName: compName,
		Slots:         slots,
	}, nil
}

func (parser *Parser) parseSwitch(switchToken Token, cursor *parserCursor, metadata map[string]any, loopDepth int) (Node, error) {
	expressionText := strings.TrimSpace(strings.TrimPrefix(switchToken.Value, "switch"))
	if expressionText == "" {
		return nil, fmt.Errorf("switch expression must not be empty at position %d", switchToken.Position)
	}

	var clauses []SwitchClause
	hasDefaultClause := false
	seenFirstClause := false

	for cursor.hasNext() {
		token := cursor.peek()

		if token.Type == TokenEndSwitch {
			break
		}

		if token.Type == TokenCase || token.Type == TokenDefault {
			seenFirstClause = true
			cursor.next()

			var clauseKind SwitchClauseKind
			var caseExpression string
			if token.Type == TokenCase {
				clauseKind = SwitchCaseClause
				caseExpression = strings.TrimSpace(strings.TrimPrefix(token.Value, "case"))
				if caseExpression == "" {
					return nil, fmt.Errorf("case expression must not be empty at position %d", token.Position)
				}
			} else {
				clauseKind = SwitchDefaultClause
				if hasDefaultClause {
					return nil, fmt.Errorf("switch cannot contain more than one default clause at position %d", token.Position)
				}
				hasDefaultClause = true
			}

			clauseBody, fallthroughPos, hasFallthrough, err := parser.parseSwitchClauseBody(cursor, metadata, loopDepth)
			if err != nil {
				return nil, err
			}

			clauses = append(clauses, SwitchClause{
				Kind:              clauseKind,
				Expression:        caseExpression,
				Body:              clauseBody,
				AllowsFallthrough: hasFallthrough,
				FallthroughPos:    fallthroughPos,
				SourcePosition:    token.Position,
			})
			continue
		}

		if !seenFirstClause {
			if token.Type == TokenText {
				if strings.TrimSpace(token.Value) != "" {
					return nil, fmt.Errorf("unexpected content before first switch clause at position %d", token.Position)
				}
				cursor.next()
				continue
			}
			if token.Type == TokenComment {
				cursor.next()
				continue
			}
			return nil, fmt.Errorf("unexpected content before first switch clause at position %d", token.Position)
		}

		return nil, fmt.Errorf("misplaced directive |%s| inside switch at position %d", token.Value, token.Position)
	}

	if cursor.hasNext() && cursor.peek().Type == TokenEndSwitch {
		cursor.next()
	} else {
		return nil, fmt.Errorf("missing closing |/switch| for switch at position %d", switchToken.Position)
	}

	if len(clauses) > 0 {
		lastClause := clauses[len(clauses)-1]
		if lastClause.AllowsFallthrough {
			return nil, fmt.Errorf("fallthrough cannot appear in the final switch clause at position %d", lastClause.FallthroughPos)
		}
	}

	return &SwitchNode{
		Expression:     expressionText,
		Clauses:        clauses,
		Evaluator:      parser.evaluator,
		SourcePosition: switchToken.Position,
	}, nil
}

func (parser *Parser) parseSwitchClauseBody(cursor *parserCursor, metadata map[string]any, loopDepth int) (Node, int, bool, error) {
	bodyNode, err := parser.parseBlockWithLoopDepthDirect(cursor, TokenEndSwitch, metadata, loopDepth, true)
	if err != nil {
		return nil, 0, false, err
	}

	var childNodes []Node
	if block, isBlockNode := bodyNode.(*BlockNode); isBlockNode {
		childNodes = block.Children
	} else if bodyNode != nil {
		childNodes = []Node{bodyNode}
	}

	var cleanChildren []Node
	var fallthroughItem *fallthroughNode
	fallthroughIndex := -1

	for index, child := range childNodes {
		if ft, isFallthrough := child.(*fallthroughNode); isFallthrough {
			if fallthroughItem != nil {
				return nil, ft.Position, false, fmt.Errorf("fallthrough is only allowed as the final directive of a switch clause at position %d", ft.Position)
			}
			fallthroughItem = ft
			fallthroughIndex = index
		} else {
			cleanChildren = append(cleanChildren, child)
		}
	}

	if fallthroughItem != nil {
		for index := fallthroughIndex + 1; index < len(childNodes); index++ {
			child := childNodes[index]
			if txt, isTextNode := child.(*TextNode); isTextNode {
				if strings.TrimSpace(string(txt.Value)) != "" {
					return nil, fallthroughItem.Position, false, fmt.Errorf("fallthrough is only allowed as the final directive of a switch clause at position %d", fallthroughItem.Position)
				}
			} else {
				return nil, fallthroughItem.Position, false, fmt.Errorf("fallthrough is only allowed as the final directive of a switch clause at position %d", fallthroughItem.Position)
			}
		}

		return &BlockNode{Children: cleanChildren}, fallthroughItem.Position, true, nil
	}

	return &BlockNode{Children: cleanChildren}, 0, false, nil
}

func findOutputIfIndex(sourceString string) int {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0

	for characterIndex := 0; characterIndex <= len(sourceString)-len("if"); characterIndex++ {
		character := sourceString[characterIndex]

		if character == '\'' && !insideDoubleQuote {
			insideSingleQuote = !insideSingleQuote
			continue
		}
		if character == '"' && !insideSingleQuote {
			insideDoubleQuote = !insideDoubleQuote
			continue
		}
		if insideSingleQuote || insideDoubleQuote {
			continue
		}

		if character == '(' {
			parenthesisDepth++
			continue
		}
		if character == ')' {
			parenthesisDepth--
			continue
		}
		if parenthesisDepth != 0 {
			continue
		}

		if strings.HasPrefix(sourceString[characterIndex:], "if") {
			beforeIsBoundary := characterIndex == 0 || isWhitespaceChar(sourceString[characterIndex-1])
			afterIndex := characterIndex + len("if")
			afterIsBoundary := afterIndex >= len(sourceString) || isWhitespaceChar(sourceString[afterIndex])

			if beforeIsBoundary && afterIsBoundary {
				return characterIndex
			}
		}
	}
	return -1
}

func (parser *Parser) parsePWA(token Token) (Node, error) {
	directiveValue := strings.TrimSpace(token.Value)
	if strings.HasPrefix(directiveValue, "pwa") {
		directiveValue = strings.TrimSpace(directiveValue[3:])
	}
	if strings.HasPrefix(directiveValue, "-meta") || strings.HasPrefix(directiveValue, "-tags") {
		directiveValue = strings.TrimSpace(directiveValue[5:])
	}

	attributesMap := parseKeyValuePairs(directiveValue)

	name := getAttr(attributesMap, "name", "title", "app-name", "application-name", "appName", "applicationName")
	manifest := getAttr(attributesMap, "manifest", "manifest-url", "manifestUrl", "manifest_url")
	theme := getAttr(attributesMap, "theme", "theme-color", "themeColor", "theme_color")
	icon := getAttr(attributesMap, "icon", "icons", "apple-icon", "appleIcon", "apple-touch-icon", "appleTouchIcon", "touch-icon", "touchIcon")
	sw := getAttr(attributesMap, "sw", "service-worker", "serviceWorker", "service_worker", "sw-path", "swPath")
	statusColor := getAttr(attributesMap, "statusColor", "status-color", "status_color", "status-bar-style", "statusBarStyle", "status")

	return &PWANode{
		Name:        name,
		Manifest:    manifest,
		Theme:       theme,
		Icon:        icon,
		SW:          sw,
		StatusColor: statusColor,
	}, nil
}

func getAttr(attributesMap map[string]string, keys ...string) string {
	for _, key := range keys {
		if value, exists := attributesMap[key]; exists && value != "" {
			return value
		}
	}
	return ""
}

func parseKeyValuePairs(inputString string) map[string]string {
	resultMap := make(map[string]string)
	characterIndex := 0
	for characterIndex < len(inputString) {
		for characterIndex < len(inputString) && isWhitespaceChar(inputString[characterIndex]) {
			characterIndex++
		}
		if characterIndex >= len(inputString) {
			break
		}

		equalsIndex := strings.IndexByte(inputString[characterIndex:], '=')
		if equalsIndex == -1 {
			for _, flagName := range strings.Fields(inputString[characterIndex:]) {
				resultMap[flagName] = "true"
			}
			break
		}

		rawKeySegment := strings.TrimSpace(inputString[characterIndex : characterIndex+equalsIndex])
		fieldsList := strings.Fields(rawKeySegment)
		if len(fieldsList) == 0 {
			characterIndex += equalsIndex + 1
			continue
		}

		for fieldIndex := 0; fieldIndex < len(fieldsList)-1; fieldIndex++ {
			resultMap[fieldsList[fieldIndex]] = "true"
		}
		attributeKey := fieldsList[len(fieldsList)-1]

		characterIndex += equalsIndex + 1

		for characterIndex < len(inputString) && isWhitespaceChar(inputString[characterIndex]) {
			characterIndex++
		}
		if characterIndex >= len(inputString) {
			resultMap[attributeKey] = ""
			break
		}

		var attributeValue string
		if inputString[characterIndex] == '\'' || inputString[characterIndex] == '"' {
			quoteChar := inputString[characterIndex]
			characterIndex++
			quoteEndIndex := strings.IndexByte(inputString[characterIndex:], quoteChar)
			if quoteEndIndex == -1 {
				attributeValue = inputString[characterIndex:]
				characterIndex = len(inputString)
			} else {
				attributeValue = inputString[characterIndex : characterIndex+quoteEndIndex]
				characterIndex += quoteEndIndex + 1
			}
		} else {
			startIndex := characterIndex
			for characterIndex < len(inputString) && !isWhitespaceChar(inputString[characterIndex]) {
				characterIndex++
			}
			attributeValue = inputString[startIndex:characterIndex]
		}

		if attributeKey != "" {
			resultMap[attributeKey] = attributeValue
		}
	}
	return resultMap
}

func (parser *Parser) parseHTMX(token Token) (Node, error) {
	directiveValue := strings.TrimSpace(token.Value)
	if strings.HasPrefix(directiveValue, "htmx") {
		directiveValue = strings.TrimSpace(directiveValue[4:])
	}

	attributesMap := parseKeyValuePairs(directiveValue)
	var extensionsList []string
	if extString, exists := attributesMap["ext"]; exists && extString != "" {
		for _, rawExt := range strings.Split(extString, ",") {
			if trimmedExt := strings.TrimSpace(rawExt); trimmedExt != "" {
				extensionsList = append(extensionsList, trimmedExt)
			}
		}
	}

	indicatorEnabled := false
	if indicatorValue, exists := attributesMap["indicator"]; exists {
		indicatorEnabled = indicatorValue == "true" || indicatorValue == "1" || indicatorValue == ""
	}

	return &HTMXNode{
		Src:        attributesMap["src"],
		Extensions: extensionsList,
		Config:     attributesMap["config"],
		Indicator:  indicatorEnabled,
	}, nil
}

func (parser *Parser) parseHXAttr(token Token) (Node, error) {
	directiveValue := strings.TrimSpace(token.Value)
	httpMethod := "get"
	if strings.HasPrefix(directiveValue, "htmx-post ") {
		httpMethod = "post"
		directiveValue = directiveValue[10:]
	} else if strings.HasPrefix(directiveValue, "htmx-put ") {
		httpMethod = "put"
		directiveValue = directiveValue[9:]
	} else if strings.HasPrefix(directiveValue, "htmx-delete ") {
		httpMethod = "delete"
		directiveValue = directiveValue[12:]
	} else if strings.HasPrefix(directiveValue, "htmx-patch ") {
		httpMethod = "patch"
		directiveValue = directiveValue[11:]
	} else if strings.HasPrefix(directiveValue, "htmx-get ") {
		directiveValue = directiveValue[9:]
	}

	directiveValue = strings.TrimSpace(directiveValue)

	var targetURL string
	attributesString := directiveValue

	if len(directiveValue) > 0 && (directiveValue[0] == '\'' || directiveValue[0] == '"') {
		quoteChar := directiveValue[0]
		quoteEndIndex := strings.IndexByte(directiveValue[1:], quoteChar)
		if quoteEndIndex != -1 {
			targetURL = directiveValue[1 : 1+quoteEndIndex]
			attributesString = strings.TrimSpace(directiveValue[1+quoteEndIndex+1:])
		}
	} else {
		valueParts := strings.Fields(directiveValue)
		if len(valueParts) > 0 {
			targetURL = valueParts[0]
			if len(directiveValue) > len(targetURL) {
				attributesString = strings.TrimSpace(directiveValue[len(targetURL):])
			} else {
				attributesString = ""
			}
		}
	}

	attributesMap := parseKeyValuePairs(attributesString)
	return &HXAttrNode{
		Method:    httpMethod,
		URL:       targetURL,
		Target:    attributesMap["target"],
		Swap:      attributesMap["swap"],
		Indicator: attributesMap["indicator"],
		Trigger:   attributesMap["trigger"],
	}, nil
}

func (parser *Parser) parseAlpine(token Token) (Node, error) {
	rawDirectiveValue := strings.TrimSpace(token.Value)
	if strings.HasPrefix(rawDirectiveValue, "alpinejs") {
		rawDirectiveValue = strings.TrimSpace(rawDirectiveValue[8:])
	} else if strings.HasPrefix(rawDirectiveValue, "alpine") {
		rawDirectiveValue = strings.TrimSpace(rawDirectiveValue[6:])
	} else if strings.HasPrefix(rawDirectiveValue, "reactive") {
		rawDirectiveValue = strings.TrimSpace(rawDirectiveValue[8:])
	}

	parsedOptionList, optionMap, parseErr := parseAlpineOptions(rawDirectiveValue)
	if parseErr != nil {
		return nil, parseErr
	}

	// Validate setup options
	for _, option := range parsedOptionList {
		if !SupportedAlpineSetupOptions[option.Key] {
			return nil, fmt.Errorf("invalid Alpine setup option %q; supported options are build, cloak, plugins, src, version", option.Key)
		}
	}

	// Process cloak option
	cloakEnabled := true
	if cloakValue, exists := optionMap["cloak"]; exists {
		trimmedCloakValue := strings.TrimSpace(cloakValue)
		if trimmedCloakValue == "true" || trimmedCloakValue == "1" || trimmedCloakValue == "" {
			cloakEnabled = true
		} else if trimmedCloakValue == "false" || trimmedCloakValue == "0" {
			cloakEnabled = false
		} else {
			return nil, fmt.Errorf("invalid Alpine option %q: expected true, false, 1, or 0, received %q", "cloak", cloakValue)
		}
	}

	// Process build option
	buildType := "standard"
	if buildValue, exists := optionMap["build"]; exists {
		trimmedBuildValue := strings.TrimSpace(buildValue)
		if trimmedBuildValue == "standard" || trimmedBuildValue == "csp" {
			buildType = trimmedBuildValue
		} else {
			return nil, fmt.Errorf("invalid Alpine build %q; expected \"standard\" or \"csp\"", buildValue)
		}
	}

	// Process version option
	versionString := ""
	if rawVersion, exists := optionMap["version"]; exists {
		versionString = strings.TrimSpace(rawVersion)
		if validationErr := validateAlpineVersion(versionString); validationErr != nil {
			return nil, validationErr
		}
	}

	// Process src option
	sourceURL := ""
	if rawSourceURL, exists := optionMap["src"]; exists {
		sourceURL = strings.TrimSpace(rawSourceURL)
		if validationErr := validateAlpineURL(sourceURL); validationErr != nil {
			return nil, validationErr
		}
	}

	// Process plugins option
	var pluginList []string
	seenPlugins := make(map[string]bool)
	if pluginString, exists := optionMap["plugins"]; exists && pluginString != "" {
		for _, rawPluginName := range strings.Split(pluginString, ",") {
			trimmedPluginName := strings.TrimSpace(rawPluginName)
			if trimmedPluginName == "" {
				return nil, fmt.Errorf("empty plugin name specified in Alpine plugins list")
			}
			if !SupportedAlpinePlugins[trimmedPluginName] {
				return nil, fmt.Errorf("unknown Alpine plugin %q; supported plugins are anchor, collapse, focus, intersect, mask, morph, persist, sort", trimmedPluginName)
			}
			if seenPlugins[trimmedPluginName] {
				return nil, fmt.Errorf("duplicate Alpine plugin %q requested", trimmedPluginName)
			}
			seenPlugins[trimmedPluginName] = true
			pluginList = append(pluginList, trimmedPluginName)
		}
	}

	return &AlpineNode{
		Src:     sourceURL,
		Plugins: pluginList,
		Cloak:   cloakEnabled,
		Version: versionString,
		Build:   buildType,
	}, nil
}

func (parser *Parser) parseState(token Token) (Node, error) {
	rawDirectiveValue := strings.TrimSpace(token.Value)
	if strings.HasPrefix(rawDirectiveValue, "alpine-data") {
		rawDirectiveValue = strings.TrimSpace(rawDirectiveValue[11:])
	}

	parsedOptionList, _, parseErr := parseAlpineOptions(rawDirectiveValue)
	if parseErr != nil {
		return nil, parseErr
	}

	stateMap := make(map[string]any)
	for _, option := range parsedOptionList {
		typedStateValue, conversionErr := parseAlpineStateValue(option.Key, option.Value)
		if conversionErr != nil {
			return nil, conversionErr
		}
		stateMap[option.Key] = typedStateValue
	}

	return &StateNode{
		StateMap: stateMap,
	}, nil
}

func (parser *Parser) parseAlpineAttr(token Token) (Node, error) {
	rawDirectiveValue := strings.TrimSpace(token.Value)
	partTokens := strings.SplitN(rawDirectiveValue, " ", 2)

	fullDirectiveName := partTokens[0]
	expressionString := ""
	if len(partTokens) > 1 {
		expressionString = strings.TrimSpace(partTokens[1])
		if len(expressionString) > 1 && ((expressionString[0] == '\'' && expressionString[len(expressionString)-1] == '\'') || (expressionString[0] == '"' && expressionString[len(expressionString)-1] == '"')) {
			expressionString = expressionString[1 : len(expressionString)-1]
		}
	}

	baseDirectiveName := fullDirectiveName
	if dotIndex := strings.IndexByte(fullDirectiveName, '.'); dotIndex != -1 {
		baseDirectiveName = fullDirectiveName[:dotIndex]
	}

	if !SupportedAlpineElementDirectives[baseDirectiveName] {
		closestMatch := findClosestDirective(baseDirectiveName)
		if closestMatch != "" {
			return nil, fmt.Errorf("unknown Alpine directive %q; did you mean %q?", fullDirectiveName, closestMatch)
		}
		return nil, fmt.Errorf("unknown Alpine directive %q", fullDirectiveName)
	}

	// Enforce directive constraints
	if baseDirectiveName == "alpine-cloak" {
		if expressionString != "" {
			return nil, fmt.Errorf("Alpine directive %q does not accept a value", fullDirectiveName)
		}
	} else if baseDirectiveName == "alpine-show" || baseDirectiveName == "alpine-text" || baseDirectiveName == "alpine-html" || baseDirectiveName == "alpine-model" {
		if expressionString == "" {
			return nil, fmt.Errorf("Alpine directive %q requires an expression", baseDirectiveName)
		}
	}

	return &AlpineAttrNode{
		Directive: fullDirectiveName,
		Value:     expressionString,
	}, nil
}
