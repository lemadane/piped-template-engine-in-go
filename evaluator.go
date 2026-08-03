package pte

import (
	"fmt"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Evaluator struct{}

func NewEvaluator() *Evaluator {
	return &Evaluator{}
}

func (evaluator *Evaluator) Evaluate(expression string, context *Context) (any, error) {
	trimmedExpression := strings.TrimSpace(expression)
	if trimmedExpression == "" {
		return nil, nil
	}
	return evaluator.evaluateValue(trimmedExpression, context)
}

func (evaluator *Evaluator) EvaluateBoolean(expression string, context *Context) (bool, error) {
	evaluatedVal, err := evaluator.evaluateCondition(strings.TrimSpace(expression), context)
	if err != nil {
		return false, err
	}
	return evaluator.toBoolean(evaluatedVal), nil
}

func (evaluator *Evaluator) ValuesEqual(left, right any) bool {
	if left == nil || right == nil {
		return left == right
	}

	leftNum, isLeftNum := parseNumberValue(left)
	rightNum, isRightNum := parseNumberValue(right)
	if isLeftNum && isRightNum {
		eq, err := compareNumbers(leftNum, rightNum, "==")
		if err == nil {
			return eq
		}
	}

	return reflect.DeepEqual(left, right)
}

func (evaluator *Evaluator) evaluateCondition(expression string, context *Context) (any, error) {
	trimmedExpression := strings.TrimSpace(expression)

	if operatorIndex := evaluator.findWordOperator(trimmedExpression, "nor"); operatorIndex != -1 {
		leftExpr := trimmedExpression[:operatorIndex]
		rightExpr := trimmedExpression[operatorIndex+len("nor"):]
		leftBool, err := evaluator.EvaluateBoolean(leftExpr, context)
		if err != nil {
			return nil, err
		}
		rightBool, err := evaluator.EvaluateBoolean(rightExpr, context)
		if err != nil {
			return nil, err
		}
		return !(leftBool || rightBool), nil
	}

	if operatorIndex := evaluator.findWordOperator(trimmedExpression, "or"); operatorIndex != -1 {
		leftExpr := trimmedExpression[:operatorIndex]
		rightExpr := trimmedExpression[operatorIndex+len("or"):]
		leftBool, err := evaluator.EvaluateBoolean(leftExpr, context)
		if err != nil {
			return nil, err
		}
		rightBool, err := evaluator.EvaluateBoolean(rightExpr, context)
		if err != nil {
			return nil, err
		}
		return leftBool || rightBool, nil
	}

	if operatorIndex := evaluator.findWordOperator(trimmedExpression, "nand"); operatorIndex != -1 {
		leftExpr := trimmedExpression[:operatorIndex]
		rightExpr := trimmedExpression[operatorIndex+len("nand"):]
		leftBool, err := evaluator.EvaluateBoolean(leftExpr, context)
		if err != nil {
			return nil, err
		}
		rightBool, err := evaluator.EvaluateBoolean(rightExpr, context)
		if err != nil {
			return nil, err
		}
		return !(leftBool && rightBool), nil
	}

	if operatorIndex := evaluator.findWordOperator(trimmedExpression, "and"); operatorIndex != -1 {
		leftExpr := trimmedExpression[:operatorIndex]
		rightExpr := trimmedExpression[operatorIndex+len("and"):]
		leftBool, err := evaluator.EvaluateBoolean(leftExpr, context)
		if err != nil {
			return nil, err
		}
		rightBool, err := evaluator.EvaluateBoolean(rightExpr, context)
		if err != nil {
			return nil, err
		}
		return leftBool && rightBool, nil
	}

	if evaluator.startsWithWord(trimmedExpression, "not") {
		valueExpr := strings.TrimSpace(trimmedExpression[len("not"):])
		valBool, err := evaluator.EvaluateBoolean(valueExpr, context)
		if err != nil {
			return nil, err
		}
		return !valBool, nil
	}

	if comparisonDesc := evaluator.findComparison(trimmedExpression); comparisonDesc != nil {
		leftVal, err := evaluator.evaluateValue(comparisonDesc.left, context)
		if err != nil {
			return nil, err
		}
		rightVal, err := evaluator.evaluateValue(comparisonDesc.right, context)
		if err != nil {
			return nil, err
		}
		return evaluator.compare(leftVal, rightVal, comparisonDesc.operator)
	}

	return evaluator.evaluateValue(trimmedExpression, context)
}

func (evaluator *Evaluator) evaluateValue(expression string, context *Context) (any, error) {
	trimmedExpression := evaluator.removeWrappingParentheses(strings.TrimSpace(expression))
	if trimmedExpression == "" {
		return nil, nil
	}

	if filteredDesc := evaluator.parseFilteredExpression(trimmedExpression); filteredDesc != nil {
		return evaluator.evaluateFilteredExpression(filteredDesc, context)
	}

	if arithDesc := evaluator.findBinaryArithmetic(trimmedExpression); arithDesc != nil {
		leftVal, err := evaluator.evaluateValue(arithDesc.left, context)
		if err != nil {
			return nil, err
		}
		rightVal, err := evaluator.evaluateValue(arithDesc.right, context)
		if err != nil {
			return nil, err
		}
		leftNum, isLeftNum := parseNumberValue(leftVal)
		rightNum, isRightNum := parseNumberValue(rightVal)
		if isLeftNum && isRightNum {
			return evaluator.evaluateArithmetic(leftNum, rightNum, arithDesc.operator)
		}
	}

	if ternaryDesc := evaluator.findTernaryExpression(trimmedExpression); ternaryDesc != nil {
		condBool, err := evaluator.EvaluateBoolean(ternaryDesc.condition, context)
		if err != nil {
			return nil, err
		}
		if condBool {
			return evaluator.evaluateValue(ternaryDesc.trueExpression, context)
		}
		return evaluator.evaluateValue(ternaryDesc.falseExpression, context)
	}

	if operatorIndex := evaluator.findNullCoalescingOperator(trimmedExpression); operatorIndex != -1 {
		leftExpr := strings.TrimSpace(trimmedExpression[:operatorIndex])
		rightExpr := strings.TrimSpace(trimmedExpression[operatorIndex+2:])
		leftVal, err := evaluator.evaluateValue(leftExpr, context)
		if err != nil {
			return nil, err
		}
		if leftVal != nil {
			return leftVal, nil
		}
		return evaluator.evaluateValue(rightExpr, context)
	}

	if evaluator.isQuotedString(trimmedExpression) {
		return trimmedExpression[1 : len(trimmedExpression)-1], nil
	}

	if trimmedExpression == "true" {
		return true, nil
	}

	if trimmedExpression == "false" {
		return false, nil
	}

	if trimmedExpression == "null" {
		return nil, nil
	}

	if evaluator.isNumber(trimmedExpression) {
		if !strings.ContainsAny(trimmedExpression, ".eE") {
			if parsedInt, err := strconv.ParseInt(trimmedExpression, 10, 64); err == nil {
				return parsedInt, nil
			}
			if parsedUint, err := strconv.ParseUint(trimmedExpression, 10, 64); err == nil {
				return parsedUint, nil
			}
			return nil, fmt.Errorf("integer literal out of range: %s", trimmedExpression)
		}
		parsedFloat, err := strconv.ParseFloat(trimmedExpression, 64)
		if err != nil {
			return nil, err
		}
		if math.IsNaN(parsedFloat) || math.IsInf(parsedFloat, 0) {
			return nil, fmt.Errorf("invalid float literal: %s", trimmedExpression)
		}
		return parsedFloat, nil
	}

	return evaluator.readPath(trimmedExpression, context)
}

func (evaluator *Evaluator) compare(left, right any, operator string) (bool, error) {
	leftNum, isLeftNum := parseNumberValue(left)
	rightNum, isRightNum := parseNumberValue(right)

	if isLeftNum && isRightNum {
		return compareNumbers(leftNum, rightNum, operator)
	}

	// Compare as strings
	leftStr := fmt.Sprintf("%v", left)
	rightStr := fmt.Sprintf("%v", right)
	if left == nil {
		leftStr = ""
	}
	if right == nil {
		rightStr = ""
	}

	switch operator {
	case "==":
		return leftStr == rightStr, nil
	case "!=":
		return leftStr != rightStr, nil
	case ">":
		return leftStr > rightStr, nil
	case ">=":
		return leftStr >= rightStr, nil
	case "<":
		return leftStr < rightStr, nil
	case "<=":
		return leftStr <= rightStr, nil
	default:
		return false, fmt.Errorf("unsupported operator: %s", operator)
	}
}

func (evaluator *Evaluator) toBoolean(value any) bool {
	if value == nil {
		return false
	}

	reflectVal := reflect.ValueOf(value)
	for reflectVal.Kind() == reflect.Ptr || reflectVal.Kind() == reflect.Interface {
		if reflectVal.IsNil() {
			return false
		}
		reflectVal = reflectVal.Elem()
	}

	switch reflectVal.Kind() {
	case reflect.Bool:
		return reflectVal.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflectVal.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return reflectVal.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return reflectVal.Float() != 0.0
	case reflect.String:
		return strings.TrimSpace(reflectVal.String()) != ""
	case reflect.Slice, reflect.Array:
		return reflectVal.Len() > 0
	case reflect.Map:
		return reflectVal.Len() > 0
	}

	return true
}

func (evaluator *Evaluator) isQuotedString(value string) bool {
	return len(value) >= 2 && (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") ||
		strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'"))
}

var numberRegex = regexp.MustCompile(`^-?\d+(\.\d+)?([eE][+-]?\d+)?$`)

func (evaluator *Evaluator) isNumber(value string) bool {
	return numberRegex.MatchString(value)
}

func (evaluator *Evaluator) startsWithWord(expression, word string) bool {
	return expression == word || strings.HasPrefix(expression, word+" ")
}

func (evaluator *Evaluator) findWordOperator(expression, operator string) int {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0

	operatorLength := len(operator)
	for index := 0; index <= len(expression)-operatorLength; index++ {
		character := expression[index]

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

		if strings.HasPrefix(expression[index:], operator) {
			beforeIsBoundary := index == 0 || isWhitespaceChar(expression[index-1])
			afterIndex := index + operatorLength
			afterIsBoundary := afterIndex >= len(expression) || isWhitespaceChar(expression[afterIndex])

			if beforeIsBoundary && afterIsBoundary {
				return index
			}
		}
	}
	return -1
}

type comparisonDesc struct {
	left     string
	operator string
	right    string
}

func (evaluator *Evaluator) findComparison(expression string) *comparisonDesc {
	operatorsList := []string{"==", "!=", ">=", "<=", ">", "<"}

	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0

	for index := 0; index < len(expression); index++ {
		character := expression[index]

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

		for _, op := range operatorsList {
			if strings.HasPrefix(expression[index:], op) {
				return &comparisonDesc{
					left:     strings.TrimSpace(expression[:index]),
					operator: op,
					right:    strings.TrimSpace(expression[index+len(op):]),
				}
			}
		}
	}
	return nil
}

func (evaluator *Evaluator) removeWrappingParentheses(expression string) string {
	result := expression
	for strings.HasPrefix(result, "(") && strings.HasSuffix(result, ")") && evaluator.wrapsWholeExpression(result) {
		result = strings.TrimSpace(result[1 : len(result)-1])
	}
	return result
}

func (evaluator *Evaluator) wrapsWholeExpression(expression string) bool {
	depth := 0
	insideSingleQuote := false
	insideDoubleQuote := false

	for index := 0; index < len(expression); index++ {
		character := expression[index]

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
			depth++
		}
		if character == ')' {
			depth--
		}

		if depth == 0 && index < len(expression)-1 {
			return false
		}
	}
	return depth == 0
}

type ternaryDesc struct {
	condition       string
	trueExpression  string
	falseExpression string
}

func (evaluator *Evaluator) findTernaryExpression(expression string) *ternaryDesc {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0
	questionIndex := -1

	for index := 0; index < len(expression); index++ {
		character := expression[index]

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

		if character != '?' {
			continue
		}

		if evaluator.isOptionalChainingQuestionMark(expression, index) {
			continue
		}
		if evaluator.isNullCoalescingQuestionMark(expression, index) {
			continue
		}

		questionIndex = index
		break
	}

	if questionIndex == -1 {
		return nil
	}

	colonIndex := evaluator.findTernaryColon(expression, questionIndex+1)
	if colonIndex == -1 {
		return nil // Invalid or incomplete
	}

	return &ternaryDesc{
		condition:       strings.TrimSpace(expression[:questionIndex]),
		trueExpression:  strings.TrimSpace(expression[questionIndex+1 : colonIndex]),
		falseExpression: strings.TrimSpace(expression[colonIndex+1:]),
	}
}

func (evaluator *Evaluator) findTernaryColon(expression string, startIndex int) int {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0
	nestedTernaryDepth := 0

	for index := startIndex; index < len(expression); index++ {
		character := expression[index]

		if character == '\'' && !insideDoubleQuote {
			insideSingleQuote = !insideSingleQuote
			continue
		}
		if character == '"' && !insideDoubleQuote {
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

		if character == '?' {
			if evaluator.isOptionalChainingQuestionMark(expression, index) {
				continue
			}
			if evaluator.isNullCoalescingQuestionMark(expression, index) {
				continue
			}
			nestedTernaryDepth++
			continue
		}

		if character == ':' {
			if nestedTernaryDepth == 0 {
				return index
			}
			nestedTernaryDepth--
		}
	}
	return -1
}

func (evaluator *Evaluator) isOptionalChainingQuestionMark(expression string, index int) bool {
	return index+1 < len(expression) && expression[index+1] == '.'
}

func (evaluator *Evaluator) isNullCoalescingQuestionMark(expression string, index int) bool {
	previousIsQuestionMark := index > 0 && expression[index-1] == '?'
	nextIsQuestionMark := index+1 < len(expression) && expression[index+1] == '?'
	return previousIsQuestionMark || nextIsQuestionMark
}

func (evaluator *Evaluator) findNullCoalescingOperator(expression string) int {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0

	for index := 0; index < len(expression)-1; index++ {
		character := expression[index]

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

		if parenthesisDepth == 0 && strings.HasPrefix(expression[index:], "??") {
			return index
		}
	}
	return -1
}

type filteredExpressionDesc struct {
	valueExpression string
	filters         []string
}

func (evaluator *Evaluator) parseFilteredExpression(expression string) *filteredExpressionDesc {
	parts := evaluator.splitByTopLevelComma(expression)
	if len(parts) <= 1 {
		return nil
	}
	return &filteredExpressionDesc{
		valueExpression: parts[0],
		filters:         parts[1:],
	}
}

func (evaluator *Evaluator) splitByTopLevelComma(expression string) []string {
	var parts []string
	var currentPart strings.Builder

	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0
	bracketDepth := 0
	braceDepth := 0

	for index := 0; index < len(expression); index++ {
		character := expression[index]

		if character == '\'' && !insideDoubleQuote {
			insideSingleQuote = !insideSingleQuote
			currentPart.WriteByte(character)
			continue
		}
		if character == '"' && !insideSingleQuote {
			insideDoubleQuote = !insideDoubleQuote
			currentPart.WriteByte(character)
			continue
		}

		if !insideSingleQuote && !insideDoubleQuote {
			if character == '(' {
				parenthesisDepth++
				currentPart.WriteByte(character)
				continue
			}
			if character == ')' {
				parenthesisDepth--
				currentPart.WriteByte(character)
				continue
			}
			if character == '[' {
				bracketDepth++
				currentPart.WriteByte(character)
				continue
			}
			if character == ']' {
				bracketDepth--
				currentPart.WriteByte(character)
				continue
			}
			if character == '{' {
				braceDepth++
				currentPart.WriteByte(character)
				continue
			}
			if character == '}' {
				braceDepth--
				currentPart.WriteByte(character)
				continue
			}

			if parenthesisDepth == 0 && bracketDepth == 0 && braceDepth == 0 && character == ',' {
				part := strings.TrimSpace(currentPart.String())
				parts = append(parts, part)
				currentPart.Reset()
				continue
			}
		}
		currentPart.WriteByte(character)
	}

	lastPart := strings.TrimSpace(currentPart.String())
	parts = append(parts, lastPart)
	return parts
}

func (evaluator *Evaluator) evaluateFilteredExpression(filtered *filteredExpressionDesc, context *Context) (any, error) {
	evaluatedVal, err := evaluator.evaluateValue(filtered.valueExpression, context)
	if err != nil {
		return nil, err
	}

	for _, filterSource := range filtered.filters {
		evaluatedVal, err = evaluator.applyFilter(evaluatedVal, filterSource, context)
		if err != nil {
			return nil, err
		}
	}
	return evaluatedVal, nil
}

type filterCallDesc struct {
	name               string
	argumentExpression string
}

func (evaluator *Evaluator) parseFilterCall(filterSource string) filterCallDesc {
	trimmed := strings.TrimSpace(filterSource)
	for index := 0; index < len(trimmed); index++ {
		if isWhitespaceChar(trimmed[index]) {
			return filterCallDesc{
				name:               strings.TrimSpace(trimmed[:index]),
				argumentExpression: strings.TrimSpace(trimmed[index+1:]),
			}
		}
	}
	return filterCallDesc{
		name:               trimmed,
		argumentExpression: "",
	}
}

func (evaluator *Evaluator) applyFilter(val any, filterSource string, context *Context) (any, error) {
	call := evaluator.parseFilterCall(filterSource)

	switch call.name {
	case "upper":
		return strings.ToUpper(evaluator.stringValue(val)), nil
	case "lower":
		return strings.ToLower(evaluator.stringValue(val)), nil
	case "trim":
		return strings.TrimSpace(evaluator.stringValue(val)), nil
	case "capitalize":
		return evaluator.capitalizeText(evaluator.stringValue(val)), nil
	case "slug":
		return evaluator.slugify(evaluator.stringValue(val)), nil
	case "length":
		return evaluator.lengthOf(val), nil
	case "default":
		return evaluator.defaultValue(val, call.argumentExpression, context)
	case "currency":
		return evaluator.currencyValue(val, call.argumentExpression, context)
	case "number":
		return evaluator.numberValue(val, call.argumentExpression, context)
	case "date":
		return evaluator.formatTemporalValue(val, call.argumentExpression, context, "2006-01-02")
	case "time":
		return evaluator.formatTemporalValue(val, call.argumentExpression, context, "15:04:05")
	case "datetime":
		return evaluator.formatTemporalValue(val, call.argumentExpression, context, "2006-01-02 15:04:05")
	default:
		return nil, fmt.Errorf("unknown filter: %s", call.name)
	}
}

func (evaluator *Evaluator) stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}

func (evaluator *Evaluator) capitalizeText(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

var slugRemoveRegex = regexp.MustCompile(`[^a-z0-9\s-]`)
var slugCollapseRegex = regexp.MustCompile(`[\s_]+`)
var slugHyphenCollapseRegex = regexp.MustCompile(`-+`)

func (evaluator *Evaluator) slugify(value string) string {
	if value == "" {
		return ""
	}
	normalized := strings.ToLower(value)
	normalized = slugRemoveRegex.ReplaceAllString(normalized, "")
	normalized = strings.TrimSpace(normalized)
	normalized = slugCollapseRegex.ReplaceAllString(normalized, "-")
	normalized = slugHyphenCollapseRegex.ReplaceAllString(normalized, "-")
	return normalized
}

func (evaluator *Evaluator) lengthOf(value any) int {
	if value == nil {
		return 0
	}

	reflectVal := reflect.ValueOf(value)
	for reflectVal.Kind() == reflect.Ptr || reflectVal.Kind() == reflect.Interface {
		if reflectVal.IsNil() {
			return 0
		}
		reflectVal = reflectVal.Elem()
	}

	switch reflectVal.Kind() {
	case reflect.String:
		return reflectVal.Len()
	case reflect.Slice, reflect.Array:
		return reflectVal.Len()
	case reflect.Map:
		return reflectVal.Len()
	}
	return len(fmt.Sprintf("%v", value))
}

func (evaluator *Evaluator) defaultValue(value any, argumentExpression string, context *Context) (any, error) {
	if argumentExpression == "" {
		return nil, fmt.Errorf("default filter requires an argument")
	}

	if evaluator.toBoolean(value) {
		return value, nil
	}

	return evaluator.evaluateValue(argumentExpression, context)
}

func (evaluator *Evaluator) currencyValue(value any, argumentExpression string, context *Context) (string, error) {
	if value == nil {
		return "", nil
	}

	symbol := ""
	if argumentExpression != "" {
		symVal, err := evaluator.evaluateValue(argumentExpression, context)
		if err != nil {
			return "", err
		}
		symbol = fmt.Sprintf("%v", symVal)
	}

	num, isNum := toFloat64(value)
	if !isNum {
		return "", fmt.Errorf("value is not numeric: %v", value)
	}

	return symbol + evaluator.formatNumberPattern(num, "#,##0.00"), nil
}

func (evaluator *Evaluator) numberValue(value any, argumentExpression string, context *Context) (string, error) {
	if value == nil {
		return "", nil
	}

	pattern := "#,##0.##"
	if argumentExpression != "" {
		patVal, err := evaluator.evaluateValue(argumentExpression, context)
		if err != nil {
			return "", err
		}
		pattern = fmt.Sprintf("%v", patVal)
	}

	num, isNum := toFloat64(value)
	if !isNum {
		return "", fmt.Errorf("value is not numeric: %v", value)
	}

	return evaluator.formatNumberPattern(num, pattern), nil
}

func (evaluator *Evaluator) formatNumberPattern(num float64, pattern string) string {
	hasComma := strings.Contains(pattern, ",")
	decimalPlaces := 0
	isVariableDecimal := false

	dotIndex := strings.Index(pattern, ".")
	if dotIndex != -1 {
		decimalPart := pattern[dotIndex+1:]
		decimalPlaces = len(decimalPart)
		if strings.Contains(decimalPart, "#") {
			isVariableDecimal = true
		}
	}

	// Format number
	var formatted string
	if isVariableDecimal {
		// Strip trailing zeros up to decimalPlaces
		formatted = strconv.FormatFloat(num, 'f', decimalPlaces, 64)
		if strings.Contains(formatted, ".") {
			formatted = strings.TrimRight(formatted, "0")
			formatted = strings.TrimRight(formatted, ".")
		}
	} else {
		formatted = fmt.Sprintf("%.*f", decimalPlaces, num)
	}

	if hasComma {
		parts := strings.Split(formatted, ".")
		integerPart := parts[0]
		// Handle negative sign
		isNegative := false
		if strings.HasPrefix(integerPart, "-") {
			isNegative = true
			integerPart = integerPart[1:]
		}

		var withCommas []string
		for len(integerPart) > 3 {
			withCommas = append([]string{integerPart[len(integerPart)-3:]}, withCommas...)
			integerPart = integerPart[:len(integerPart)-3]
		}
		if len(integerPart) > 0 {
			withCommas = append([]string{integerPart}, withCommas...)
		}
		resultString := strings.Join(withCommas, ",")
		if isNegative {
			resultString = "-" + resultString
		}
		if len(parts) > 1 {
			resultString = resultString + "." + parts[1]
		}
		return resultString
	}
	return formatted
}

func (evaluator *Evaluator) formatTemporalValue(value any, argumentExpression string, context *Context, defaultGoLayout string) (string, error) {
	if value == nil {
		return "", nil
	}

	goLayout := defaultGoLayout
	if argumentExpression != "" {
		fmtVal, err := evaluator.evaluateValue(argumentExpression, context)
		if err != nil {
			return "", err
		}
		pattern := fmt.Sprintf("%v", fmtVal)
		goLayout = javaToGoTimeLayout(pattern)
	}

	var parsedTime time.Time
	if tVal, isTime := value.(time.Time); isTime {
		parsedTime = tVal
	} else if stringVal, isString := value.(string); isString {
		var parseErr error
		parsedTime, parseErr = parseTimeString(stringVal)
		if parseErr != nil {
			return "", parseErr
		}
	} else {
		reflectVal := reflect.ValueOf(value)
		for reflectVal.Kind() == reflect.Ptr || reflectVal.Kind() == reflect.Interface {
			if reflectVal.IsNil() {
				return "", nil
			}
			reflectVal = reflectVal.Elem()
		}
		if reflectVal.CanInterface() {
			if tVal, isTime := reflectVal.Interface().(time.Time); isTime {
				parsedTime = tVal
			} else {
				return "", fmt.Errorf("value is not a date/time value: %T", value)
			}
		} else {
			return "", fmt.Errorf("value is not a date/time value: %T", value)
		}
	}

	if parsedTime.IsZero() {
		return "", nil
	}

	return parsedTime.Format(goLayout), nil
}

func parseTimeString(sourceString string) (time.Time, error) {
	trimmedString := strings.TrimSpace(sourceString)
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
		"15:04:05",
	}

	for _, layout := range layouts {
		if parsedTime, err := time.ParseInLocation(layout, trimmedString, time.Local); err == nil {
			return parsedTime, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date/time text: %s", sourceString)
}

func javaToGoTimeLayout(pattern string) string {
	replacements := []struct {
		java string
		goS  string
	}{
		{"yyyy", "2006"},
		{"yy", "06"},
		{"MM", "01"},
		{"dd", "02"},
		{"HH", "15"},
		{"hh", "03"},
		{"mm", "04"},
		{"ss", "05"},
		{"a", "PM"},
		{"Z", "-0700"},
		{"z", "MST"},
	}

	resultString := pattern
	for _, replacement := range replacements {
		resultString = strings.ReplaceAll(resultString, replacement.java, replacement.goS)
	}
	return resultString
}

// PropertyReader implementation

type pathSegment struct {
	name     string
	optional bool
}

type parsedPath struct {
	rootName string
	segments []pathSegment
}

func (evaluator *Evaluator) readPath(expression string, context *Context) (any, error) {
	parsedPathObj := evaluator.parsePath(expression)
	if parsedPathObj.rootName == "" {
		return nil, nil
	}

	currentVal := context.Get(parsedPathObj.rootName)

	for _, segment := range parsedPathObj.segments {
		if currentVal == nil {
			if segment.optional {
				return nil, nil
			}
			return nil, fmt.Errorf("cannot read property %q on nil source", segment.name)
		}
		var err error
		currentVal, err = evaluator.readProperty(currentVal, segment.name, segment.optional)
		if err != nil {
			return nil, err
		}
	}
	return currentVal, nil
}

func (evaluator *Evaluator) readProperty(source any, name string, optional bool) (any, error) {
	if source == nil {
		if optional {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read property %q on nil source", name)
	}

	reflectVal := reflect.ValueOf(source)
	for reflectVal.Kind() == reflect.Ptr || reflectVal.Kind() == reflect.Interface {
		if reflectVal.IsNil() {
			if optional {
				return nil, nil
			}
			return nil, fmt.Errorf("cannot read property %q on nil pointer", name)
		}
		reflectVal = reflectVal.Elem()
	}

	cleanName := strings.TrimSuffix(name, "()")
	if reflectVal.Kind() == reflect.Slice || reflectVal.Kind() == reflect.Array || reflectVal.Kind() == reflect.String {
		if cleanName == "size" || cleanName == "length" || cleanName == "count" {
			return reflectVal.Len(), nil
		}
	}

	if reflectVal.Kind() == reflect.Map {
		keyType := reflectVal.Type().Key()
		var mapKey reflect.Value

		if keyType.Kind() == reflect.String {
			targetVal := reflect.New(keyType).Elem()
			targetVal.SetString(name)
			mapKey = targetVal
		} else if keyType.Kind() >= reflect.Int && keyType.Kind() <= reflect.Int64 {
			targetVal := reflect.New(keyType).Elem()
			if parsedInt, err := strconv.ParseInt(name, 10, 64); err == nil && !targetVal.OverflowInt(parsedInt) {
				targetVal.SetInt(parsedInt)
				mapKey = targetVal
			}
		} else if keyType.Kind() >= reflect.Uint && keyType.Kind() <= reflect.Uintptr {
			targetVal := reflect.New(keyType).Elem()
			if parsedUint, err := strconv.ParseUint(name, 10, 64); err == nil && !targetVal.OverflowUint(parsedUint) {
				targetVal.SetUint(parsedUint)
				mapKey = targetVal
			}
		} else {
			stringVal := reflect.ValueOf(name)
			if stringVal.Type().AssignableTo(keyType) {
				mapKey = stringVal
			} else if stringVal.Type().ConvertibleTo(keyType) {
				mapKey = stringVal.Convert(keyType)
			}
		}

		if mapKey.IsValid() && mapKey.Type().AssignableTo(keyType) {
			mapVal := reflectVal.MapIndex(mapKey)
			if mapVal.IsValid() {
				return mapVal.Interface(), nil
			}
		}
		if cleanName == "size" || cleanName == "length" || cleanName == "count" {
			return reflectVal.Len(), nil
		}
		return nil, nil
	}

	if reflectVal.Kind() == reflect.Struct {
		// Try Method first
		methodNames := []string{
			name,
			capitalize(name),
			"Get" + capitalize(name),
			"Is" + capitalize(name),
		}

		for _, methodName := range methodNames {
			method := reflectVal.MethodByName(methodName)
			if !method.IsValid() && reflectVal.CanAddr() {
				method = reflectVal.Addr().MethodByName(methodName)
			}

			if method.IsValid() && method.Type().NumIn() == 0 && method.Type().NumOut() >= 1 {
				callResults := method.Call(nil)
				if len(callResults) == 2 && method.Type().Out(1).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
					if !callResults[1].IsNil() {
						return nil, callResults[1].Interface().(error)
					}
				}
				return callResults[0].Interface(), nil
			}
		}

		// Try Field
		fieldVal := reflectVal.FieldByName(name)
		if !fieldVal.IsValid() {
			fieldVal = reflectVal.FieldByName(capitalize(name))
		}

		if fieldVal.IsValid() && fieldVal.CanInterface() {
			return fieldVal.Interface(), nil
		}

		if optional {
			return nil, nil
		}
		return nil, fmt.Errorf("property %q not found on struct %T", name, source)
	}

	if optional {
		return nil, nil
	}
	return nil, fmt.Errorf("cannot read property %q on value of type %T (not a map or struct)", name, source)
}

func (evaluator *Evaluator) parsePath(expression string) parsedPath {
	trimmed := strings.TrimSpace(expression)
	if trimmed == "" {
		return parsedPath{}
	}

	var segments []pathSegment
	var currentBuilder strings.Builder
	var rootName string
	nextSegmentOptional := false

	characterIndex := 0
	for characterIndex < len(trimmed) {
		currentByte := trimmed[characterIndex]

		if currentByte == '.' {
			if currentBuilder.Len() == 0 {
				// Invalid path, e.g. starting with .
				return parsedPath{}
			}

			if rootName == "" {
				rootName = currentBuilder.String()
			} else {
				segments = append(segments, pathSegment{
					name:     currentBuilder.String(),
					optional: nextSegmentOptional,
				})
			}
			currentBuilder.Reset()
			nextSegmentOptional = false
			characterIndex++
			continue
		}

		if currentByte == '?' && characterIndex+1 < len(trimmed) && trimmed[characterIndex+1] == '.' {
			if currentBuilder.Len() == 0 {
				return parsedPath{}
			}

			if rootName == "" {
				rootName = currentBuilder.String()
			} else {
				segments = append(segments, pathSegment{
					name:     currentBuilder.String(),
					optional: nextSegmentOptional,
				})
			}
			currentBuilder.Reset()
			nextSegmentOptional = true
			characterIndex += 2
			continue
		}

		currentBuilder.WriteByte(currentByte)
		characterIndex++
	}

	if currentBuilder.Len() > 0 {
		if rootName == "" {
			rootName = currentBuilder.String()
		} else {
			segments = append(segments, pathSegment{
				name:     currentBuilder.String(),
				optional: nextSegmentOptional,
			})
		}
	}

	return parsedPath{
		rootName: rootName,
		segments: segments,
	}
}

// Helpers

func isWhitespaceChar(character byte) bool {
	return character == ' ' || character == '\t' || character == '\n' || character == '\r'
}

func toFloat64(value any) (float64, bool) {
	if value == nil {
		return 0, false
	}
	reflectVal := reflect.ValueOf(value)
	for reflectVal.Kind() == reflect.Ptr || reflectVal.Kind() == reflect.Interface {
		if reflectVal.IsNil() {
			return 0, false
		}
		reflectVal = reflectVal.Elem()
	}

	switch reflectVal.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(reflectVal.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(reflectVal.Uint()), true
	case reflect.Float32, reflect.Float64:
		return reflectVal.Float(), true
	case reflect.String:
		if parsedFloat, err := strconv.ParseFloat(reflectVal.String(), 64); err == nil {
			return parsedFloat, true
		}
	}
	return 0, false
}

func capitalize(sourceString string) string {
	if sourceString == "" {
		return ""
	}
	return strings.ToUpper(sourceString[:1]) + sourceString[1:]
}

type binaryArithDesc struct {
	left     string
	operator string
	right    string
}

func (evaluator *Evaluator) findBinaryArithmetic(expression string) *binaryArithDesc {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0
	bracketDepth := 0

	for characterIndex := len(expression) - 1; characterIndex >= 0; characterIndex-- {
		character := expression[characterIndex]

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

		if character == ')' {
			parenthesisDepth++
			continue
		}
		if character == '(' {
			parenthesisDepth--
			continue
		}
		if character == ']' {
			bracketDepth++
			continue
		}
		if character == '[' {
			bracketDepth--
			continue
		}
		if parenthesisDepth != 0 || bracketDepth != 0 {
			continue
		}

		if (character == '+' || character == '-') && characterIndex > 0 && characterIndex < len(expression)-1 {
			leftPart := strings.TrimSpace(expression[:characterIndex])
			if leftPart != "" && !strings.HasSuffix(leftPart, "+") && !strings.HasSuffix(leftPart, "-") && !strings.HasSuffix(leftPart, "*") && !strings.HasSuffix(leftPart, "/") && !strings.HasSuffix(leftPart, "(") && !strings.HasSuffix(leftPart, ",") {
				return &binaryArithDesc{
					left:     leftPart,
					operator: string(character),
					right:    strings.TrimSpace(expression[characterIndex+1:]),
				}
			}
		}
	}

	for characterIndex := len(expression) - 1; characterIndex >= 0; characterIndex-- {
		character := expression[characterIndex]

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

		if character == ')' {
			parenthesisDepth++
			continue
		}
		if character == '(' {
			parenthesisDepth--
			continue
		}
		if character == ']' {
			bracketDepth++
			continue
		}
		if character == '[' {
			bracketDepth--
			continue
		}
		if parenthesisDepth != 0 || bracketDepth != 0 {
			continue
		}

		if (character == '*' || character == '/' || character == '%') && characterIndex > 0 && characterIndex < len(expression)-1 {
			return &binaryArithDesc{
				left:     strings.TrimSpace(expression[:characterIndex]),
				operator: string(character),
				right:    strings.TrimSpace(expression[characterIndex+1:]),
			}
		}
	}

	return nil
}

type numberKind int

const (
	numberKindSignedInt numberKind = iota
	numberKindUnsignedInt
	numberKindFloat
)

type numberValue struct {
	kind     numberKind
	intVal   int64
	uintVal  uint64
	floatVal float64
}

func parseNumberValue(value any) (numberValue, bool) {
	if value == nil {
		return numberValue{}, false
	}

	reflectVal := reflect.ValueOf(value)
	for reflectVal.Kind() == reflect.Ptr || reflectVal.Kind() == reflect.Interface {
		if reflectVal.IsNil() {
			return numberValue{}, false
		}
		reflectVal = reflectVal.Elem()
	}

	switch reflectVal.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return numberValue{kind: numberKindSignedInt, intVal: reflectVal.Int()}, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return numberValue{kind: numberKindUnsignedInt, uintVal: reflectVal.Uint()}, true
	case reflect.Float32, reflect.Float64:
		f := reflectVal.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return numberValue{}, false
		}
		return numberValue{kind: numberKindFloat, floatVal: f}, true
	case reflect.String:
		s := strings.TrimSpace(reflectVal.String())
		if s == "" {
			return numberValue{}, false
		}
		if !strings.ContainsAny(s, ".eE") {
			if i, err := strconv.ParseInt(s, 10, 64); err == nil {
				return numberValue{kind: numberKindSignedInt, intVal: i}, true
			}
			if u, err := strconv.ParseUint(s, 10, 64); err == nil {
				return numberValue{kind: numberKindUnsignedInt, uintVal: u}, true
			}
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return numberValue{}, false
			}
			return numberValue{kind: numberKindFloat, floatVal: f}, true
		}
	}

	return numberValue{}, false
}

