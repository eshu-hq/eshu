package dbtsql

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
