package pte

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// DefaultAlpineVersion defines the pinned stable release version for AlpineJS core and official plugins.
const DefaultAlpineVersion = "3.14.8"

// SupportedAlpinePlugins lists official AlpineJS plugins supported by PTEGo.
var SupportedAlpinePlugins = map[string]bool{
	"anchor":    true,
	"collapse":  true,
	"focus":     true,
	"intersect": true,
	"mask":      true,
	"morph":     true,
	"persist":   true,
	"sort":      true,
}

// SupportedAlpineSetupOptions lists valid options for the |alpine| directive.
var SupportedAlpineSetupOptions = map[string]bool{
	"version": true,
	"plugins": true,
	"cloak":   true,
	"build":   true,
	"src":     true,
}

// SupportedAlpineElementDirectives lists valid element directives for AlpineJS.
var SupportedAlpineElementDirectives = map[string]bool{
	"alpine-show":  true,
	"alpine-cloak": true,
	"alpine-text":  true,
	"alpine-html":  true,
	"alpine-model": true,
	"alpine-data":  true,
}

// AlpineOption represents a parsed key-value option pair.
type AlpineOption struct {
	Key   string
	Value string
}

// parseAlpineOptions strictly parses key-value option pairs from an Alpine directive string.
// It enforces quote termination, escaped quotes, no missing keys/values, and duplicate key detection.
func parseAlpineOptions(inputString string) ([]AlpineOption, map[string]string, error) {
	parsedOptions := make([]AlpineOption, 0)
	optionMap := make(map[string]string)

	characterIndex := 0
	inputLength := len(inputString)

	for characterIndex < inputLength {
		// Skip whitespace
		for characterIndex < inputLength && isWhitespaceChar(inputString[characterIndex]) {
			characterIndex++
		}
		if characterIndex >= inputLength {
			break
		}

		// Read key name
		keyStartIndex := characterIndex
		for characterIndex < inputLength && inputString[characterIndex] != '=' && !isWhitespaceChar(inputString[characterIndex]) {
			characterIndex++
		}

		keyName := inputString[keyStartIndex:characterIndex]
		if keyName == "" {
			return nil, nil, fmt.Errorf("missing property name at position %d in %q", keyStartIndex, inputString)
		}

		// Skip whitespace before '='
		for characterIndex < inputLength && isWhitespaceChar(inputString[characterIndex]) {
			characterIndex++
		}

		if characterIndex >= inputLength || inputString[characterIndex] != '=' {
			return nil, nil, fmt.Errorf("missing value for property %q at position %d", keyName, keyStartIndex)
		}

		// Skip '='
		characterIndex++

		// Skip whitespace after '='
		for characterIndex < inputLength && isWhitespaceChar(inputString[characterIndex]) {
			characterIndex++
		}

		if characterIndex >= inputLength {
			return nil, nil, fmt.Errorf("missing value for property %q after '=' at position %d", keyName, characterIndex)
		}

		// Read value
		var optionValue string
		valueStartIndex := characterIndex

		if inputString[characterIndex] == '\'' || inputString[characterIndex] == '"' {
			quoteCharacter := inputString[characterIndex]
			characterIndex++
			stringValueBuilder := strings.Builder{}

			quoteTerminated := false
			for characterIndex < inputLength {
				currentCharacter := inputString[characterIndex]
				if currentCharacter == '\\' && characterIndex+1 < inputLength {
					nextCharacter := inputString[characterIndex+1]
					if nextCharacter == quoteCharacter || nextCharacter == '\\' {
						stringValueBuilder.WriteByte(nextCharacter)
						characterIndex += 2
						continue
					}
				}
				if currentCharacter == quoteCharacter {
					quoteTerminated = true
					characterIndex++
					break
				}
				stringValueBuilder.WriteByte(currentCharacter)
				characterIndex++
			}

			if !quoteTerminated {
				return nil, nil, fmt.Errorf("unterminated quote for property %q starting at position %d", keyName, valueStartIndex)
			}
			optionValue = stringValueBuilder.String()
		} else if inputString[characterIndex] == '[' || inputString[characterIndex] == '{' {
			openDelimiter := inputString[characterIndex]
			closeDelimiter := byte(']')
			if openDelimiter == '{' {
				closeDelimiter = '}'
			}
			delimiterDepth := 0
			insideQuote := false
			var activeQuoteChar byte

			structureValueBuilder := strings.Builder{}
			structureTerminated := false

			for characterIndex < inputLength {
				currentCharacter := inputString[characterIndex]
				structureValueBuilder.WriteByte(currentCharacter)

				if insideQuote {
					if currentCharacter == '\\' && characterIndex+1 < inputLength {
						characterIndex++
						structureValueBuilder.WriteByte(inputString[characterIndex])
					} else if currentCharacter == activeQuoteChar {
						insideQuote = false
					}
				} else {
					if currentCharacter == '\'' || currentCharacter == '"' {
						insideQuote = true
						activeQuoteChar = currentCharacter
					} else if currentCharacter == openDelimiter {
						delimiterDepth++
					} else if currentCharacter == closeDelimiter {
						delimiterDepth--
						if delimiterDepth == 0 {
							structureTerminated = true
							characterIndex++
							break
						}
					}
				}
				characterIndex++
			}

			if !structureTerminated {
				return nil, nil, fmt.Errorf("unterminated bracket/brace for property %q starting at position %d", keyName, valueStartIndex)
			}
			optionValue = structureValueBuilder.String()
		} else {
			unquotedStartIndex := characterIndex
			for characterIndex < inputLength && !isWhitespaceChar(inputString[characterIndex]) {
				characterIndex++
			}
			optionValue = inputString[unquotedStartIndex:characterIndex]
			if optionValue == "" {
				return nil, nil, fmt.Errorf("missing value for property %q at position %d", keyName, unquotedStartIndex)
			}
		}

		if _, exists := optionMap[keyName]; exists {
			return nil, nil, fmt.Errorf("duplicate property %q found in options", keyName)
		}

		optionMap[keyName] = optionValue
		parsedOptions = append(parsedOptions, AlpineOption{
			Key:   keyName,
			Value: optionValue,
		})
	}

	return parsedOptions, optionMap, nil
}

