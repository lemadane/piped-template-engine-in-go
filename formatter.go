package pte

import (
	"regexp"
	"strings"
)

var commentRegex = regexp.MustCompile(`<!--[\s\S]*?-->`)
var spaceRegex = regexp.MustCompile(`\s+`)
var tagSpaceRegex = regexp.MustCompile(`>\s+<`)

func MinifyHTML(html string) string {
	if html == "" {
		return ""
	}
	res := commentRegex.ReplaceAllString(html, "")
	res = spaceRegex.ReplaceAllString(res, " ")
	res = tagSpaceRegex.ReplaceAllString(res, "><")
	return strings.TrimSpace(res)
}

func PrettifyHTML(html string) string {
	if html == "" {
		return ""
	}
	tokens := splitHtmlTokens(html)
	var sb strings.Builder
	indent := 0

	for _, token := range tokens {
		trimmed := strings.TrimSpace(token)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "</") {
			indent = indent - 1
			if indent < 0 {
				indent = 0
			}
			sb.WriteString("\n" + strings.Repeat("  ", indent) + trimmed)
		} else if strings.HasPrefix(trimmed, "<") &&
			!strings.HasPrefix(trimmed, "<!") &&
			!strings.HasSuffix(trimmed, "/>") &&
			!strings.HasPrefix(trimmed, "<?") {
			sb.WriteString("\n" + strings.Repeat("  ", indent) + trimmed)
			indent++
		} else {
			sb.WriteString(trimmed)
		}
	}
	return strings.TrimSpace(sb.String())
}

func splitHtmlTokens(html string) []string {
	var tokens []string
	var current strings.Builder

	inTag := false
	for i := 0; i < len(html); i++ {
		c := html[i]
		if c == '<' && !inTag {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			inTag = true
		} else if c == '>' && inTag {
			current.WriteByte(c)
			tokens = append(tokens, current.String())
			current.Reset()
			inTag = false
			continue
		}
		current.WriteByte(c)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}
