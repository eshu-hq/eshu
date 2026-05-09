package dbtsql

import (
	"regexp"
	"strings"
)

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