// parseAlpineStateValue converts a raw string state value into a strongly-typed Go value.
func parseAlpineStateValue(keyName string, rawValue string) (any, error) {
	trimmedValue := strings.TrimSpace(rawValue)
	if trimmedValue == "" {
		return "", nil
	}
	if trimmedValue == "true" {
		return true, nil
	}
	if trimmedValue == "false" {
		return false, nil
	}
	if trimmedValue == "null" {
		return nil, nil
	}

	// JSON Array parsing
	if strings.HasPrefix(trimmedValue, "[") {
		var parsedArray []any
		if jsonErr := json.Unmarshal([]byte(trimmedValue), &parsedArray); jsonErr != nil {
			return nil, fmt.Errorf("invalid Alpine state value for %q: malformed JSON array", keyName)
		}
		return parsedArray, nil
	}

	// JSON Object parsing
	if strings.HasPrefix(trimmedValue, "{") {
		var parsedObject map[string]any
		if jsonErr := json.Unmarshal([]byte(trimmedValue), &parsedObject); jsonErr != nil {
			return nil, fmt.Errorf("invalid Alpine state value for %q: malformed JSON object", keyName)
		}
		return parsedObject, nil
	}

	// Check if value resembles a numeric expression
	if isNumericExpression(trimmedValue) {
		if strings.EqualFold(trimmedValue, "nan") || strings.EqualFold(trimmedValue, "inf") || strings.EqualFold(trimmedValue, "+inf") || strings.EqualFold(trimmedValue, "-inf") {
			return nil, fmt.Errorf("invalid Alpine state value for %q: %q is not a valid number", keyName, trimmedValue)
		}

		if parsedInteger, integerErr := strconv.ParseInt(trimmedValue, 10, 64); integerErr == nil {
			return parsedInteger, nil
		}
		if parsedFloat, floatErr := strconv.ParseFloat(trimmedValue, 64); floatErr == nil {
			return parsedFloat, nil
		}
		return nil, fmt.Errorf("invalid Alpine state value for %q: %q is not a valid number", keyName, trimmedValue)
	}

	return trimmedValue, nil
}

