package pte

import (
	"fmt"
	"math"
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

func (e *Evaluator) Evaluate(expression string, context *Context) (any, error) {
	trimmedExpression := strings.TrimSpace(expression)
	if trimmedExpression == "" {
		return nil, nil
	}
	return e.evaluateValue(trimmedExpression, context)
}

func (e *Evaluator) EvaluateBoolean(expression string, context *Context) (bool, error) {
	val, err := e.evaluateCondition(strings.TrimSpace(expression), context)
	if err != nil {
		return false, err
	}
	return e.toBoolean(val), nil
}

func (e *Evaluator) ValuesEqual(left, right any) bool {
	if left == nil || right == nil {
		return left == right
	}

	leftNum, isLeftNum := toFloat64(left)
	rightNum, isRightNum := toFloat64(right)
	if isLeftNum && isRightNum {
		return math.Abs(leftNum-rightNum) < 1e-9
	}

	return reflect.DeepEqual(left, right)
}

func (e *Evaluator) evaluateCondition(expression string, context *Context) (any, error) {
	trimmedExpression := strings.TrimSpace(expression)

	if idx := e.findWordOperator(trimmedExpression, "nor"); idx != -1 {
		left := trimmedExpression[:idx]
		right := trimmedExpression[idx+len("nor"):]
		leftBool, err := e.EvaluateBoolean(left, context)
		if err != nil {
			return nil, err
		}
		rightBool, err := e.EvaluateBoolean(right, context)
		if err != nil {
			return nil, err
		}
		return !(leftBool || rightBool), nil
	}

	if idx := e.findWordOperator(trimmedExpression, "or"); idx != -1 {
		left := trimmedExpression[:idx]
		right := trimmedExpression[idx+len("or"):]
		leftBool, err := e.EvaluateBoolean(left, context)
		if err != nil {
			return nil, err
		}
		rightBool, err := e.EvaluateBoolean(right, context)
		if err != nil {
			return nil, err
		}
		return leftBool || rightBool, nil
	}

	if idx := e.findWordOperator(trimmedExpression, "nand"); idx != -1 {
		left := trimmedExpression[:idx]
		right := trimmedExpression[idx+len("nand"):]
		leftBool, err := e.EvaluateBoolean(left, context)
		if err != nil {
			return nil, err
		}
		rightBool, err := e.EvaluateBoolean(right, context)
		if err != nil {
			return nil, err
		}
		return !(leftBool && rightBool), nil
	}

	if idx := e.findWordOperator(trimmedExpression, "and"); idx != -1 {
		left := trimmedExpression[:idx]
		right := trimmedExpression[idx+len("and"):]
		leftBool, err := e.EvaluateBoolean(left, context)
		if err != nil {
			return nil, err
		}
		rightBool, err := e.EvaluateBoolean(right, context)
		if err != nil {
			return nil, err
		}
		return leftBool && rightBool, nil
	}

	if e.startsWithWord(trimmedExpression, "not") {
		valueExpr := strings.TrimSpace(trimmedExpression[len("not"):])
		valBool, err := e.EvaluateBoolean(valueExpr, context)
		if err != nil {
			return nil, err
		}
		return !valBool, nil
	}

	if comp := e.findComparison(trimmedExpression); comp != nil {
		leftVal, err := e.evaluateValue(comp.left, context)
		if err != nil {
			return nil, err
		}
		rightVal, err := e.evaluateValue(comp.right, context)
		if err != nil {
			return nil, err
		}
		return e.compare(leftVal, rightVal, comp.operator)
	}

	return e.evaluateValue(trimmedExpression, context)
}

func (e *Evaluator) evaluateValue(expression string, context *Context) (any, error) {
	trimmedExpression := e.removeWrappingParentheses(strings.TrimSpace(expression))
	if trimmedExpression == "" {
		return nil, nil
	}

	if filtered := e.parseFilteredExpression(trimmedExpression); filtered != nil {
		return e.evaluateFilteredExpression(filtered, context)
	}

	if ternary := e.findTernaryExpression(trimmedExpression); ternary != nil {
		condBool, err := e.EvaluateBoolean(ternary.condition, context)
		if err != nil {
			return nil, err
		}
		if condBool {
			return e.evaluateValue(ternary.trueExpression, context)
		}
		return e.evaluateValue(ternary.falseExpression, context)
	}

	if idx := e.findNullCoalescingOperator(trimmedExpression); idx != -1 {
		leftExpr := strings.TrimSpace(trimmedExpression[:idx])
		rightExpr := strings.TrimSpace(trimmedExpression[idx+2:])
		leftVal, err := e.evaluateValue(leftExpr, context)
		if err != nil {
			return nil, err
		}
		if leftVal != nil {
			return leftVal, nil
		}
		return e.evaluateValue(rightExpr, context)
	}

	if e.isQuotedString(trimmedExpression) {
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

	if e.isNumber(trimmedExpression) {
		val, err := strconv.ParseFloat(trimmedExpression, 64)
		if err != nil {
			return nil, err
		}
		return val, nil
	}

	return e.readPath(trimmedExpression, context)
}

func (e *Evaluator) compare(left, right any, operator string) (bool, error) {
	leftNum, isLeftNum := toFloat64(left)
	rightNum, isRightNum := toFloat64(right)

	if isLeftNum && isRightNum {
		switch operator {
		case "==":
			return math.Abs(leftNum-rightNum) < 1e-9, nil
		case "!=":
			return math.Abs(leftNum-rightNum) >= 1e-9, nil
		case ">":
			return leftNum > rightNum, nil
		case ">=":
			return leftNum >= rightNum, nil
		case "<":
			return leftNum < rightNum, nil
		case "<=":
			return leftNum <= rightNum, nil
		default:
			return false, fmt.Errorf("unsupported operator: %s", operator)
		}
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

func (e *Evaluator) toBoolean(value any) bool {
	if value == nil {
		return false
	}

	val := reflect.ValueOf(value)
	for val.Kind() == reflect.Ptr || val.Kind() == reflect.Interface {
		if val.IsNil() {
			return false
		}
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Bool:
		return val.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return val.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return val.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return val.Float() != 0.0
	case reflect.String:
		return strings.TrimSpace(val.String()) != ""
	case reflect.Slice, reflect.Array:
		return val.Len() > 0
	case reflect.Map:
		return val.Len() > 0
	}

	return true
}

func (e *Evaluator) isQuotedString(value string) bool {
	return len(value) >= 2 && (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") ||
		strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'"))
}

var numberRegex = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

func (e *Evaluator) isNumber(value string) bool {
	return numberRegex.MatchString(value)
}

func (e *Evaluator) startsWithWord(expression, word string) bool {
	return expression == word || strings.HasPrefix(expression, word+" ")
}

func (e *Evaluator) findWordOperator(expression, operator string) int {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0

	opLen := len(operator)
	for index := 0; index <= len(expression)-opLen; index++ {
		current := expression[index]

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

		if strings.HasPrefix(expression[index:], operator) {
			beforeIsBoundary := index == 0 || isWhitespaceChar(expression[index-1])
			afterIndex := index + opLen
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

func (e *Evaluator) findComparison(expression string) *comparisonDesc {
	operators := []string{"==", "!=", ">=", "<=", ">", "<"}

	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0

	for index := 0; index < len(expression); index++ {
		current := expression[index]

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

		for _, op := range operators {
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

func (e *Evaluator) removeWrappingParentheses(expression string) string {
	result := expression
	for strings.HasPrefix(result, "(") && strings.HasSuffix(result, ")") && e.wrapsWholeExpression(result) {
		result = strings.TrimSpace(result[1 : len(result)-1])
	}
	return result
}

func (e *Evaluator) wrapsWholeExpression(expression string) bool {
	depth := 0
	insideSingleQuote := false
	insideDoubleQuote := false

	for index := 0; index < len(expression); index++ {
		current := expression[index]

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
			depth++
		}
		if current == ')' {
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

func (e *Evaluator) findTernaryExpression(expression string) *ternaryDesc {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0
	questionIndex := -1

	for index := 0; index < len(expression); index++ {
		current := expression[index]

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

		if current != '?' {
			continue
		}

		if e.isOptionalChainingQuestionMark(expression, index) {
			continue
		}
		if e.isNullCoalescingQuestionMark(expression, index) {
			continue
		}

		questionIndex = index
		break
	}

	if questionIndex == -1 {
		return nil
	}

	colonIndex := e.findTernaryColon(expression, questionIndex+1)
	if colonIndex == -1 {
		return nil // Invalid or incomplete
	}

	return &ternaryDesc{
		condition:       strings.TrimSpace(expression[:questionIndex]),
		trueExpression:  strings.TrimSpace(expression[questionIndex+1 : colonIndex]),
		falseExpression: strings.TrimSpace(expression[colonIndex+1:]),
	}
}

func (e *Evaluator) findTernaryColon(expression string, startIndex int) int {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0
	nestedTernaryDepth := 0

	for index := startIndex; index < len(expression); index++ {
		current := expression[index]

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

		if current == '?' {
			if e.isOptionalChainingQuestionMark(expression, index) {
				continue
			}
			if e.isNullCoalescingQuestionMark(expression, index) {
				continue
			}
			nestedTernaryDepth++
			continue
		}

		if current == ':' {
			if nestedTernaryDepth == 0 {
				return index
			}
			nestedTernaryDepth--
		}
	}
	return -1
}

func (e *Evaluator) isOptionalChainingQuestionMark(expression string, index int) bool {
	return index+1 < len(expression) && expression[index+1] == '.'
}

func (e *Evaluator) isNullCoalescingQuestionMark(expression string, index int) bool {
	previousIsQuestionMark := index > 0 && expression[index-1] == '?'
	nextIsQuestionMark := index+1 < len(expression) && expression[index+1] == '?'
	return previousIsQuestionMark || nextIsQuestionMark
}

func (e *Evaluator) findNullCoalescingOperator(expression string) int {
	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0

	for index := 0; index < len(expression)-1; index++ {
		current := expression[index]

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

func (e *Evaluator) parseFilteredExpression(expression string) *filteredExpressionDesc {
	parts := e.splitByTopLevelComma(expression)
	if len(parts) <= 1 {
		return nil
	}
	return &filteredExpressionDesc{
		valueExpression: parts[0],
		filters:         parts[1:],
	}
}

func (e *Evaluator) splitByTopLevelComma(expression string) []string {
	var parts []string
	var currentPart strings.Builder

	insideSingleQuote := false
	insideDoubleQuote := false
	parenthesisDepth := 0
	bracketDepth := 0
	braceDepth := 0

	for index := 0; index < len(expression); index++ {
		current := expression[index]

		if current == '\'' && !insideDoubleQuote {
			insideSingleQuote = !insideSingleQuote
			currentPart.WriteByte(current)
			continue
		}
		if current == '"' && !insideSingleQuote {
			insideDoubleQuote = !insideDoubleQuote
			currentPart.WriteByte(current)
			continue
		}

		if !insideSingleQuote && !insideDoubleQuote {
			if current == '(' {
				parenthesisDepth++
				currentPart.WriteByte(current)
				continue
			}
			if current == ')' {
				parenthesisDepth--
				currentPart.WriteByte(current)
				continue
			}
			if current == '[' {
				bracketDepth++
				currentPart.WriteByte(current)
				continue
			}
			if current == ']' {
				bracketDepth--
				currentPart.WriteByte(current)
				continue
			}
			if current == '{' {
				braceDepth++
				currentPart.WriteByte(current)
				continue
			}
			if current == '}' {
				braceDepth--
				currentPart.WriteByte(current)
				continue
			}

			if parenthesisDepth == 0 && bracketDepth == 0 && braceDepth == 0 && current == ',' {
				part := strings.TrimSpace(currentPart.String())
				parts = append(parts, part)
				currentPart.Reset()
				continue
			}
		}
		currentPart.WriteByte(current)
	}

	lastPart := strings.TrimSpace(currentPart.String())
	parts = append(parts, lastPart)
	return parts
}

func (e *Evaluator) evaluateFilteredExpression(filtered *filteredExpressionDesc, context *Context) (any, error) {
	val, err := e.evaluateValue(filtered.valueExpression, context)
	if err != nil {
		return nil, err
	}

	for _, filterSource := range filtered.filters {
		val, err = e.applyFilter(val, filterSource, context)
		if err != nil {
			return nil, err
		}
	}
	return val, nil
}

type filterCallDesc struct {
	name               string
	argumentExpression string
}

func (e *Evaluator) parseFilterCall(filterSource string) filterCallDesc {
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

func (e *Evaluator) applyFilter(val any, filterSource string, context *Context) (any, error) {
	call := e.parseFilterCall(filterSource)

	switch call.name {
	case "upper":
		return strings.ToUpper(e.stringValue(val)), nil
	case "lower":
		return strings.ToLower(e.stringValue(val)), nil
	case "trim":
		return strings.TrimSpace(e.stringValue(val)), nil
	case "capitalize":
		return e.capitalizeText(e.stringValue(val)), nil
	case "slug":
		return e.slugify(e.stringValue(val)), nil
	case "length":
		return e.lengthOf(val), nil
	case "default":
		return e.defaultValue(val, call.argumentExpression, context)
	case "currency":
		return e.currencyValue(val, call.argumentExpression, context)
	case "number":
		return e.numberValue(val, call.argumentExpression, context)
	case "date":
		return e.formatTemporalValue(val, call.argumentExpression, context, "2006-01-02")
	case "time":
		return e.formatTemporalValue(val, call.argumentExpression, context, "15:04:05")
	case "datetime":
		return e.formatTemporalValue(val, call.argumentExpression, context, "2006-01-02 15:04:05")
	default:
		return nil, fmt.Errorf("unknown filter: %s", call.name)
	}
}

func (e *Evaluator) stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}

func (e *Evaluator) capitalizeText(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

var slugRemoveRegex = regexp.MustCompile(`[^a-z0-9\s-]`)
var slugCollapseRegex = regexp.MustCompile(`[\s_]+`)
var slugHyphenCollapseRegex = regexp.MustCompile(`-+`)

func (e *Evaluator) slugify(value string) string {
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

func (e *Evaluator) lengthOf(value any) int {
	if value == nil {
		return 0
	}

	val := reflect.ValueOf(value)
	for val.Kind() == reflect.Ptr || val.Kind() == reflect.Interface {
		if val.IsNil() {
			return 0
		}
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.String:
		return val.Len()
	case reflect.Slice, reflect.Array:
		return val.Len()
	case reflect.Map:
		return val.Len()
	}
	return len(fmt.Sprintf("%v", value))
}

func (e *Evaluator) defaultValue(value any, argumentExpression string, context *Context) (any, error) {
	if argumentExpression == "" {
		return nil, fmt.Errorf("default filter requires an argument")
	}

	if e.toBoolean(value) {
		return value, nil
	}

	return e.evaluateValue(argumentExpression, context)
}

func (e *Evaluator) currencyValue(value any, argumentExpression string, context *Context) (string, error) {
	if value == nil {
		return "", nil
	}

	symbol := ""
	if argumentExpression != "" {
		symVal, err := e.evaluateValue(argumentExpression, context)
		if err != nil {
			return "", err
		}
		symbol = fmt.Sprintf("%v", symVal)
	}

	num, ok := toFloat64(value)
	if !ok {
		return "", fmt.Errorf("value is not numeric: %v", value)
	}

	return symbol + e.formatNumberPattern(num, "#,##0.00"), nil
}

func (e *Evaluator) numberValue(value any, argumentExpression string, context *Context) (string, error) {
	if value == nil {
		return "", nil
	}

	pattern := "#,##0.##"
	if argumentExpression != "" {
		patVal, err := e.evaluateValue(argumentExpression, context)
		if err != nil {
			return "", err
		}
		pattern = fmt.Sprintf("%v", patVal)
	}

	num, ok := toFloat64(value)
	if !ok {
		return "", fmt.Errorf("value is not numeric: %v", value)
	}

	return e.formatNumberPattern(num, pattern), nil
}

func (e *Evaluator) formatNumberPattern(num float64, pattern string) string {
	// A simple pattern formatter that supports common templates like #,##0.00 or #,##0.##
	hasComma := strings.Contains(pattern, ",")
	decimalPlaces := 0
	isVariableDecimal := false

	dotIdx := strings.Index(pattern, ".")
	if dotIdx != -1 {
		decPart := pattern[dotIdx+1:]
		decimalPlaces = len(decPart)
		if strings.Contains(decPart, "#") {
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
		intPart := parts[0]
		// Handle negative sign
		neg := false
		if strings.HasPrefix(intPart, "-") {
			neg = true
			intPart = intPart[1:]
		}

		var withCommas []string
		for len(intPart) > 3 {
			withCommas = append([]string{intPart[len(intPart)-3:]}, withCommas...)
			intPart = intPart[:len(intPart)-3]
		}
		if len(intPart) > 0 {
			withCommas = append([]string{intPart}, withCommas...)
		}
		res := strings.Join(withCommas, ",")
		if neg {
			res = "-" + res
		}
		if len(parts) > 1 {
			res = res + "." + parts[1]
		}
		return res
	}

	return formatted
}

func (e *Evaluator) formatTemporalValue(value any, argumentExpression string, context *Context, defaultPattern string) (string, error) {
	if value == nil {
		return "", nil
	}

	pattern := defaultPattern
	if argumentExpression != "" {
		patVal, err := e.evaluateValue(argumentExpression, context)
		if err != nil {
			return "", err
		}
		pattern = fmt.Sprintf("%v", patVal)
	}

	goLayout := javaToGoTimeLayout(pattern)

	// Retrieve time.Time
	var t time.Time
	switch val := value.(type) {
	case time.Time:
		t = val
	case *time.Time:
		if val != nil {
			t = *val
		}
	case string:
		var err error
		t, err = parseTimeString(val)
		if err != nil {
			return "", err
		}
	default:
		// Attempt reflection for common types
		refVal := reflect.ValueOf(value)
		for refVal.Kind() == reflect.Ptr || refVal.Kind() == reflect.Interface {
			if refVal.IsNil() {
				return "", nil
			}
			refVal = refVal.Elem()
		}
		if refVal.Type().String() == "time.Time" {
			t = refVal.Interface().(time.Time)
		} else {
			return "", fmt.Errorf("value is not a date/time value: %T", value)
		}
	}

	if t.IsZero() {
		return "", nil
	}

	return t.Format(goLayout), nil
}

func parseTimeString(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
		"15:04:05",
	}

	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date/time text: %s", s)
}

func javaToGoTimeLayout(pattern string) string {
	// A basic translator from Java DateTimeFormatter symbols to Go layout symbols
	// Java symbols: yyyy, yy, MM, dd, HH, hh, mm, ss, a, z, Z
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

	result := pattern
	for _, rep := range replacements {
		result = strings.ReplaceAll(result, rep.java, rep.goS)
	}
	return result
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

func (e *Evaluator) readPath(expression string, context *Context) (any, error) {
	path := e.parsePath(expression)
	if path.rootName == "" {
		return nil, nil
	}

	current := context.Get(path.rootName)

	for _, seg := range path.segments {
		if current == nil {
			if seg.optional {
				return nil, nil
			}
			return nil, fmt.Errorf("cannot read property %q on nil source", seg.name)
		}
		var err error
		current, err = e.readProperty(current, seg.name, seg.optional)
		if err != nil {
			return nil, err
		}
	}
	return current, nil
}

func (e *Evaluator) readProperty(source any, name string, optional bool) (any, error) {
	if source == nil {
		if optional {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read property %q on nil source", name)
	}

	val := reflect.ValueOf(source)
	for val.Kind() == reflect.Ptr || val.Kind() == reflect.Interface {
		if val.IsNil() {
			if optional {
				return nil, nil
			}
			return nil, fmt.Errorf("cannot read property %q on nil pointer", name)
		}
		val = val.Elem()
	}

	if val.Kind() == reflect.Map {
		mapKey := reflect.ValueOf(name)
		mapVal := val.MapIndex(mapKey)
		if mapVal.IsValid() {
			return mapVal.Interface(), nil
		}
		return nil, nil
	}

	if val.Kind() == reflect.Struct {
		// Try Method first
		methodNames := []string{
			name,
			capitalize(name),
			"Get" + capitalize(name),
			"Is" + capitalize(name),
		}

		for _, mName := range methodNames {
			method := val.MethodByName(mName)
			if !method.IsValid() && val.CanAddr() {
				method = val.Addr().MethodByName(mName)
			}

			if method.IsValid() && method.Type().NumIn() == 0 && method.Type().NumOut() >= 1 {
				res := method.Call(nil)
				if len(res) == 2 && method.Type().Out(1).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
					if !res[1].IsNil() {
						return nil, res[1].Interface().(error)
					}
				}
				return res[0].Interface(), nil
			}
		}

		// Try Field
		fieldVal := val.FieldByName(name)
		if !fieldVal.IsValid() {
			fieldVal = val.FieldByName(capitalize(name))
		}

		if fieldVal.IsValid() {
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

func (e *Evaluator) parsePath(expression string) parsedPath {
	trimmed := strings.TrimSpace(expression)
	if trimmed == "" {
		return parsedPath{}
	}

	var segments []pathSegment
	var current strings.Builder
	var rootName string
	nextSegmentOptional := false

	index := 0
	for index < len(trimmed) {
		currentByte := trimmed[index]

		if currentByte == '.' {
			if current.Len() == 0 {
				// Invalid path, e.g. starting with .
				return parsedPath{}
			}

			if rootName == "" {
				rootName = current.String()
			} else {
				segments = append(segments, pathSegment{
					name:     current.String(),
					optional: nextSegmentOptional,
				})
			}
			current.Reset()
			nextSegmentOptional = false
			index++
			continue
		}

		if currentByte == '?' && index+1 < len(trimmed) && trimmed[index+1] == '.' {
			if current.Len() == 0 {
				return parsedPath{}
			}

			if rootName == "" {
				rootName = current.String()
			} else {
				segments = append(segments, pathSegment{
					name:     current.String(),
					optional: nextSegmentOptional,
				})
			}
			current.Reset()
			nextSegmentOptional = true
			index += 2
			continue
		}

		current.WriteByte(currentByte)
		index++
	}

	if current.Len() > 0 {
		if rootName == "" {
			rootName = current.String()
		} else {
			segments = append(segments, pathSegment{
				name:     current.String(),
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

func isWhitespaceChar(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func toFloat64(val any) (float64, bool) {
	if val == nil {
		return 0, false
	}
	v := reflect.ValueOf(val)
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return 0, false
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(v.Uint()), true
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	case reflect.String:
		if f, err := strconv.ParseFloat(v.String(), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
