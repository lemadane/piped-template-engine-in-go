package pte

import (
	"strings"
)

type htmlMinifierState int

const (
	stateData htmlMinifierState = iota
	stateTag
	stateDoubleQuote
	stateSingleQuote
	stateUnquoted
	stateComment
	stateRawText
)

func MinifyHTML(htmlString string) string {
	if htmlString == "" {
		return ""
	}

	cleanHTML := stripCommentsContextAware(htmlString)
	return collapseOutsideWhitespace(cleanHTML)
}

func stripCommentsContextAware(htmlString string) string {
	var sb strings.Builder
	sb.Grow(len(htmlString))

	length := len(htmlString)
	i := 0

	for i < length {
		// 1. Check for raw elements (<script>, <style>, <pre>, <textarea>)
		if htmlString[i] == '<' {
			rawTag, rawEndTag := matchRawTagStart(htmlString[i:])
			if rawTag != "" {
				i = copyRawElement(htmlString, i, rawTag, rawEndTag, &sb)
				continue
			}
		}

		// 3. Check for HTML comment start (<!--) in normal HTML data context
		if strings.HasPrefix(htmlString[i:], "<!--") {
			commentEnd := strings.Index(htmlString[i+4:], "-->")
			if commentEnd != -1 {
				// Skip comment
				i += 4 + commentEnd + 3
				continue
			} else {
				// Unterminated comment: skip remaining string
				break
			}
		}

		// 4. Check for tag start (<)
		if htmlString[i] == '<' {
			i = scanTag(htmlString, i, &sb)
			continue
		}

		// 5. Normal HTML data character
		sb.WriteByte(htmlString[i])
		i++
	}

	return sb.String()
}

func scanTag(htmlString string, startIndex int, sb *strings.Builder) int {
	length := len(htmlString)
	i := startIndex

	inQuote := byte(0)
	inUnquoted := false

	for i < length {
		if inQuote == 0 && !inUnquoted && strings.HasPrefix(htmlString[i:], "<!--") {
			commentEnd := strings.Index(htmlString[i+4:], "-->")
			if commentEnd != -1 {
				i += 4 + commentEnd + 3
				continue
			} else {
				i = length
				break
			}
		}

		b := htmlString[i]
		sb.WriteByte(b)
		i++

		if inQuote != 0 {
			if b == inQuote {
				inQuote = 0
			}
			continue
		}

		if inUnquoted {
			if isWhitespaceChar(b) {
				inUnquoted = false
			} else if b == '>' {
				inUnquoted = false
				break
			}
			continue
		}

		if b == '>' {
			break
		}

		if b == '"' || b == '\'' {
			inQuote = b
			continue
		}

		if b == '=' {
			if i < length && !isWhitespaceChar(htmlString[i]) && htmlString[i] != '"' && htmlString[i] != '\'' && htmlString[i] != '>' {
				inUnquoted = true
			}
			continue
		}
	}
	return i
}

func copyRawElement(htmlString string, startIndex int, tag, endTag string, sb *strings.Builder) int {
	length := len(htmlString)
	i := startIndex

	// 1. Copy opening tag (handling attributes & quotes inside opening tag)
	i = scanTag(htmlString, i, sb)

	// 2. Copy body until closing tag (case-insensitive)
	closingIndex := indexOfCaseInsensitive(htmlString[i:], endTag)
	if closingIndex != -1 {
		sb.WriteString(htmlString[i : i+closingIndex+len(endTag)])
		return i + closingIndex + len(endTag)
	}

	// Unterminated raw element
	sb.WriteString(htmlString[i:])
	return length
}

func matchRawTagStart(s string) (string, string) {
	rawTags := []string{"script", "style", "pre", "textarea"}
	for _, tag := range rawTags {
		tagLen := len(tag)
		if len(s) > tagLen+1 && strings.EqualFold(s[1:1+tagLen], tag) {
			nextChar := s[1+tagLen]
			if nextChar == ' ' || nextChar == '>' || nextChar == '\t' || nextChar == '\n' || nextChar == '\r' || nextChar == '/' {
				return tag, "</" + tag + ">"
			}
		}
	}
	return "", ""
}