func (num numberValue) asFloat() float64 {
	switch num.kind {
	case numberKindSignedInt:
		return float64(num.intVal)
	case numberKindUnsignedInt:
		return float64(num.uintVal)
	default:
		return num.floatVal
	}
}

func applyCmp(cmp int, operator string) (bool, error) {
	switch operator {
	case "==":
		return cmp == 0, nil
	case "!=":
		return cmp != 0, nil
	case ">":
		return cmp > 0, nil
	case ">=":
		return cmp >= 0, nil
	case "<":
		return cmp < 0, nil
	case "<=":
		return cmp <= 0, nil
	default:
		return false, fmt.Errorf("unsupported operator: %s", operator)
	}
}

func compareNumbers(left, right numberValue, operator string) (bool, error) {
	if left.kind != numberKindFloat && right.kind != numberKindFloat {
		var cmp int
		if left.kind == numberKindSignedInt && right.kind == numberKindSignedInt {
			if left.intVal < right.intVal {
				cmp = -1
			} else if left.intVal > right.intVal {
				cmp = 1
			} else {
				cmp = 0
			}
		} else if left.kind == numberKindUnsignedInt && right.kind == numberKindUnsignedInt {
			if left.uintVal < right.uintVal {
				cmp = -1
			} else if left.uintVal > right.uintVal {
				cmp = 1
			} else {
				cmp = 0
			}
		} else if left.kind == numberKindSignedInt && right.kind == numberKindUnsignedInt {
			if left.intVal < 0 {
				cmp = -1
			} else if uint64(left.intVal) < right.uintVal {
				cmp = -1
			} else if uint64(left.intVal) > right.uintVal {
				cmp = 1
			} else {
				cmp = 0
			}
		} else { // left is Unsigned, right is Signed
			if right.intVal < 0 {
				cmp = 1
			} else if left.uintVal < uint64(right.intVal) {
				cmp = -1
			} else if left.uintVal > uint64(right.intVal) {
				cmp = 1
			} else {
				cmp = 0
			}
		}

		return applyCmp(cmp, operator)
	}

	if left.kind == numberKindFloat && right.kind == numberKindFloat {
		if math.IsNaN(left.floatVal) || math.IsNaN(right.floatVal) {
			if operator == "!=" {
				return true, nil
			}
			return false, nil
		}
		var cmp int
		if left.floatVal < right.floatVal {
			cmp = -1
		} else if left.floatVal > right.floatVal {
			cmp = 1
		} else {
			cmp = 0
		}
		return applyCmp(cmp, operator)
	}

	// Mixed Float and Integer (Signed or Unsigned)
	floatNum := left
	intNum := right
	isLeftFloat := (left.kind == numberKindFloat)
	if !isLeftFloat {
		floatNum = right
		intNum = left
	}

	if math.IsNaN(floatNum.floatVal) {
		if operator == "!=" {
			return true, nil
		}
		return false, nil
	}
	if math.IsInf(floatNum.floatVal, 1) {
		var cmp int
		if isLeftFloat {
			cmp = 1
		} else {
			cmp = -1
		}
		return applyCmp(cmp, operator)
	}
	if math.IsInf(floatNum.floatVal, -1) {
		var cmp int
		if isLeftFloat {
			cmp = -1
		} else {
			cmp = 1
		}
		return applyCmp(cmp, operator)
	}

	floatRat := new(big.Rat).SetFloat64(floatNum.floatVal)
	if floatRat == nil {
		return false, fmt.Errorf("invalid float value")
	}

	intRat := new(big.Rat)
	if intNum.kind == numberKindSignedInt {
		intRat.SetInt64(intNum.intVal)
	} else {
		intRat.SetUint64(intNum.uintVal)
	}

	var cmp int
	if isLeftFloat {
		cmp = floatRat.Cmp(intRat)
	} else {
		cmp = intRat.Cmp(floatRat)
	}
	return applyCmp(cmp, operator)
}

