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

func (n *LayoutNode) Render(ctx *Context, w io.Writer) error {
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

	// Render sections to string buffers
	sectionValues := make(map[string]string)
	for name, sectionBlock := range n.Sections {
		var buf bytes.Buffer
		if err := sectionBlock.Render(ctx, &buf); err != nil {
			return err
		}
		sectionValues[name] = buf.String()
	}

	subContext := ctx.With("_sections", sectionValues)
	return engine.renderNamedTemplate(w, n.LayoutName, subContext)
}

func (p *Parser) Parse(tokens []Token) (*CompiledTemplate, error) {
	cursor := &parserCursor{tokens: tokens}
	metadata := make(map[string]any)

	// Skip leading whitespace/comments
	p.skipWhitespaceAndComments(cursor)

	// Check if the template starts with |layout templateName|
	if cursor.hasNext() && cursor.peek().Type == TokenLayout {
		layoutToken := cursor.next()
		layoutName := strings.TrimSpace(layoutToken.Value[len("layout "):])
		if layoutName == "" {
			return nil, fmt.Errorf("layout template name must not be empty at %d", layoutToken.Position)
		}

		sections := make(map[string]Node)
		for cursor.hasNext() {
			p.skipWhitespaceAndComments(cursor)
			if !cursor.hasNext() {
				break
			}

			tok := cursor.peek()
			if tok.Type != TokenSection {
				return nil, fmt.Errorf("unexpected token %q outside section blocks in layout page at %d", tok.Value, tok.Position)
			}
			cursor.next()

			secName := strings.TrimSpace(tok.Value[len("section "):])
			if secName == "" {
				return nil, fmt.Errorf("section name must not be empty at %d", tok.Position)
			}

			secBody, err := p.parseBlock(cursor, TokenEndSection, metadata)
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

	rootBlock, err := p.parseBlock(cursor, "", metadata)
	if err != nil {
		return nil, err
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

func (c *parserCursor) hasNext() bool {
	return c.index < len(c.tokens)
}

func (c *parserCursor) peek() Token {
	if !c.hasNext() {
		return Token{}
	}
	return c.tokens[c.index]
}

func (c *parserCursor) next() Token {
	t := c.peek()
	c.index++
	return t
}

func (p *Parser) parseBlock(cursor *parserCursor, stopToken TokenType, metadata map[string]any) (Node, error) {
	var nodes []Node

	for cursor.hasNext() {
		token := cursor.peek()

		if stopToken != "" && token.Type == stopToken {
			break
		}

		if token.Type == TokenElse || token.Type == TokenElseIf || token.Type == TokenRecover || token.Type == TokenCase || token.Type == TokenDefault {
			break
		}

		cursor.next()

		switch token.Type {
		case TokenText:
			nodes = append(nodes, NewTextNode(token.Value))
		case TokenComment:
			// Ignore comments
		case TokenExpression:
			exprNode, err := p.parseExpression(token.Value)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, exprNode)
		case TokenIf:
			ifNode, err := p.parseIf(token, cursor, metadata)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, ifNode)
		case TokenEach:
			eachNode, err := p.parseEach(token, cursor, metadata)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, eachNode)
		case TokenModel:
			modelType := strings.TrimSpace(token.Value[len("model "):])
			nodes = append(nodes, &ModelNode{ModelType: modelType})
		case TokenField:
			propertyPath := strings.TrimSpace(token.Value[len("field "):])
			nodes = append(nodes, &FieldNode{PropertyPath: propertyPath, Evaluator: p.evaluator})
		case TokenDisplay:
			propertyPath := strings.TrimSpace(token.Value[len("display "):])
			nodes = append(nodes, &DisplayNode{PropertyPath: propertyPath, Evaluator: p.evaluator})
		case TokenEditor:
			propertyPath := strings.TrimSpace(token.Value[len("editor "):])
			nodes = append(nodes, &EditorNode{PropertyPath: propertyPath, Evaluator: p.evaluator})
		case TokenMacro:
			macroNode, err := p.parseMacro(token, cursor, metadata)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, macroNode)
		case TokenCall:
			callNode, err := p.parseCallMacro(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, callNode)
		case TokenSeparator:
			sepBody, err := p.parseBlock(cursor, TokenEndSeparator, metadata)
			if err != nil {
				return nil, err
			}
			if cursor.hasNext() && cursor.peek().Type == TokenEndSeparator {
				cursor.next()
			}
			nodes = append(nodes, &SeparatorNode{Body: sepBody})
		case TokenFragment:
			fragNode, err := p.parseFragment(token, cursor, metadata)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, fragNode)
		case TokenMinify:
			minNode, err := p.parseMinify(token, cursor, metadata)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, minNode)
		case TokenPage:
			p.parsePageMetadata(token, metadata)
		case TokenAttempt:
			attNode, err := p.parseAttempt(token, cursor, metadata)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, attNode)
		case TokenInclude:
			incNode, err := p.parseInclude(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, incNode)
		case TokenYield:
			secName := strings.TrimSpace(token.Value[len("yield "):])
			nodes = append(nodes, &YieldNode{SectionName: secName})
		case TokenComponent:
			compNode, err := p.parseComponent(token, cursor, metadata)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, compNode)
		case TokenSlot:
			slotName := strings.TrimSpace(token.Value[len("slot "):])
			nodes = append(nodes, &SlotNode{SlotName: slotName})
		case TokenSwitch:
			swNode, err := p.parseSwitch(token, cursor, metadata)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, swNode)
		case TokenFallthrough:
			nodes = append(nodes, &fallthroughNode{})
		case TokenPWA:
			pwaNode, err := p.parsePWA(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, pwaNode)
		case TokenHTMX:
			htmxNode, err := p.parseHTMX(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, htmxNode)
		case TokenHXAttr:
			hxAttrNode, err := p.parseHXAttr(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, hxAttrNode)
		case TokenAlpine:
			alpineNode, err := p.parseAlpine(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, alpineNode)
		case TokenState:
			stateNode, err := p.parseState(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, stateNode)
		case TokenAlpineAttr:
			alpineAttrNode, err := p.parseAlpineAttr(token)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, alpineAttrNode)
		default:
			exprNode, err := p.parseExpression(token.Value)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, exprNode)
		}
	}

	return &BlockNode{Children: nodes}, nil
}