func indexOfCaseInsensitive(s, substr string) int {
	n := len(s)
	m := len(substr)
	if m == 0 {
		return 0
	}
	if n < m {
		return -1
	}

	for i := 0; i <= n-m; i++ {
		match := true
		for j := 0; j < m; j++ {
			c1 := s[i+j]
			c2 := substr[j]
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 'a' - 'A'
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 'a' - 'A'
			}
			if c1 != c2 {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func collapseOutsideWhitespace(htmlString string) string {
	var stringBuilder strings.Builder
	stringBuilder.Grow(len(htmlString))

	length := len(htmlString)
	characterIndex := 0
	insideSpace := false

	for characterIndex < length {
		if strings.HasPrefix(htmlString[characterIndex:], "|raw|") {
			rawEnd := strings.Index(htmlString[characterIndex+5:], "|/raw|")
			if rawEnd != -1 {
				endPos := characterIndex + 5 + rawEnd + 6
				stringBuilder.WriteString(htmlString[characterIndex:endPos])
				characterIndex = endPos
				insideSpace = false
				continue
			}
		}

		if htmlString[characterIndex] == '<' {
			rawTagName, rawEndTag := matchRawTagStart(htmlString[characterIndex:])
			if rawTagName != "" {
				characterIndex = copyRawElement(htmlString, characterIndex, rawTagName, rawEndTag, &stringBuilder)
				insideSpace = false
				continue
			}

			characterIndex = scanTag(htmlString, characterIndex, &stringBuilder)
			insideSpace = false
			continue
		}

		character := htmlString[characterIndex]
		if isWhitespaceChar(character) {
			if !insideSpace {
				stringBuilder.WriteByte(' ')
				insideSpace = true
			}
			characterIndex++
		} else {
			stringBuilder.WriteByte(character)
			insideSpace = false
			characterIndex++
		}
	}

	result := stringBuilder.String()
	return collapseTagSpacesOutsideRaw(result)
}

func collapseTagSpacesOutsideRaw(htmlString string) string {
	var stringBuilder strings.Builder
	stringBuilder.Grow(len(htmlString))

	length := len(htmlString)
	characterIndex := 0

	for characterIndex < length {
		if strings.HasPrefix(htmlString[characterIndex:], "|raw|") {
			rawEnd := strings.Index(htmlString[characterIndex+5:], "|/raw|")
			if rawEnd != -1 {
				endPos := characterIndex + 5 + rawEnd + 6
				stringBuilder.WriteString(htmlString[characterIndex:endPos])
				characterIndex = endPos
				continue
			}
		}

		if htmlString[characterIndex] == '<' {
			rawTagName, rawEndTag := matchRawTagStart(htmlString[characterIndex:])
			if rawTagName != "" {
				characterIndex = copyRawElement(htmlString, characterIndex, rawTagName, rawEndTag, &stringBuilder)
				continue
			}

			characterIndex = scanTag(htmlString, characterIndex, &stringBuilder)

			// After tag closing >, check if there is a single space followed by <
			if characterIndex+1 < length && htmlString[characterIndex] == ' ' && htmlString[characterIndex+1] == '<' {
				characterIndex++ // skip space between > and <
			}
			continue
		}

		stringBuilder.WriteByte(htmlString[characterIndex])
		characterIndex++
	}

	return strings.TrimSpace(stringBuilder.String())
}

func PrettifyHTML(htmlString string) string {
	if htmlString == "" {
		return ""
	}
	htmlTokens := splitHtmlTokens(htmlString)
	var stringBuilder strings.Builder
	indentationLevel := 0

	for _, token := range htmlTokens {
		trimmedToken := strings.TrimSpace(token)
		if trimmedToken == "" {
			continue
		}

		if strings.HasPrefix(trimmedToken, "</") {
			indentationLevel = indentationLevel - 1
			if indentationLevel < 0 {
				indentationLevel = 0
			}
			stringBuilder.WriteString("\n" + strings.Repeat("  ", indentationLevel) + trimmedToken)
		} else if strings.HasPrefix(trimmedToken, "<") &&
			!strings.HasPrefix(trimmedToken, "<!") &&
			!strings.HasSuffix(trimmedToken, "/>") &&
			!strings.HasPrefix(trimmedToken, "<?") {
			stringBuilder.WriteString("\n" + strings.Repeat("  ", indentationLevel) + trimmedToken)
			indentationLevel++
		} else {
			stringBuilder.WriteString(trimmedToken)
		}
	}
	return strings.TrimSpace(stringBuilder.String())
}

func splitHtmlTokens(htmlString string) []string {
	var tokens []string
	var currentTokenBuilder strings.Builder

	insideTag := false
	inQuote := byte(0)
	for characterIndex := 0; characterIndex < len(htmlString); characterIndex++ {
		character := htmlString[characterIndex]
		if character == '<' && !insideTag {
			if currentTokenBuilder.Len() > 0 {
				tokens = append(tokens, currentTokenBuilder.String())
				currentTokenBuilder.Reset()
			}
			insideTag = true
			inQuote = 0
		} else if insideTag {
			if inQuote != 0 {
				if character == inQuote {
					inQuote = 0
				}
			} else {
				if character == '"' || character == '\'' {
					inQuote = character
				} else if character == '>' {
					currentTokenBuilder.WriteByte(character)
					tokens = append(tokens, currentTokenBuilder.String())
					currentTokenBuilder.Reset()
					insideTag = false
					continue
				}
			}
		}
		currentTokenBuilder.WriteByte(character)
	}

	if currentTokenBuilder.Len() > 0 {
		tokens = append(tokens, currentTokenBuilder.String())
	}
	return tokens
}
