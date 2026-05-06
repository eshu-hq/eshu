package parser

import (
	"regexp"
	"strings"
)

var (
	dbtBareIdentifierRe         = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	dbtQualifiedReferenceRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\.(?:\*|[A-Za-z_][A-Za-z0-9_]*)$`)
	dbtQualifiedReferenceScanRe = regexp.MustCompile(`\b(?P<alias>[A-Za-z_][A-Za-z0-9_]*)\.(?P<column>\*|[A-Za-z_][A-Za-z0-9_]*)`)
	dbtFunctionCallRe           = regexp.MustCompile(`^(?P<name>[A-Za-z_][A-Za-z0-9_]*)\((?P<arguments>.*)\)$`)
	dbtFunctionCallScanRe       = regexp.MustCompile(`\b(?P<name>[A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	dbtWindowFunctionRe         = regexp.MustCompile(`(?is)^(?P<name>[A-Za-z_][A-Za-z0-9_]*)\((?P<arguments>.*)\)\s+over\s*\((?P<window>.*)\)$`)
	dbtSingleQuotedLiteralRe    = regexp.MustCompile(`(?s)^'(?:[^'\\]|\\.)*'$`)
	dbtSingleQuotedLiteralScan  = regexp.MustCompile(`(?s)'(?:[^'\\]|\\.)*'`)
	dbtNumericLiteralRe         = regexp.MustCompile(`^[+-]?(?:\d+(?:\.\d+)?|\.\d+)$`)
	dbtNumericLiteralScan       = regexp.MustCompile(`(^|[^A-Za-z0-9_])([+-]?(?:\d+(?:\.\d+)?|\.\d+))($|[^A-Za-z0-9_])`)
	dbtTypeIdentifierRe         = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\b`)
	dbtCaseExpressionRe         = regexp.MustCompile(`(?is)^case\b.*\bend$`)
	dbtCaseKeywordRe            = regexp.MustCompile(`(?i)\b(?:case|when|then|else|end|is|null|and|or|not|in|like|between|true|false)\b`)
	dbtQualifiedMacroCallRe     = regexp.MustCompile(`^(?P<name>[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)+)\((?P<arguments>.*)\)$`)
)

var (
	dbtAggregateFunctions          = map[string]struct{}{"avg": {}, "count": {}, "max": {}, "min": {}, "sum": {}}
	dbtSimpleScalarFunctions       = map[string]struct{}{"md5": {}, "upper": {}, "lower": {}, "trim": {}, "ltrim": {}, "rtrim": {}}
	dbtLiteralParameterScalarFuncs = map[string]struct{}{"date_trunc": {}}
	dbtMultiInputRowLevelFunctions = map[string]struct{}{"concat": {}}
)

const (
	dbtAggregateExpressionReason = "aggregate_expression_semantics_not_captured"
	dbtDerivedExpressionReason   = "derived_expression_semantics_not_captured"
	dbtMultiInputExpression      = "multi_input_expression_semantics_not_captured"
	dbtMacroExpressionReason     = "macro_expression_not_resolved"
	dbtTemplatedExpressionReason = "templated_expression_not_resolved"
	dbtWindowExpressionReason    = "window_expression_semantics_not_captured"
)

func expressionHonestyGapReason(expression string) string {
	normalized := stripWrappingParentheses(strings.TrimSpace(expression))
	if normalized == "" {
		return ""
	}
	if inner, ok := unwrapTemplatedExpression(normalized); ok && !dbtBareIdentifierRe.MatchString(inner) {
		if expressionPartialReason(inner) != "" {
			return dbtTemplatedExpressionReason
		}
		normalized = inner
	}
	if strings.Contains(normalized, "{{") || strings.Contains(normalized, "}}") || strings.Contains(normalized, "{%") || strings.Contains(normalized, "%}") {
		return dbtTemplatedExpressionReason
	}
	if dbtQualifiedMacroCallRe.MatchString(normalized) && !macroExpressionHasLineage(normalized) && !isSupportedQualifiedMacroExpression(normalized) {
		return dbtMacroExpressionReason
	}
	return ""
}

func expressionIgnoredIdentifiers(expression string) map[string]struct{} {
	result := make(map[string]struct{})
	valueExpression, typeExpression, ok := supportedCastExpression(expression)
	if !ok {
		return result
	}
	_ = valueExpression
	for _, match := range dbtTypeIdentifierRe.FindAllString(typeExpression, -1) {
		result[match] = struct{}{}
	}
	return result
}

func derivedExpressionGap(expression string, modelName string, reason string) map[string]string {
	return map[string]string{
		"expression": strings.TrimSpace(expression),
		"model_name": modelName,
		"reason":     reason,
	}
}

func expressionPartialReason(expression string) string {
	normalized := stripWrappingParentheses(strings.TrimSpace(expression))
	if normalized == "" {
		return ""
	}
	if inner, ok := unwrapTemplatedExpression(normalized); ok && !dbtBareIdentifierRe.MatchString(inner) {
		if expressionPartialReason(inner) != "" {
			return dbtTemplatedExpressionReason
		}
		normalized = inner
	}
	if reason := expressionHonestyGapReason(normalized); reason != "" {
		return reason
	}
	if dbtQualifiedMacroCallRe.MatchString(normalized) && macroExpressionHasLineage(normalized) {
		return ""
	}
	if dbtBareIdentifierRe.MatchString(normalized) || dbtQualifiedReferenceRe.MatchString(normalized) {
		return ""
	}
	if isSupportedCaseExpression(normalized) || isSupportedArithmeticExpression(normalized) || isSupportedScalarWrapper(normalized) {
		return ""
	}
	if isSupportedQualifiedMacroExpression(normalized) {
		return ""
	}
	if isSupportedAggregateExpression(normalized) {
		return ""
	}
	if isSupportedWindowExpression(normalized) {
		return ""
	}
	if reason := unsupportedFunctionReason(normalized); reason != "" {
		return reason
	}
	return dbtDerivedExpressionReason
}

func macroExpressionHasLineage(expression string) bool {
	matches := dbtQualifiedMacroCallRe.FindStringSubmatch(strings.TrimSpace(expression))
	if matches == nil {
		return false
	}
	return len(simpleReferenceTokens(matches[2])) > 0
}

func isSupportedAggregateExpression(expression string) bool {
	matches := dbtFunctionCallRe.FindStringSubmatch(expression)
	if matches == nil {
		return false
	}
	functionName := strings.ToLower(strings.TrimSpace(matches[1]))
	if _, ok := dbtAggregateFunctions[functionName]; !ok {
		return false
	}
	return supportsRowLevelArguments(splitTopLevelArguments(matches[2]))
}

func isSupportedWindowExpression(expression string) bool {
	matches := dbtWindowFunctionRe.FindStringSubmatch(expression)
	if matches == nil {
		return false
	}
	functionName := strings.ToLower(strings.TrimSpace(matches[1]))
	if _, ok := dbtAggregateFunctions[functionName]; !ok {
		return false
	}
	if !supportsRowLevelArguments(splitTopLevelArguments(matches[2])) {
		return false
	}
	return len(simpleReferenceTokens(expression)) > 0
}

func isSupportedQualifiedMacroExpression(expression string) bool {
	matches := dbtQualifiedMacroCallRe.FindStringSubmatch(expression)
	if matches == nil {
		return false
	}
	return supportsRowLevelArguments(splitTopLevelArguments(matches[2])) && len(simpleReferenceTokens(matches[2])) > 0
}

func expressionTransformMetadata(expression string) map[string]string {
	normalized := stripWrappingParentheses(strings.TrimSpace(expression))
	if normalized == "" || dbtBareIdentifierRe.MatchString(normalized) || dbtQualifiedReferenceRe.MatchString(normalized) {
		return nil
	}
	if expressionHonestyGapReason(normalized) != "" {
		return nil
	}
	if _, _, ok := supportedCastExpression(normalized); ok {
		return map[string]string{"transform_kind": "cast", "transform_expression": normalized}
	}
	if isSupportedCaseExpression(normalized) {
		return map[string]string{"transform_kind": "case", "transform_expression": normalized}
	}
	if isSupportedArithmeticExpression(normalized) {
		return map[string]string{"transform_kind": "arithmetic", "transform_expression": normalized}
	}
	if metadata := partialTransformMetadata(normalized); metadata != nil {
		return metadata
	}
	return supportedFunctionMetadata(normalized)
}

func stripWrappingParentheses(expression string) string {
	normalized := strings.TrimSpace(expression)
	for strings.HasPrefix(normalized, "(") && strings.HasSuffix(normalized, ")") {
		depth := 0
		balanced := true
		for index, character := range normalized {
			switch character {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 && index != len(normalized)-1 {
					balanced = false
				}
			}
			if !balanced {
				break
			}
		}
		if !balanced || depth != 0 {
			return normalized
		}
		normalized = strings.TrimSpace(normalized[1 : len(normalized)-1])
	}
	return normalized
}

func isSupportedScalarWrapper(expression string) bool {
	_, _, ok := supportedCastExpression(expression)
	return ok || supportedFunctionMetadata(expression) != nil
}

func isSupportedRowLevelExpression(expression string) bool {
	normalized := stripWrappingParentheses(strings.TrimSpace(expression))
	if normalized == "" {
		return false
	}
	return isSimpleReferenceExpression(normalized) ||
		isLiteralExpression(normalized) ||
		isSupportedScalarWrapper(normalized) ||
		isSupportedQualifiedMacroExpression(normalized)
}

func isSupportedCaseExpression(expression string) bool {
	if !dbtCaseExpressionRe.MatchString(expression) || hasUnsupportedFunctionCall(expression) {
		return false
	}
	references := simpleReferenceTokens(expression)
	if len(references) == 0 {
		return false
	}
	collapsed := collapsedShape(expression, references, true)
	return regexp.MustCompile(`^[\s()=<>!,+\-*/%]*$`).MatchString(collapsed)
}

func isSupportedArithmeticExpression(expression string) bool {
	if !strings.ContainsAny(expression, "+-*/%") || hasUnsupportedFunctionCall(expression) {
		return false
	}
	references := simpleReferenceTokens(expression)
	if len(references) == 0 {
		return false
	}
	collapsed := collapsedShape(expression, references, false)
	return regexp.MustCompile(`^[\s()+\-*/%]*$`).MatchString(collapsed)
}

func supportedCastExpression(expression string) (string, string, bool) {
	matches := dbtFunctionCallRe.FindStringSubmatch(expression)
	if matches == nil || strings.ToLower(strings.TrimSpace(matches[1])) != "cast" {
		return "", "", false
	}
	valueExpression, typeExpression, ok := splitCastArguments(matches[2])
	if !ok || !isSupportedRowLevelExpression(valueExpression) || strings.TrimSpace(typeExpression) == "" {
		return "", "", false
	}
	return valueExpression, typeExpression, true
}

func unsupportedFunctionReason(expression string) string {
	if matches := dbtWindowFunctionRe.FindStringSubmatch(expression); matches != nil {
		if isSupportedWindowExpression(expression) {
			return ""
		}
		return dbtWindowExpressionReason
	}
	matches := dbtFunctionCallRe.FindStringSubmatch(expression)
	if matches == nil || supportedFunctionMetadata(expression) != nil {
		return ""
	}
	functionName := strings.ToLower(strings.TrimSpace(matches[1]))
	if _, ok := dbtAggregateFunctions[functionName]; ok {
		return dbtAggregateExpressionReason
	}
	arguments := splitTopLevelArguments(matches[2])
	referenceCount := 0
	for _, argument := range arguments {
		if isSimpleReferenceExpression(argument) {
			referenceCount++
		}
	}
	if referenceCount > 1 {
		return dbtMultiInputExpression
	}
	return ""
}

func partialTransformMetadata(expression string) map[string]string {
	if matches := dbtWindowFunctionRe.FindStringSubmatch(expression); matches != nil && len(simpleReferenceTokens(expression)) > 0 {
		return map[string]string{
			"transform_kind":       "window_" + strings.ToLower(strings.TrimSpace(matches[1])),
			"transform_expression": expression,
		}
	}
	matches := dbtFunctionCallRe.FindStringSubmatch(expression)
	if matches == nil {
		return nil
	}
	functionName := strings.ToLower(strings.TrimSpace(matches[1]))
	if _, ok := dbtAggregateFunctions[functionName]; ok && supportsRowLevelArguments(splitTopLevelArguments(matches[2])) {
		return map[string]string{"transform_kind": functionName, "transform_expression": expression}
	}
	return nil
}

func supportedFunctionMetadata(expression string) map[string]string {
	matches := dbtFunctionCallRe.FindStringSubmatch(expression)
	if matches == nil {
		return nil
	}
	functionName := strings.ToLower(strings.TrimSpace(matches[1]))
	arguments := splitTopLevelArguments(matches[2])
	if _, ok := dbtSimpleScalarFunctions[functionName]; ok && len(arguments) == 1 && isSupportedRowLevelExpression(arguments[0]) {
		return map[string]string{"transform_kind": functionName, "transform_expression": expression}
	}
	if functionName == "coalesce" && len(arguments) >= 2 && supportsRowLevelArguments(arguments) {
		return map[string]string{"transform_kind": "coalesce", "transform_expression": expression}
	}
	if _, ok := dbtLiteralParameterScalarFuncs[functionName]; ok && len(arguments) >= 2 {
		referenceArguments := 0
		valid := true
		for _, argument := range arguments {
			if isSimpleReferenceExpression(argument) {
				referenceArguments++
				continue
			}
			if !isLiteralExpression(argument) {
				valid = false
			}
		}
		if valid && referenceArguments == 1 {
			return map[string]string{"transform_kind": functionName, "transform_expression": expression}
		}
	}
	if _, ok := dbtMultiInputRowLevelFunctions[functionName]; ok && len(arguments) >= 2 && supportsRowLevelArguments(arguments) {
		return map[string]string{"transform_kind": functionName, "transform_expression": expression}
	}
	if functionName == "concat_ws" && len(arguments) >= 3 && isLiteralExpression(arguments[0]) && supportsRowLevelArguments(arguments[1:]) {
		return map[string]string{"transform_kind": functionName, "transform_expression": expression}
	}
	return nil
}

func supportsRowLevelArguments(arguments []string) bool {
	referenceCount := 0
	for _, argument := range arguments {
		if isSupportedRowLevelExpression(argument) {
			referenceCount++
			continue
		}
		return false
	}
	return referenceCount >= 1
}

func simpleReferenceTokens(expression string) []string {
	matchedIdentifiers := make(map[string]struct{})
	tokens := make([]string, 0)
	seen := make(map[string]struct{})
	for _, match := range qualifiedReferenceMatches(expression) {
		token := match.Alias + "." + match.Column
		matchedIdentifiers[match.Alias] = struct{}{}
		matchedIdentifiers[match.Column] = struct{}{}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	for _, identifier := range unqualifiedIdentifiers(expression, matchedIdentifiers) {
		if _, ok := seen[identifier]; ok {
			continue
		}
		seen[identifier] = struct{}{}
		tokens = append(tokens, identifier)
	}
	return tokens
}

func hasUnsupportedFunctionCall(expression string) bool {
	for _, match := range dbtFunctionCallScanRe.FindAllStringSubmatch(expression, -1) {
		switch strings.ToLower(strings.TrimSpace(match[1])) {
		case "and", "case", "else", "end", "in", "is", "not", "or", "then", "when":
			continue
		default:
			return true
		}
	}
	return false
}

func collapsedShape(expression string, references []string, stripCaseKeywords bool) string {
	sanitized := dbtSingleQuotedLiteralScan.ReplaceAllString(expression, "0")
	sanitized = dbtNumericLiteralScan.ReplaceAllString(sanitized, "${1}0${3}")
	sanitized = replaceReferenceTokens(sanitized, references)
	if stripCaseKeywords {
		sanitized = dbtCaseKeywordRe.ReplaceAllString(sanitized, " ")
	}
	return strings.ReplaceAll(strings.ReplaceAll(sanitized, "REF", ""), "0", "")
}

func replaceReferenceTokens(expression string, references []string) string {
	sanitized := expression
	for _, token := range references {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(token) + `\b`)
		sanitized = re.ReplaceAllString(sanitized, "REF")
	}
	return sanitized
}

func isSimpleReferenceExpression(expression string) bool {
	normalized := stripWrappingParentheses(strings.TrimSpace(expression))
	return normalized != "" && (dbtBareIdentifierRe.MatchString(normalized) || dbtQualifiedReferenceRe.MatchString(normalized))
}

func isLiteralExpression(expression string) bool {
	normalized := stripWrappingParentheses(strings.TrimSpace(expression))
	if normalized == "" {
		return false
	}
	switch strings.ToLower(normalized) {
	case "null", "true", "false":
		return true
	}
	return dbtSingleQuotedLiteralRe.MatchString(normalized) || dbtNumericLiteralRe.MatchString(normalized)
}

func splitTopLevelArguments(arguments string) []string {
	items := make([]string, 0)
	current := make([]rune, 0, len(arguments))
	depth := 0
	inSingleQuote := false
	var prev rune
	for _, character := range arguments {
		if character == '\'' && prev != '\\' {
			inSingleQuote = !inSingleQuote
		} else if !inSingleQuote {
			switch character {
			case '(':
				depth++
			case ')':
				if depth > 0 {
					depth--
				}
			case ',':
				if depth == 0 {
					item := strings.TrimSpace(string(current))
					if item != "" {
						items = append(items, item)
					}
					current = current[:0]
					prev = character
					continue
				}
			}
		}
		current = append(current, character)
		prev = character
	}
	if tail := strings.TrimSpace(string(current)); tail != "" {
		items = append(items, tail)
	}
	return items
}

type qualifiedReferenceMatch struct {
	Alias  string
	Column string
}

func qualifiedReferenceMatches(expression string) []qualifiedReferenceMatch {
	indexes := dbtQualifiedReferenceScanRe.FindAllStringSubmatchIndex(expression, -1)
	matches := make([]qualifiedReferenceMatch, 0, len(indexes))
	for _, indexSet := range indexes {
		if len(indexSet) < 6 {
			continue
		}
		if next := nextNonSpaceCharacter(expression, indexSet[1]); next == "(" {
			continue
		}
		matches = append(matches, qualifiedReferenceMatch{
			Alias:  expression[indexSet[2]:indexSet[3]],
			Column: expression[indexSet[4]:indexSet[5]],
		})
	}
	return matches
}

func splitCastArguments(arguments string) (string, string, bool) {
	depth := 0
	inSingleQuote := false
	lowerArguments := strings.ToLower(arguments)
	for index, character := range arguments {
		if character == '\'' && (index == 0 || arguments[index-1] != '\\') {
			inSingleQuote = !inSingleQuote
			continue
		}
		if inSingleQuote {
			continue
		}
		switch character {
		case '(':
			depth++
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth != 0 || index+4 > len(arguments) || lowerArguments[index:index+4] != " as " {
			continue
		}
		return strings.TrimSpace(arguments[:index]), strings.TrimSpace(arguments[index+4:]), true
	}
	return "", "", false
}