func (p *Parser) parseExpression(val string) (Node, error) {
	trimmed := strings.TrimSpace(val)

	var condition string
	ifIdx := findOutputIfIndex(trimmed)
	if ifIdx != -1 {
		condition = strings.TrimSpace(trimmed[ifIdx+2:])
		trimmed = strings.TrimSpace(trimmed[:ifIdx])
	}

	var mode OutputMode
	var expr string

	if strings.HasPrefix(trimmed, "html ") {
		mode = ModeTrustedHtml
		expr = strings.TrimSpace(trimmed[len("html "):])
	} else if strings.HasPrefix(trimmed, "attr ") {
		mode = ModeAttributeEscaped
		expr = strings.TrimSpace(trimmed[len("attr "):])
	} else if strings.HasPrefix(trimmed, "url ") {
		mode = ModeUrlEncoded
		expr = strings.TrimSpace(trimmed[len("url "):])
	} else if strings.HasPrefix(trimmed, "json ") {
		mode = ModeJsonEncoded
		expr = strings.TrimSpace(trimmed[len("json "):])
	} else {
		mode = ModeHtmlEscaped
		expr = trimmed
	}

	return &ExpressionNode{
		Expression: expr,
		Mode:       mode,
		Evaluator:  p.evaluator,
		Condition:  condition,
	}, nil
}

func (p *Parser) parseIf(ifToken Token, cursor *parserCursor, metadata map[string]any) (Node, error) {
	condition := strings.TrimSpace(ifToken.Value[len("if "):])
	thenBlock, err := p.parseBlock(cursor, TokenEndIf, metadata)
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
			body, err := p.parseBlock(cursor, TokenEndIf, metadata)
			if err != nil {
				return nil, err
			}
			elseIfBranches = append(elseIfBranches, ElseIfBranch{Condition: cond, Block: body})
		} else if current.Type == TokenElse {
			cursor.next()
			var err error
			elseBlock, err = p.parseBlock(cursor, TokenEndIf, metadata)
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
		Evaluator:      p.evaluator,
	}, nil
}