func (evaluator *Evaluator) evaluateArithmetic(leftNum, rightNum numberValue, operator string) (any, error) {
	if leftNum.kind != numberKindFloat && rightNum.kind != numberKindFloat {
		leftBig := new(big.Int)
		if leftNum.kind == numberKindSignedInt {
			leftBig.SetInt64(leftNum.intVal)
		} else {
			leftBig.SetUint64(leftNum.uintVal)
		}

		rightBig := new(big.Int)
		if rightNum.kind == numberKindSignedInt {
			rightBig.SetInt64(rightNum.intVal)
		} else {
			rightBig.SetUint64(rightNum.uintVal)
		}

		resBig := new(big.Int)
		switch operator {
		case "+":
			resBig.Add(leftBig, rightBig)
		case "-":
			resBig.Sub(leftBig, rightBig)
		case "*":
			resBig.Mul(leftBig, rightBig)
		case "/":
			if rightBig.Sign() == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			resBig.Quo(leftBig, rightBig)
		case "%":
			return performModulo(leftNum, rightNum)
		default:
			return nil, fmt.Errorf("unsupported operator: %s", operator)
		}

		// Check bounds based on operand types
		if leftNum.kind == numberKindUnsignedInt && rightNum.kind == numberKindUnsignedInt {
			if operator == "-" && resBig.Sign() < 0 {
				return nil, fmt.Errorf("unsigned integer underflow")
			}
			if resBig.Sign() < 0 {
				return nil, fmt.Errorf("unsigned integer overflow")
			}
			if resBig.IsUint64() {
				return resBig.Uint64(), nil
			}
			return nil, fmt.Errorf("unsigned integer overflow")
		}

		if leftNum.kind == numberKindSignedInt && rightNum.kind == numberKindSignedInt {
			if resBig.IsInt64() {
				return resBig.Int64(), nil
			}
			return nil, fmt.Errorf("integer overflow")
		}

		// Mixed signed/unsigned integer arithmetic
		if resBig.Sign() >= 0 {
			if leftNum.kind == numberKindUnsignedInt || rightNum.kind == numberKindUnsignedInt {
				if resBig.IsUint64() {
					if resBig.IsInt64() {
						return resBig.Int64(), nil
					}
					return resBig.Uint64(), nil
				}
				return nil, fmt.Errorf("integer overflow")
			}
		}

		if resBig.IsInt64() {
			return resBig.Int64(), nil
		}
		if resBig.IsUint64() {
			return resBig.Uint64(), nil
		}
		return nil, fmt.Errorf("integer overflow")
	}

	// Floating-point arithmetic
	if math.IsNaN(leftNum.asFloat()) || math.IsNaN(rightNum.asFloat()) || math.IsInf(leftNum.asFloat(), 0) || math.IsInf(rightNum.asFloat(), 0) {
		return nil, fmt.Errorf("invalid float operand")
	}

	switch operator {
	case "+":
		res := leftNum.asFloat() + rightNum.asFloat()
		if math.IsNaN(res) || math.IsInf(res, 0) {
			return nil, fmt.Errorf("float arithmetic overflow")
		}
		return res, nil
	case "-":
		res := leftNum.asFloat() - rightNum.asFloat()
		if math.IsNaN(res) || math.IsInf(res, 0) {
			return nil, fmt.Errorf("float arithmetic overflow")
		}
		return res, nil
	case "*":
		res := leftNum.asFloat() * rightNum.asFloat()
		if math.IsNaN(res) || math.IsInf(res, 0) {
			return nil, fmt.Errorf("float arithmetic overflow")
		}
		return res, nil
	case "/":
		if rightNum.asFloat() == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		res := leftNum.asFloat() / rightNum.asFloat()
		if math.IsNaN(res) || math.IsInf(res, 0) {
			return nil, fmt.Errorf("float arithmetic overflow")
		}
		return res, nil
	case "%":
		return nil, fmt.Errorf("modulo requires integer operands")
	default:
		return nil, fmt.Errorf("unsupported operator: %s", operator)
	}
}

func performModulo(left, right numberValue) (any, error) {
	if left.kind == numberKindFloat || right.kind == numberKindFloat {
		return nil, fmt.Errorf("modulo requires integer operands")
	}

	var rInt int64
	if right.kind == numberKindSignedInt {
		rInt = right.intVal
	} else {
		if right.uintVal > math.MaxInt64 {
			return nil, fmt.Errorf("modulo divisor out of range")
		}
		rInt = int64(right.uintVal)
	}

	if rInt == 0 {
		return nil, fmt.Errorf("division by zero")
	}

	if left.kind == numberKindSignedInt {
		return left.intVal % rInt, nil
	}
	return int64(left.uintVal % uint64(rInt)), nil
}
