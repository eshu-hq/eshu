package kotlin

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var kotlinPrimaryConstructorPropertyPattern = regexp.MustCompile(
	`(?m)(?:^|,)\s*(?:(?:@[A-Za-z_]\w*(?:\([^)]*\))?\s+)*)*(?:(?:private|public|protected|internal|override|open|final|const|lateinit)\s+)*(?:val|var)\s+([A-Za-z_]\w*)\s*:\s*([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*(?:<[^>]+>)?\??)`,
)

var (
	kotlinFunctionReturnPattern = regexp.MustCompile(
		`\bfun\s+(?:<[^>]+>\s*)?(?:([A-Za-z_]\w*)\.)?([A-Za-z_]\w*)\s*\([^)]*\)\s*:\s*([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*(?:<[^>]+>)?\??)`,
	)
	kotlinSuspendFunctionPattern    = regexp.MustCompile(`\bsuspend\s+fun\b`)
	kotlinFunctionCallAssignPattern = regexp.MustCompile(
		`^\s*(?:val|var)\s+([A-Za-z_]\w*)\s*=\s*((?:[A-Za-z_]\w*\.)*[A-Za-z_]\w*)\s*\([^()]*\)\s*$`,
	)
	kotlinCastAssignPattern = regexp.MustCompile(
		`^\s*(?:val|var)\s+([A-Za-z_]\w*)\s*=\s*.+?\s+as\??\s+([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*(?:<[^>]+>)?\??)\s*$`,
	)
	kotlinLazyDelegatedAssignPattern = regexp.MustCompile(
		`^\s*(?:val|var)\s+([A-Za-z_]\w*)\s+by\s+lazy(?:\s*\([^)]*\))?\s*\{\s*(.+?)\s*\}\s*$`,
	)
	kotlinConstructorCallPattern       = regexp.MustCompile(`\b([A-Z][A-Za-z_]\w*)\s*\(`)
	kotlinParenthesizedReceiverPattern = regexp.MustCompile(
		`\(([A-Za-z_]\w*(?:\([^()]*\))?(?:\.[A-Za-z_]\w*(?:\([^()]*\))?)*?)\)\.`,
	)
)

func kotlinPrimaryConstructorPropertyTypes(line string) map[string]string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}

	openIndex := strings.Index(trimmed, "(")
	if openIndex < 0 {
		return nil
	}
	closeIndex := kotlinMatchingParenIndex(trimmed, openIndex)
	if closeIndex <= openIndex {
		return nil
	}

	constructor := trimmed[openIndex+1 : closeIndex]
	matches := kotlinPrimaryConstructorPropertyPattern.FindAllStringSubmatch(constructor, -1)
	if len(matches) == 0 {
		return nil
	}

	properties := make(map[string]string, len(matches))
	for _, match := range matches {
		if len(match) != 3 {
			continue
		}
		name := strings.TrimSpace(match[1])
		typ := kotlinCanonicalTypeReference(match[2])
		if name == "" || typ == "" {
			continue
		}
		properties[name] = typ
	}
	if len(properties) == 0 {
		return nil
	}
	return properties
}

func kotlinFunctionDeclarationReturnType(line string) (string, string, string) {
	matches := kotlinFunctionReturnPattern.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) != 4 {
		return "", "", ""
	}
	return strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2]), kotlinCanonicalTypeReference(matches[3])
}

func kotlinFunctionIsSuspend(line string) bool {
	return kotlinSuspendFunctionPattern.MatchString(strings.TrimSpace(line))
}

func kotlinCurrentTypeScopeName(stack []scopedContext) string {
	return currentScopedName(stack, "class", "interface")
}

func kotlinTypedDeclarationType(line string) string {
	matches := kotlinTypedVariablePattern.FindStringSubmatch(line)
	if len(matches) != 3 {
		return ""
	}
	return kotlinCanonicalTypeReference(matches[2])
}

