package pte

import (
	"strings"
)

func MinifyHTML(htmlString string) string {
	if htmlString == "" {
		return ""
	}

	var stringBuilder strings.Builder
	stringBuilder.Grow(len(htmlString))

	length := len(htmlString)
	characterIndex := 0

	for characterIndex < length {
		// 1. Check for HTML comment outside raw elements
		if strings.HasPrefix(htmlString[characterIndex:], "<!--") {
			commentEnd := strings.Index(htmlString[characterIndex+4:], "-->")
			if commentEnd != -1 {
				characterIndex += 4 + commentEnd + 3
				continue
			}
		}

		// 2. Check for raw/preserved element start: <script>, <style>, <pre>, <textarea>
		if htmlString[characterIndex] == '<' {
			rawTagName, rawEndTag := matchRawTagStart(htmlString[characterIndex:])
			if rawTagName != "" {
				openTagEndIndex := strings.IndexByte(htmlString[characterIndex:], '>')
				if openTagEndIndex != -1 {
					absoluteOpenTagEnd := characterIndex + openTagEndIndex + 1
					stringBuilder.WriteString(htmlString[characterIndex:absoluteOpenTagEnd])
					characterIndex = absoluteOpenTagEnd

					closingTagIndex := indexOfCaseInsensitive(htmlString[characterIndex:], rawEndTag)
					if closingTagIndex != -1 {
						absoluteClosingTagEnd := characterIndex + closingTagIndex + len(rawEndTag)
						stringBuilder.WriteString(htmlString[characterIndex:absoluteClosingTagEnd])
						characterIndex = absoluteClosingTagEnd
						continue
					} else {
						stringBuilder.WriteString(htmlString[characterIndex:])
						break
					}
				}
			}
		}

		character := htmlString[characterIndex]
		stringBuilder.WriteByte(character)
		characterIndex++
	}

	rawResult := stringBuilder.String()
	return collapseOutsideWhitespace(rawResult)
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
	subLower := strings.ToLower(substr)
	sLower := strings.ToLower(s)
	return strings.Index(sLower, subLower)
}

func collapseOutsideWhitespace(htmlString string) string {
	var stringBuilder strings.Builder
	stringBuilder.Grow(len(htmlString))

	length := len(htmlString)
	characterIndex := 0
	insideSpace := false

	for characterIndex < length {
		if htmlString[characterIndex] == '<' {
			rawTagName, rawEndTag := matchRawTagStart(htmlString[characterIndex:])
			if rawTagName != "" {
				openTagEndIndex := strings.IndexByte(htmlString[characterIndex:], '>')
				if openTagEndIndex != -1 {
					absoluteOpenTagEnd := characterIndex + openTagEndIndex + 1
					stringBuilder.WriteString(htmlString[characterIndex:absoluteOpenTagEnd])
					characterIndex = absoluteOpenTagEnd

					closingTagIndex := indexOfCaseInsensitive(htmlString[characterIndex:], rawEndTag)
					if closingTagIndex != -1 {
						absoluteClosingTagEnd := characterIndex + closingTagIndex + len(rawEndTag)
						stringBuilder.WriteString(htmlString[characterIndex:absoluteClosingTagEnd])
						characterIndex = absoluteClosingTagEnd
						insideSpace = false
						continue
					} else {
						stringBuilder.WriteString(htmlString[characterIndex:])
						break
					}
				}
			}
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
		if htmlString[characterIndex] == '<' {
			rawTagName, rawEndTag := matchRawTagStart(htmlString[characterIndex:])
			if rawTagName != "" {
				openTagEndIndex := strings.IndexByte(htmlString[characterIndex:], '>')
				if openTagEndIndex != -1 {
					absoluteOpenTagEnd := characterIndex + openTagEndIndex + 1
					stringBuilder.WriteString(htmlString[characterIndex:absoluteOpenTagEnd])
					characterIndex = absoluteOpenTagEnd

					closingTagIndex := indexOfCaseInsensitive(htmlString[characterIndex:], rawEndTag)
					if closingTagIndex != -1 {
						absoluteClosingTagEnd := characterIndex + closingTagIndex + len(rawEndTag)
						stringBuilder.WriteString(htmlString[characterIndex:absoluteClosingTagEnd])
						characterIndex = absoluteClosingTagEnd
						continue
					} else {
						stringBuilder.WriteString(htmlString[characterIndex:])
						break
					}
				}
			}
		}

		if htmlString[characterIndex] == '>' && characterIndex+2 < length && htmlString[characterIndex+1] == ' ' && htmlString[characterIndex+2] == '<' {
			stringBuilder.WriteString("><")
			characterIndex += 3
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
	for characterIndex := 0; characterIndex < len(htmlString); characterIndex++ {
		character := htmlString[characterIndex]
		if character == '<' && !insideTag {
			if currentTokenBuilder.Len() > 0 {
				tokens = append(tokens, currentTokenBuilder.String())
				currentTokenBuilder.Reset()
			}
			insideTag = true
		} else if character == '>' && insideTag {
			currentTokenBuilder.WriteByte(character)
			tokens = append(tokens, currentTokenBuilder.String())
			currentTokenBuilder.Reset()
			insideTag = false
			continue
		}
		currentTokenBuilder.WriteByte(character)
	}

	if currentTokenBuilder.Len() > 0 {
		tokens = append(tokens, currentTokenBuilder.String())
	}
	return tokens
}