func (p *Parser) parseEach(eachToken Token, cursor *parserCursor, metadata map[string]any) (Node, error) {
	statement := strings.TrimSpace(eachToken.Value[len("each "):])
	inIndex := strings.Index(statement, " in ")
	if inIndex == -1 {
		return nil, fmt.Errorf("invalid each statement format. Expected '|each item in items|' at %d", eachToken.Position)
	}

	leftSide := strings.TrimSpace(statement[:inIndex])
	collectionExpr := strings.TrimSpace(statement[inIndex+4:])

	bodyBlock, err := p.parseBlock(cursor, TokenEndEach, metadata)
	if err != nil {
		return nil, err
	}

	var elseBlock Node
	if cursor.hasNext() && cursor.peek().Type == TokenElse {
		cursor.next()
		elseBlock, err = p.parseBlock(cursor, TokenEndEach, metadata)
		if err != nil {
			return nil, err
		}
	}

	if cursor.hasNext() && cursor.peek().Type == TokenEndEach {
		cursor.next()
	} else {
		return nil, fmt.Errorf("missing closing |/each| for loop at %d", eachToken.Position)
	}

	// Separate SeparatorNode from body block children if present
	var separatorNode Node
	if block, ok := bodyBlock.(*BlockNode); ok {
		var bodyChildren []Node
		for _, child := range block.Children {
			if sep, ok := child.(*SeparatorNode); ok {
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
		Evaluator:            p.evaluator,
		IsMapLoop:            isMapLoop,
	}, nil
}

func (p *Parser) parseMacro(macroToken Token, cursor *parserCursor, metadata map[string]any) (Node, error) {
	val := strings.TrimSpace(macroToken.Value[len("macro "):])
	openParen := strings.IndexByte(val, '(')
	closeParen := strings.IndexByte(val, ')')

	var name string
	var params []string

	if openParen != -1 && closeParen > openParen {
		name = strings.TrimSpace(val[:openParen])
		argsStr := strings.TrimSpace(val[openParen+1 : closeParen])
		if argsStr != "" {
			for _, arg := range strings.Split(argsStr, ",") {
				params = append(params, strings.TrimSpace(arg))
			}
		}
	} else {
		name = val
	}

	body, err := p.parseBlock(cursor, TokenEndMacro, metadata)
	if err != nil {
		return nil, err
	}

	if cursor.hasNext() && cursor.peek().Type == TokenEndMacro {
		cursor.next()
	} else {
		return nil, fmt.Errorf("missing closing |/macro| for macro %q", name)
	}

	return &MacroNode{
		Name:       name,
		Parameters: params,
		Body:       body,
	}, nil
}

func (p *Parser) parseCallMacro(callToken Token) (Node, error) {
	val := strings.TrimSpace(callToken.Value[len("call "):])
	openParen := strings.IndexByte(val, '(')
	closeParen := strings.LastIndexByte(val, ')')

	var name string
	var args []string

	if openParen != -1 && closeParen > openParen {
		name = strings.TrimSpace(val[:openParen])
		argsStr := strings.TrimSpace(val[openParen+1 : closeParen])
		if argsStr != "" {
			// Split by top level comma inside macro args
			args = p.evaluator.splitByTopLevelComma(argsStr)
		}
	} else {
		name = val
	}

	return &CallNode{
		MacroName:           name,
		ArgumentExpressions: args,
		Evaluator:           p.evaluator,
	}, nil
}

func (p *Parser) parseFragment(fragmentToken Token, cursor *parserCursor, metadata map[string]any) (Node, error) {
	name := strings.TrimSpace(fragmentToken.Value[len("fragment "):])
	body, err := p.parseBlock(cursor, TokenEndFragment, metadata)
	if err != nil {
		return nil, err
	}

	if cursor.hasNext() && cursor.peek().Type == TokenEndFragment {
		cursor.next()
	} else {
		return nil, fmt.Errorf("missing closing |/fragment| for fragment %q", name)
	}

	return &FragmentNode{
		Name: name,
		Body: body,
	}, nil
}

func (p *Parser) parseMinify(minifyToken Token, cursor *parserCursor, metadata map[string]any) (Node, error) {
	body, err := p.parseBlock(cursor, TokenEndMinify, metadata)
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

func (p *Parser) parsePageMetadata(token Token, metadata map[string]any) {
	val := strings.TrimSpace(token.Value[len("page "):])
	eqIndex := strings.IndexByte(val, '=')
	if eqIndex != -1 {
		key := strings.TrimSpace(val[:eqIndex])
		valueStr := strings.TrimSpace(val[eqIndex+1:])
		value := p.parseMetadataValue(valueStr)
		metadata[key] = value
	}
}

func (p *Parser) parseMetadataValue(str string) any {
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
		inner := strings.TrimSpace(str[1 : len(str)-1])
		if inner == "" {
			return []string{}
		}
		var items []string
		for _, item := range strings.Split(inner, ",") {
			trimmed := strings.TrimSpace(item)
			if (strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"")) ||
				(strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'")) {
				trimmed = trimmed[1 : len(trimmed)-1]
			}
			items = append(items, trimmed)
		}
		return items
	}
	if i, err := strconv.Atoi(str); err == nil {
		return i
	}
	return str
}

func (p *Parser) parseAttempt(attemptToken Token, cursor *parserCursor, metadata map[string]any) (Node, error) {
	body, err := p.parseBlock(cursor, TokenRecover, metadata)
	if err != nil {
		return nil, err
	}

	if !cursor.hasNext() || cursor.peek().Type != TokenRecover {
		return nil, fmt.Errorf("missing matching |recover| block inside |attempt|")
	}

	recoverToken := cursor.next()
	var errorVarName string
	val := strings.TrimSpace(recoverToken.Value[len("recover"):])
	if strings.HasPrefix(val, "as ") {
		errorVarName = strings.TrimSpace(val[len("as "):])
	}

	recoverBlock, err := p.parseBlock(cursor, TokenEndAttempt, metadata)
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

func (p *Parser) skipWhitespaceAndComments(cursor *parserCursor) {
	for cursor.hasNext() {
		tok := cursor.peek()
		if tok.Type == TokenText && strings.TrimSpace(tok.Value) == "" {
			cursor.next()
		} else if tok.Type == TokenComment {
			cursor.next()
		} else {
			break
		}
	}
}

func (p *Parser) parseInclude(token Token) (Node, error) {
	body := strings.TrimSpace(token.Value[len("include "):])
	if body == "" {
		return nil, fmt.Errorf("|include| template name must not be empty at %d", token.Position)
	}

	withIdx := p.findIncludeWithIndex(body)
	if withIdx == -1 {
		return &IncludeNode{
			TemplateName: body,
			Evaluator:    p.evaluator,
		}, nil
	}

	templateName := strings.TrimSpace(body[:withIdx])
	modelExpr := strings.TrimSpace(body[withIdx+len(" with "):])
	if templateName == "" {
		return nil, fmt.Errorf("|include| template name must not be empty at %d", token.Position)
	}
	if modelExpr == "" {
		return nil, fmt.Errorf("|include ... with| expression must not be empty at %d", token.Position)
	}

	return &IncludeNode{
		TemplateName:    templateName,
		ModelExpression: modelExpr,
		Evaluator:       p.evaluator,
	}, nil
}

func (p *Parser) findIncludeWithIndex(body string) int {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0

	for i := 0; i <= len(body)-len(" with "); i++ {
		current := body[i]
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

		if parenthesisDepth == 0 && strings.HasPrefix(body[i:], " with ") {
			return i
		}
	}
	return -1
}

func (p *Parser) parseComponent(compToken Token, cursor *parserCursor, metadata map[string]any) (Node, error) {
	compName := strings.TrimSpace(compToken.Value[len("component "):])
	if compName == "" {
		return nil, fmt.Errorf("component template name must not be empty at %d", compToken.Position)
	}

	slots := make(map[string]Node)
	for cursor.hasNext() && cursor.peek().Type != TokenEndComponent {
		p.skipWhitespaceAndComments(cursor)
		if !cursor.hasNext() || cursor.peek().Type == TokenEndComponent {
			break
		}

		tok := cursor.peek()
		if tok.Type != TokenSlot {
			return nil, fmt.Errorf("unexpected token %q outside slot blocks in component at %d", tok.Value, tok.Position)
		}
		cursor.next()

		slotName := strings.TrimSpace(tok.Value[len("slot "):])
		if slotName == "" {
			return nil, fmt.Errorf("slot name must not be empty at %d", tok.Position)
		}

		slotBody, err := p.parseBlock(cursor, TokenEndSlot, metadata)
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

func (p *Parser) parseSwitch(switchToken Token, cursor *parserCursor, metadata map[string]any) (Node, error) {
	expr := strings.TrimSpace(switchToken.Value[len("switch "):])

	var cases []CaseBlock
	var defaultBlock Node

	for cursor.hasNext() && cursor.peek().Type != TokenEndSwitch {
		p.skipWhitespaceAndComments(cursor)
		if !cursor.hasNext() || cursor.peek().Type == TokenEndSwitch {
			break
		}

		tok := cursor.peek()
		if tok.Type == TokenCase {
			cursor.next()
			caseExpr := strings.TrimSpace(tok.Value[len("case "):])

			caseBody, err := p.parseBlock(cursor, TokenEndSwitch, metadata)
			if err != nil {
				return nil, err
			}

			hasFallthrough := false
			if block, ok := caseBody.(*BlockNode); ok {
				var cleanChildren []Node
				for _, child := range block.Children {
					if _, ok := child.(*fallthroughNode); ok {
						hasFallthrough = true
					} else {
						cleanChildren = append(cleanChildren, child)
					}
				}
				caseBody = &BlockNode{Children: cleanChildren}
			}

			cases = append(cases, CaseBlock{
				Expression:  caseExpr,
				Body:        caseBody,
				Fallthrough: hasFallthrough,
			})
		} else if tok.Type == TokenDefault {
			cursor.next()
			body, err := p.parseBlock(cursor, TokenEndSwitch, metadata)
			if err != nil {
				return nil, err
			}

			if block, ok := body.(*BlockNode); ok {
				var cleanChildren []Node
				for _, child := range block.Children {
					if _, ok := child.(*fallthroughNode); !ok {
						cleanChildren = append(cleanChildren, child)
					}
				}
				body = &BlockNode{Children: cleanChildren}
			}

			defaultBlock = body
		} else {
			cursor.next()
		}
	}

	if cursor.hasNext() && cursor.peek().Type == TokenEndSwitch {
		cursor.next()
	} else {
		return nil, fmt.Errorf("missing closing |/switch|")
	}

	return &SwitchNode{
		Expression:   expr,
		Cases:        cases,
		DefaultBlock: defaultBlock,
		Evaluator:    p.evaluator,
	}, nil
}

func findOutputIfIndex(source string) int {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0

	for index := 0; index <= len(source)-len("if"); index++ {
		current := source[index]

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
		if parenthesisDepth != 0 {
			continue
		}

		if strings.HasPrefix(source[index:], "if") {
			beforeIsBoundary := index == 0 || isWhitespaceChar(source[index-1])
			afterIndex := index + len("if")
			afterIsBoundary := afterIndex >= len(source) || isWhitespaceChar(source[afterIndex])

			if beforeIsBoundary && afterIsBoundary {
				return index
			}
		}
	}
	return -1
}

func (p *Parser) parsePWA(tok Token) (Node, error) {
	val := strings.TrimSpace(tok.Value)
	if strings.HasPrefix(val, "pwa") {
		val = strings.TrimSpace(val[3:])
	}

	attrs := parseKeyValuePairs(val)
	name := attrs["name"]
	if name == "" {
		name = attrs["title"]
	}

	return &PWANode{
		Name:        name,
		Manifest:    attrs["manifest"],
		Theme:       attrs["theme"],
		Icon:        attrs["icon"],
		SW:          attrs["sw"],
		StatusColor: attrs["statusColor"],
	}, nil
}

func parseKeyValuePairs(input string) map[string]string {
	result := make(map[string]string)
	i := 0
	for i < len(input) {
		for i < len(input) && isWhitespaceChar(input[i]) {
			i++
		}
		if i >= len(input) {
			break
		}

		eqIdx := strings.IndexByte(input[i:], '=')
		if eqIdx == -1 {
			break
		}
		key := strings.TrimSpace(input[i : i+eqIdx])
		i += eqIdx + 1

		for i < len(input) && isWhitespaceChar(input[i]) {
			i++
		}
		if i >= len(input) {
			break
		}

		var val string
		if input[i] == '\'' || input[i] == '"' {
			quote := input[i]
			i++
			end := strings.IndexByte(input[i:], quote)
			if end == -1 {
				val = input[i:]
				i = len(input)
			} else {
				val = input[i : i+end]
				i += end + 1
			}
		} else {
			start := i
			for i < len(input) && !isWhitespaceChar(input[i]) {
				i++
			}
			val = input[start:i]
		}

		if key != "" {
			result[key] = val
		}
	}
	return result
}

func (p *Parser) parseHTMX(tok Token) (Node, error) {
	val := strings.TrimSpace(tok.Value)
	if strings.HasPrefix(val, "htmx") {
		val = strings.TrimSpace(val[4:])
	}

	attrs := parseKeyValuePairs(val)
	var exts []string
	if extStr, ok := attrs["ext"]; ok && extStr != "" {
		for _, e := range strings.Split(extStr, ",") {
			if trimmed := strings.TrimSpace(e); trimmed != "" {
				exts = append(exts, trimmed)
			}
		}
	}

	indicator := false
	if indVal, ok := attrs["indicator"]; ok {
		indicator = indVal == "true" || indVal == "1" || indVal == ""
	}

	return &HTMXNode{
		Src:        attrs["src"],
		Extensions: exts,
		Config:     attrs["config"],
		Indicator:  indicator,
	}, nil
}

func (p *Parser) parseHXAttr(tok Token) (Node, error) {
	val := strings.TrimSpace(tok.Value)
	method := "get"
	if strings.HasPrefix(val, "htmx-post ") {
		method = "post"
		val = val[10:]
	} else if strings.HasPrefix(val, "htmx-put ") {
		method = "put"
		val = val[9:]
	} else if strings.HasPrefix(val, "htmx-delete ") {
		method = "delete"
		val = val[12:]
	} else if strings.HasPrefix(val, "htmx-patch ") {
		method = "patch"
		val = val[11:]
	} else if strings.HasPrefix(val, "htmx-get ") {
		val = val[9:]
	}

	val = strings.TrimSpace(val)

	var urlPath string
	attrsStr := val

	if len(val) > 0 && (val[0] == '\'' || val[0] == '"') {
		quote := val[0]
		end := strings.IndexByte(val[1:], quote)
		if end != -1 {
			urlPath = val[1 : 1+end]
			attrsStr = strings.TrimSpace(val[1+end+1:])
		}
	} else {
		parts := strings.Fields(val)
		if len(parts) > 0 {
			urlPath = parts[0]
			if len(val) > len(urlPath) {
				attrsStr = strings.TrimSpace(val[len(urlPath):])
			} else {
				attrsStr = ""
			}
		}
	}

	attrs := parseKeyValuePairs(attrsStr)
	return &HXAttrNode{
		Method:    method,
		URL:       urlPath,
		Target:    attrs["target"],
		Swap:      attrs["swap"],
		Indicator: attrs["indicator"],
		Trigger:   attrs["trigger"],
	}, nil
}

func (p *Parser) parseAlpine(tok Token) (Node, error) {
	val := strings.TrimSpace(tok.Value)
	if strings.HasPrefix(val, "alpinejs") {
		val = strings.TrimSpace(val[8:])
	} else if strings.HasPrefix(val, "alpine") {
		val = strings.TrimSpace(val[6:])
	} else if strings.HasPrefix(val, "reactive") {
		val = strings.TrimSpace(val[8:])
	}

	attrs := parseKeyValuePairs(val)
	var plugins []string
	if pluginStr, ok := attrs["plugins"]; ok && pluginStr != "" {
		for _, pl := range strings.Split(pluginStr, ",") {
			if trimmed := strings.TrimSpace(pl); trimmed != "" {
				plugins = append(plugins, trimmed)
			}
		}
	}

	cloak := true
	if cVal, ok := attrs["cloak"]; ok {
		cloak = cVal == "true" || cVal == "1" || cVal == ""
	}

	return &AlpineNode{
		Src:     attrs["src"],
		Plugins: plugins,
		Cloak:   cloak,
	}, nil
}

func (p *Parser) parseState(tok Token) (Node, error) {
	val := strings.TrimSpace(tok.Value)
	if strings.HasPrefix(val, "alpine-data") {
		val = strings.TrimSpace(val[11:])
	}

	attrs := parseKeyValuePairs(val)
	return &StateNode{
		StateMap: attrs,
	}, nil
}

func (p *Parser) parseAlpineAttr(tok Token) (Node, error) {
	val := strings.TrimSpace(tok.Value)
	parts := strings.SplitN(val, " ", 2)
	dir := parts[0]
	expr := ""
	if len(parts) > 1 {
		expr = strings.TrimSpace(parts[1])
		if len(expr) > 1 && ((expr[0] == '\'' && expr[len(expr)-1] == '\'') || (expr[0] == '"' && expr[len(expr)-1] == '"')) {
			expr = expr[1 : len(expr)-1]
		}
	}
	return &AlpineAttrNode{
		Directive: dir,
		Value:     expr,
	}, nil
}