func kotlinInferAssignedVariableType(
	trimmed string,
	name string,
	functionContext string,
	classContext string,
	packageName string,
	classTypeParameters map[string][]string,
	localVariableTypes map[string]map[string]string,
	classPropertyTypes map[string]map[string]string,
	functionReturnTypes map[string]string,
) string {
	trimmed = strings.ReplaceAll(trimmed, "?.", ".")
	trimmed = kotlinStripReceiverPreservingScopeFunctions(trimmed)
	switch {
	case kotlinCtorAssignPattern.MatchString(trimmed):
		assignMatches := kotlinCtorAssignPattern.FindStringSubmatch(trimmed)
		if len(assignMatches) == 3 && assignMatches[1] == name {
			assignedType := strings.TrimSpace(assignMatches[2])
			if assignedType == "" {
				return ""
			}
			if kotlinLooksLikeTypeName(assignedType) {
				return kotlinCanonicalTypeReference(assignedType)
			}
			return kotlinInferFunctionCallReturnType(
				assignedType,
				localVariableTypes[functionContext],
				classPropertyTypes,
				classContext,
				packageName,
				functionReturnTypes,
				classTypeParameters,
			)
		}
	case kotlinFunctionCallAssignPattern.MatchString(trimmed):
		assignMatches := kotlinFunctionCallAssignPattern.FindStringSubmatch(trimmed)
		if len(assignMatches) == 3 && assignMatches[1] == name {
			return kotlinInferFunctionCallReturnType(
				assignMatches[2],
				localVariableTypes[functionContext],
				classPropertyTypes,
				classContext,
				packageName,
				functionReturnTypes,
				classTypeParameters,
			)
		}
	case kotlinCastAssignPattern.MatchString(trimmed):
		assignMatches := kotlinCastAssignPattern.FindStringSubmatch(trimmed)
		if len(assignMatches) == 3 && assignMatches[1] == name {
			return kotlinCanonicalTypeReference(assignMatches[2])
		}
	case kotlinStringAssignPattern.MatchString(trimmed):
		assignMatches := kotlinStringAssignPattern.FindStringSubmatch(trimmed)
		if len(assignMatches) == 3 && assignMatches[1] == name {
			return "String"
		}
	case kotlinAliasAssignPattern.MatchString(trimmed):
		assignMatches := kotlinAliasAssignPattern.FindStringSubmatch(trimmed)
		if len(assignMatches) == 3 && assignMatches[1] == name {
			return kotlinInferReceiverType(
				assignMatches[2],
				localVariableTypes[functionContext],
				classPropertyTypes,
				classContext,
				packageName,
				functionReturnTypes,
				classTypeParameters,
			)
		}
	case kotlinLazyDelegatedAssignPattern.MatchString(trimmed):
		assignMatches := kotlinLazyDelegatedAssignPattern.FindStringSubmatch(trimmed)
		if len(assignMatches) == 3 && assignMatches[1] == name {
			return kotlinInferReceiverType(
				assignMatches[2],
				localVariableTypes[functionContext],
				classPropertyTypes,
				classContext,
				packageName,
				functionReturnTypes,
				classTypeParameters,
			)
		}
	}
	return ""
}

func kotlinInferAssignedVariableCallKind(trimmed string, name string) string {
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" || name == "" {
		return ""
	}

	assignMatches := kotlinLazyDelegatedAssignPattern.FindStringSubmatch(trimmed)
	if len(assignMatches) == 3 && assignMatches[1] == name {
		return "kotlin_lazy_delegated_property_receiver"
	}

	return ""
}

func kotlinInferReceiverCallKind(receiver string, variableCallKinds map[string]string) string {
	receiver = strings.TrimSpace(receiver)
	if receiver == "" || len(variableCallKinds) == 0 {
		return ""
	}

	receiver = kotlinNormalizeParenthesizedReceivers(receiver)
	receiver = strings.TrimPrefix(receiver, "this.")
	segments := strings.Split(receiver, ".")
	if len(segments) == 0 {
		return ""
	}

	return strings.TrimSpace(variableCallKinds[strings.TrimSpace(segments[0])])
}

func kotlinMatchingParenIndex(value string, openIndex int) int {
	if openIndex < 0 || openIndex >= len(value) {
		return -1
	}

	depth := 0
	for index, char := range value[openIndex:] {
		switch char {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return openIndex + index
			}
		}
	}

	return -1
}

func kotlinLooksLikeTypeName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	first := rune(name[0])
	return first >= 'A' && first <= 'Z'
}

func kotlinNormalizeParenthesizedReceivers(value string) string {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return ""
	}

	for {
		next := kotlinParenthesizedReceiverPattern.ReplaceAllString(normalized, "$1.")
		if next == normalized {
			return normalized
		}
		normalized = next
	}
}