// isNumericExpression checks if a string consists of characters typical of numbers or malformed numbers.
func isNumericExpression(valueString string) bool {
	if valueString == "" {
		return false
	}
	firstChar := valueString[0]
	if firstChar == '-' || firstChar == '+' || firstChar == '.' || (firstChar >= '0' && firstChar <= '9') {
		return true
	}
	return strings.EqualFold(valueString, "nan") || strings.EqualFold(valueString, "inf") || strings.EqualFold(valueString, "+inf") || strings.EqualFold(valueString, "-inf")
}

// validateAlpineVersion ensures a version string is clean and injection-free.
func validateAlpineVersion(versionString string) error {
	if versionString == "" {
		return fmt.Errorf("version string cannot be empty")
	}
	for index := 0; index < len(versionString); index++ {
		character := versionString[index]
		if !(character >= '0' && character <= '9') && character != '.' && character != '-' && character != 'v' {
			return fmt.Errorf("invalid Alpine version %q: contains illegal characters", versionString)
		}
	}
	return nil
}

// validateAlpineURL ensures a URL string is valid and safe for CDN script tags.
func validateAlpineURL(urlString string) error {
	if urlString == "" {
		return fmt.Errorf("URL string cannot be empty")
	}
	parsedURL, parseErr := url.Parse(urlString)
	if parseErr != nil {
		return fmt.Errorf("invalid Alpine URL %q: %w", urlString, parseErr)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" && !strings.HasPrefix(urlString, "/") {
		return fmt.Errorf("invalid Alpine URL %q: must use http, https, or relative path", urlString)
	}
	if strings.ContainsAny(urlString, `"'>`) {
		return fmt.Errorf("invalid Alpine URL %q: contains HTML injection characters", urlString)
	}
	return nil
}

// findClosestDirective Suggests closest valid directive for misspelled directives.
func findClosestDirective(invalidDirective string) string {
	closestMatch := ""
	minimumDistance := 999

	for validDirective := range SupportedAlpineElementDirectives {
		distance := levenshteinDistance(invalidDirective, validDirective)
		if distance < minimumDistance {
			minimumDistance = distance
			closestMatch = validDirective
		}
	}
	if minimumDistance <= 3 {
		return closestMatch
	}
	return ""
}

// levenshteinDistance calculates edit distance between two strings.
func levenshteinDistance(firstString, secondString string) int {
	firstRunes := []rune(firstString)
	secondRunes := []rune(secondString)

	firstLength := len(firstRunes)
	secondLength := len(secondRunes)

	distanceMatrix := make([][]int, firstLength+1)
	for rowIndex := 0; rowIndex <= firstLength; rowIndex++ {
		distanceMatrix[rowIndex] = make([]int, secondLength+1)
		distanceMatrix[rowIndex][0] = rowIndex
	}
	for columnIndex := 0; columnIndex <= secondLength; columnIndex++ {
		distanceMatrix[0][columnIndex] = columnIndex
	}

	for rowIndex := 1; rowIndex <= firstLength; rowIndex++ {
		for columnIndex := 1; columnIndex <= secondLength; columnIndex++ {
			substitutionCost := 0
			if firstRunes[rowIndex-1] != secondRunes[columnIndex-1] {
				substitutionCost = 1
			}

			deletionCost := distanceMatrix[rowIndex-1][columnIndex] + 1
			insertionCost := distanceMatrix[rowIndex][columnIndex-1] + 1
			replacementCost := distanceMatrix[rowIndex-1][columnIndex-1] + substitutionCost

			minimumCost := deletionCost
			if insertionCost < minimumCost {
				minimumCost = insertionCost
			}
			if replacementCost < minimumCost {
				minimumCost = replacementCost
			}
			distanceMatrix[rowIndex][columnIndex] = minimumCost
		}
	}
	return distanceMatrix[firstLength][secondLength]
}
