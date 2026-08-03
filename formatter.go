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

func MinifyHTML(html string) string {
	if html == "" {
		return ""
	}
	html = commentRegex.ReplaceAllString(html, "")

	var placeholders []string
	for _, rgx := range preservedTagRegexes {
		html = rgx.ReplaceAllStringFunc(html, func(m string) string {
			idx := len(placeholders)
			placeholders = append(placeholders, m)
			return fmt.Sprintf("___PTE_PRESERVED_%d___", idx)
		})
	}

	res := spaceRegex.ReplaceAllString(html, " ")
	res = tagSpaceRegex.ReplaceAllString(res, "><")
	res = strings.TrimSpace(res)

	for i, ph := range placeholders {
		res = strings.Replace(res, fmt.Sprintf("___PTE_PRESERVED_%d___", i), ph, 1)
	}

	return res
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