func kotlinStripWrappingParentheses(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 || trimmed[0] != '(' || trimmed[len(trimmed)-1] != ')' {
		return trimmed
	}

	depth := 0
	for index, char := range trimmed {
		switch char {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && index != len(trimmed)-1 {
				return trimmed
			}
		}
	}
	if depth != 0 {
		return trimmed
	}

	return strings.TrimSpace(trimmed[1 : len(trimmed)-1])
}

func kotlinImportAlias(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "."); idx >= 0 {
		return strings.TrimSpace(trimmed[idx+1:])
	}
	return trimmed
}

func kotlinCallNameAllowed(name string) bool {
	switch name {
	case "fun", "if", "for", "while", "when", "return", "class", "interface":
		return false
	default:
		return true
	}
}

type kotlinChainedCall struct {
	Receiver string
	Name     string
	FullName string
}

func kotlinExpandChainedCalls(receiver string, name string, fullName string) []kotlinChainedCall {
	if receiver == "" || name == "" || fullName == "" {
		return nil
	}

	entries := []kotlinChainedCall{{
		Receiver: receiver,
		Name:     name,
		FullName: fullName,
	}}

	current := receiver
	for {
		nestedReceiver, nestedName, ok := kotlinSplitChainedCall(current)
		if !ok {
			break
		}
		nestedFullName := strings.TrimSpace(strings.TrimSuffix(current, "()"))
		entries = append([]kotlinChainedCall{{
			Receiver: nestedReceiver,
			Name:     nestedName,
			FullName: nestedFullName,
		}}, entries...)
		current = nestedReceiver
	}

	return entries
}

func kotlinSplitChainedCall(expression string) (string, string, bool) {
	normalized := kotlinStripWrappingParentheses(
		kotlinNormalizeParenthesizedReceivers(strings.TrimSpace(expression)),
	)
	if !strings.Contains(normalized, "(") || !strings.HasSuffix(normalized, ")") {
		return "", "", false
	}

	lastDot := strings.LastIndex(normalized, ".")
	if lastDot <= 0 || lastDot >= len(normalized)-1 {
		return "", "", false
	}

	receiver := strings.TrimSpace(normalized[:lastDot])
	name := strings.TrimSuffix(strings.TrimSpace(normalized[lastDot+1:]), "()")
	if receiver == "" || name == "" {
		return "", "", false
	}
	return receiver, name, true
}

func kotlinAppendConstructorCalls(
	payload map[string]any,
	trimmed string,
	lineNumber int,
	functionDeclCutoff int,
	seenLineCalls map[string]struct{},
	knownTypeNames map[string]struct{},
	skip bool,
) {
	if skip {
		return
	}

	for _, match := range kotlinConstructorCallPattern.FindAllStringSubmatchIndex(trimmed, -1) {
		if len(match) != 4 {
			continue
		}
		if functionDeclCutoff >= 0 && match[0] < functionDeclCutoff {
			continue
		}
		name := trimmed[match[2]:match[3]]
		if _, ok := knownTypeNames[name]; !ok {
			continue
		}
		callKey := name + "#" + strconv.Itoa(lineNumber)
		if _, ok := seenLineCalls[callKey]; ok {
			continue
		}
		seenLineCalls[callKey] = struct{}{}
		shared.AppendBucket(payload, "function_calls", map[string]any{
			"name":        name,
			"full_name":   name,
			"line_number": lineNumber,
			"lang":        "kotlin",
		})
	}
}

func kotlinAppendThisCalls(
	payload map[string]any,
	trimmed string,
	lineNumber int,
	seenLineCalls map[string]struct{},
	classContext string,
) {
	for _, match := range kotlinThisCallPattern.FindAllStringSubmatch(trimmed, -1) {
		if len(match) != 2 {
			continue
		}
		name := match[1]
		fullName := "this." + name
		callKey := fullName + "#" + strconv.Itoa(lineNumber)
		if _, ok := seenLineCalls[callKey]; ok {
			continue
		}
		seenLineCalls[callKey] = struct{}{}
		item := map[string]any{
			"name":        name,
			"full_name":   fullName,
			"line_number": lineNumber,
			"lang":        "kotlin",
		}
		if classContext != "" {
			item["class_context"] = classContext
		}
		shared.AppendBucket(payload, "function_calls", item)
	}
}
