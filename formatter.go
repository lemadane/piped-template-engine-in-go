package pte

import (
	"fmt"
	"regexp"
	"strings"
)

var commentRegex = regexp.MustCompile(`<!--[\s\S]*?-->`)
var spaceRegex = regexp.MustCompile(`\s+`)
var tagSpaceRegex = regexp.MustCompile(`>\s+<`)

var preservedTagRegexes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)<pre[\s>][\s\S]*?</pre>`),
	regexp.MustCompile(`(?i)<textarea[\s>][\s\S]*?</textarea>`),
	regexp.MustCompile(`(?i)<script[\s>][\s\S]*?</script>`),
	regexp.MustCompile(`(?i)<style[\s>][\s\S]*?</style>`),
}

func MinifyHTML(htmlString string) string {
	if htmlString == "" {
		return ""
	}
	processedHtml := commentRegex.ReplaceAllString(htmlString, "")

	var placeholders []string
	for _, regexPattern := range preservedTagRegexes {
		processedHtml = regexPattern.ReplaceAllStringFunc(processedHtml, func(matchedTag string) string {
			placeholderIndex := len(placeholders)
			placeholders = append(placeholders, matchedTag)
			return fmt.Sprintf("___PTE_PRESERVED_%d___", placeholderIndex)
		})
	}

	resultString := spaceRegex.ReplaceAllString(processedHtml, " ")
	resultString = tagSpaceRegex.ReplaceAllString(resultString, "><")
	resultString = strings.TrimSpace(resultString)

	for placeholderIndex, placeholderText := range placeholders {
		resultString = strings.Replace(resultString, fmt.Sprintf("___PTE_PRESERVED_%d___", placeholderIndex), placeholderText, 1)
	}

	return resultString
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
