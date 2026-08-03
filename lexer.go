package pte

import (
	"fmt"
	"strings"
)

type Lexer struct{}

func NewLexer() *Lexer {
	return &Lexer{}
}

func (l *Lexer) Tokenize(template string) ([]Token, error) {
	var tokens []Token
	if template == "" {
		return tokens, nil
	}

	length := len(template)
	cursor := 0

	for cursor < length {
		pipeIndex := strings.IndexByte(template[cursor:], '|')
		if pipeIndex == -1 {
			tokens = append(tokens, Token{
				Type:     TokenText,
				Value:    template[cursor:],
				Position: cursor,
			})
			break
		}

		absolutePipeIndex := cursor + pipeIndex
		if absolutePipeIndex > cursor {
			tokens = append(tokens, Token{
				Type:     TokenText,
				Value:    template[cursor:absolutePipeIndex],
				Position: cursor,
			})
		}

		// Check for comment |-- ... --|
		if strings.HasPrefix(template[absolutePipeIndex:], "|--") {
			commentEnd := strings.Index(template[absolutePipeIndex+3:], "--|")
			if commentEnd == -1 {
				return nil, fmt.Errorf("unclosed comment starting at index %d", absolutePipeIndex)
			}
			absoluteCommentEnd := absolutePipeIndex + 3 + commentEnd
			tokens = append(tokens, Token{
				Type:     TokenComment,
				Value:    template[absolutePipeIndex+3 : absoluteCommentEnd],
				Position: absolutePipeIndex,
			})
			cursor = absoluteCommentEnd + 3
			continue
		}

		// Check for comment |# ... #| or |# ... |
		if strings.HasPrefix(template[absolutePipeIndex:], "|#") {
			isBlock := false
			commentEnd := -1
			for i := absolutePipeIndex + 2; i < len(template); i++ {
				if template[i] == '|' {
					if i > absolutePipeIndex+2 && template[i-1] == '#' {
						isBlock = true
						commentEnd = i - 1
						break
					} else {
						isBlock = false
						commentEnd = i
						break
					}
				}
			}

			if commentEnd == -1 {
				return nil, fmt.Errorf("unclosed comment starting at index %d", absolutePipeIndex)
			}

			tokens = append(tokens, Token{
				Type:     TokenComment,
				Value:    template[absolutePipeIndex+2 : commentEnd],
				Position: absolutePipeIndex,
			})
			if isBlock {
				cursor = commentEnd + 2
			} else {
				cursor = commentEnd + 1
			}
			continue
		}

		// Standard expression or directive pipe
		closingPipe := strings.IndexByte(template[absolutePipeIndex+1:], '|')
		if closingPipe == -1 {
			return nil, fmt.Errorf("missing closing pipe for expression starting at index %d", absolutePipeIndex)
		}

		absoluteClosingPipe := absolutePipeIndex + 1 + closingPipe
		content := strings.TrimSpace(template[absolutePipeIndex+1 : absoluteClosingPipe])
		tokenType := l.classifyToken(content)

		tokens = append(tokens, Token{
			Type:     tokenType,
			Value:    content,
			Position: absolutePipeIndex,
		})
		cursor = absoluteClosingPipe + 1
	}

	return tokens, nil
}

func (l *Lexer) classifyToken(content string) TokenType {
	if strings.HasPrefix(content, "if ") {
		return TokenIf
	} else if strings.HasPrefix(content, "else if ") {
		return TokenElseIf
	} else if content == "else" {
		return TokenElse
	} else if content == "/if" {
		return TokenEndIf
	} else if strings.HasPrefix(content, "each ") {
		return TokenEach
	} else if content == "/each" {
		return TokenEndEach
	} else if strings.HasPrefix(content, "for ") {
		return TokenFor
	} else if content == "/for" {
		return TokenEndFor
	} else if content == "continue" {
		return TokenContinue
	} else if content == "break" {
		return TokenBreak
	} else if content == "switch" || strings.HasPrefix(content, "switch ") {
		return TokenSwitch
	} else if content == "case" || strings.HasPrefix(content, "case ") {
		return TokenCase
	} else if content == "default" || strings.HasPrefix(content, "default ") {
		return TokenDefault
	} else if content == "fallthrough" || strings.HasPrefix(content, "fallthrough ") {
		return TokenFallthrough
	} else if content == "/switch" {
		return TokenEndSwitch
	} else if strings.HasPrefix(content, "include ") {
		return TokenInclude
	} else if strings.HasPrefix(content, "layout ") {
		return TokenLayout
	} else if strings.HasPrefix(content, "section ") {
		return TokenSection
	} else if content == "/section" {
		return TokenEndSection
	} else if strings.HasPrefix(content, "yield ") {
		return TokenYield
	} else if strings.HasPrefix(content, "component ") {
		return TokenComponent
	} else if content == "/component" {
		return TokenEndComponent
	} else if strings.HasPrefix(content, "slot ") {
		return TokenSlot
	} else if content == "/slot" {
		return TokenEndSlot
	} else if strings.HasPrefix(content, "model ") {
		return TokenModel
	} else if strings.HasPrefix(content, "field ") {
		return TokenField
	} else if strings.HasPrefix(content, "display ") {
		return TokenDisplay
	} else if strings.HasPrefix(content, "editor ") {
		return TokenEditor
	} else if strings.HasPrefix(content, "macro ") {
		return TokenMacro
	} else if content == "/macro" {
		return TokenEndMacro
	} else if strings.HasPrefix(content, "call ") {
		return TokenCall
	} else if content == "separator" {
		return TokenSeparator
	} else if content == "/separator" {
		return TokenEndSeparator
	} else if strings.HasPrefix(content, "fragment ") {
		return TokenFragment
	} else if content == "/fragment" {
		return TokenEndFragment
	} else if content == "minify" {
		return TokenMinify
	} else if content == "/minify" {
		return TokenEndMinify
	} else if strings.HasPrefix(content, "page ") {
		return TokenPage
	} else if content == "attempt" {
		return TokenAttempt
	} else if strings.HasPrefix(content, "recover") {
		return TokenRecover
	} else if content == "/attempt" {
		return TokenEndAttempt
	} else if content == "pwa" || strings.HasPrefix(content, "pwa ") {
		return TokenPWA
	} else if content == "htmx" || strings.HasPrefix(content, "htmx ") {
		return TokenHTMX
	} else if strings.HasPrefix(content, "htmx-get ") || strings.HasPrefix(content, "htmx-post ") || strings.HasPrefix(content, "htmx-put ") || strings.HasPrefix(content, "htmx-delete ") || strings.HasPrefix(content, "htmx-patch ") {
		return TokenHXAttr
	} else if content == "alpine" || strings.HasPrefix(content, "alpine ") || content == "alpinejs" || strings.HasPrefix(content, "alpinejs ") || content == "reactive" || strings.HasPrefix(content, "reactive ") {
		return TokenAlpine
	} else if content == "alpine-data" || strings.HasPrefix(content, "alpine-data ") {
		return TokenState
	} else if strings.HasPrefix(content, "alpine-") {
		return TokenAlpineAttr
	}
	return TokenExpression
}
