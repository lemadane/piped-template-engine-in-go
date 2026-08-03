package pte

import (
	"encoding/json"
	"strings"
	"testing"
)

// FuzzAlpineOptionParser tests that the strict Alpine option parser never panics on arbitrary string input.
func FuzzAlpineOptionParser(fuzzingInstance *testing.F) {
	seedCorpusInputs := []string{
		"key=value",
		"name='Ada' age=25",
		"message=\"It's ready\" cloak=true",
		"plugins='collapse,focus' build='csp'",
		"items='[\"Rice\",\"Coffee\"]'",
		"profile='{\"name\":\"Lemuel\"}'",
		"key='unterminated",
		"=missingkey",
		"missingval=",
		"key='val' key='duplicate'",
	}

	for _, seedInput := range seedCorpusInputs {
		fuzzingInstance.Add(seedInput)
	}

	fuzzingInstance.Fuzz(func(testingInstance *testing.T, inputString string) {
		parsedOptionList, optionMap, parseErr := parseAlpineOptions(inputString)
		if parseErr == nil {
			if len(parsedOptionList) != len(optionMap) {
				testingInstance.Fatalf("option list length %d does not match map length %d for input %q", len(parsedOptionList), len(optionMap), inputString)
			}
		}
	})
}

// FuzzAlpineStateParser tests that the state value parser never panics and that successful x-data outputs produce valid JSON.
func FuzzAlpineStateParser(fuzzingInstance *testing.F) {
	seedCorpusInputs := []string{
		"true",
		"false",
		"null",
		"42",
		"-100",
		"3.14159",
		"1.2.3",
		"--",
		"NaN",
		"[\"a\",\"b\"]",
		"{\"a\":1}",
		"[1,",
		"{name:",
		"It's ready",
		"C:\\System32",
	}

	for _, seedInput := range seedCorpusInputs {
		fuzzingInstance.Add(seedInput)
	}

	fuzzingInstance.Fuzz(func(testingInstance *testing.T, inputString string) {
		typedStateValue, conversionErr := parseAlpineStateValue("fuzzProperty", inputString)
		if conversionErr == nil {
			stateMap := map[string]any{
				"fuzzProperty": typedStateValue,
			}
			stateNode := &StateNode{
				StateMap: stateMap,
			}

			outputBuffer := &strings.Builder{}
			renderErr := stateNode.Render(nil, outputBuffer)
			if renderErr != nil {
				testingInstance.Fatalf("failed to render state node for input %q: %v", inputString, renderErr)
			}

			renderedOutput := outputBuffer.String()
			attributeStartIndex := strings.Index(renderedOutput, `x-data="`)
			if attributeStartIndex == -1 {
				testingInstance.Fatalf("x-data attribute missing in rendered output %q", renderedOutput)
			}

			quoteStartIndex := attributeStartIndex + len(`x-data="`)
			quoteEndIndex := strings.Index(renderedOutput[quoteStartIndex:], `"`)
			if quoteEndIndex == -1 {
				testingInstance.Fatalf("unclosed quote in rendered output %q", renderedOutput)
			}

			rawAttributeContent := renderedOutput[quoteStartIndex : quoteStartIndex+quoteEndIndex]
			unescapedJSON := strings.ReplaceAll(rawAttributeContent, "&quot;", `"`)

			var decodedStateMap map[string]any
			if unmarshalErr := json.Unmarshal([]byte(unescapedJSON), &decodedStateMap); unmarshalErr != nil {
				testingInstance.Fatalf("unescaped x-data JSON %q is invalid for input %q: %v", unescapedJSON, inputString, unmarshalErr)
			}
		}
	})
}
