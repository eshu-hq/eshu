package parser

import (
	"regexp"
	"strings"
)

var (
	phpAssignmentPattern        = regexp.MustCompile(`^\s*(\$[A-Za-z_]\w*)\s*=\s*([^;]+)`)
	phpReferenceVariablePattern = regexp.MustCompile(`^\$([A-Za-z_]\w*)`)
	phpStaticPropertyPattern    = regexp.MustCompile(`^([A-Za-z_]\w*(?:\\[A-Za-z_]\w*)*)::\$([A-Za-z_]\w*)$`)
)

func currentPHPFunctionScopeKey(stack []phpScopedContext) string {
	contextName, contextKind, _ := currentPHPContext(stack)
	if contextName == "" {
		return ""
	}
	if contextKind != "function_definition" && contextKind != "method_declaration" {
		return ""
	}
	classContext := currentPHPScopedName(stack, "class_declaration", "interface_declaration", "trait_declaration")
	return classContext + "::" + contextName
}

func inferPHPVariableType(
	rawLine string,
	variable string,
	lineNumber int,
	classContext string,
	classParentTypes map[string]string,
	classPropertyTypes map[string]map[string]string,
	localVariableTypes map[string]string,
	methodReturnTypes map[string]map[string]string,
	functionReturnTypes map[string]string,
	importAliases map[string]string,
) string {
	if matches := phpTypedVariablePattern.FindStringSubmatch(rawLine); len(matches) == 2 && strings.Contains(rawLine, variable) {
		return normalizePHPImportedTypeName(matches[1], importAliases)
	}
	if matches := phpVariableTypePattern.FindStringSubmatch(rawLine); len(matches) == 2 {
		return normalizePHPImportedTypeName(matches[1], importAliases)
	}
	if matches := phpAssignmentPattern.FindStringSubmatch(rawLine); len(matches) == 3 && matches[1] == variable {
		if strings.Contains(matches[2], "new class") {
			return phpAnonymousClassName(lineNumber)
		}
		if inferred := inferPHPReferenceType(matches[2], classContext, classParentTypes, classPropertyTypes, localVariableTypes, methodReturnTypes, functionReturnTypes, importAliases); inferred != "" {
			return inferred
		}
		if inferred := inferPHPFunctionCallType(matches[2], functionReturnTypes, importAliases); inferred != "" {
			return inferred
		}
	}
	return "mixed"
}

func inferPHPMethodReceiverType(
	raw string,
	classContext string,
	classParentTypes map[string]string,
	classPropertyTypes map[string]map[string]string,
	localVariableTypes map[string]string,
	methodReturnTypes map[string]map[string]string,
	functionReturnTypes map[string]string,
	importAliases map[string]string,
) string {
	trimmed := normalizePHPParenthesizedReceiverExpression(raw)
	if trimmed == "" {
		return ""
	}
	if index := strings.LastIndex(trimmed, "->"); index >= 0 {
		trimmed = trimmed[:index]
	}
	return inferPHPReferenceType(trimmed, classContext, classParentTypes, classPropertyTypes, localVariableTypes, methodReturnTypes, functionReturnTypes, importAliases)
}

func inferPHPReferenceType(
	raw string,
	classContext string,
	classParentTypes map[string]string,
	classPropertyTypes map[string]map[string]string,
	localVariableTypes map[string]string,
	methodReturnTypes map[string]map[string]string,
	functionReturnTypes map[string]string,
	importAliases map[string]string,
) string {
	trimmed := normalizePHPParenthesizedReceiverExpression(raw)
	if trimmed == "" {
		return ""
	}

	if matches := phpNewCallPattern.FindStringSubmatch(trimmed); len(matches) == 2 && !strings.Contains(trimmed, "->") && !strings.Contains(trimmed, "::") {
		normalized := normalizePHPImportedTypeName(matches[1], importAliases)
		switch normalized {
		case "self", "static":
			return classContext
		case "parent":
			return resolvePHPParentClassType(classContext, classParentTypes)
		default:
			return normalized
		}
	}

	if inferred := inferPHPFunctionCallType(trimmed, functionReturnTypes, importAliases); inferred != "" {
		return inferred
	}

	if inferred := inferPHPMethodCallType(trimmed, classContext, classParentTypes, classPropertyTypes, localVariableTypes, methodReturnTypes, functionReturnTypes, importAliases); inferred != "" {
		return inferred
	}

	if inferred := inferPHPCallChainType(trimmed, classContext, classParentTypes, classPropertyTypes, localVariableTypes, methodReturnTypes, functionReturnTypes, importAliases); inferred != "" {
		return inferred
	}
	if inferred := resolvePHPStaticPropertyRootType(trimmed, classContext, classParentTypes, classPropertyTypes, importAliases); inferred != "" {
		return inferred
	}

	segments := strings.Split(trimmed, "->")
	if len(segments) == 0 {
		return ""
	}

	root := strings.TrimSpace(segments[0])
	switch {
	case root == "$this":
		if classContext == "" {
			return ""
		}
		return resolvePHPReferenceChainType(classContext, segments[1:], classPropertyTypes, methodReturnTypes, functionReturnTypes, importAliases)
	case strings.HasPrefix(root, "$"):
		if matches := phpReferenceVariablePattern.FindStringSubmatch(root); len(matches) == 2 {
			if localVariableTypes == nil {
				return ""
			}
			rootType := strings.TrimSpace(localVariableTypes[matches[1]])
			if rootType == "" {
				return ""
			}
			return resolvePHPReferenceChainType(rootType, segments[1:], classPropertyTypes, methodReturnTypes, functionReturnTypes, importAliases)
		}
	}

	return ""
}

func inferPHPCallChainType(
	raw string,
	classContext string,
	classParentTypes map[string]string,
	classPropertyTypes map[string]map[string]string,
	localVariableTypes map[string]string,
	methodReturnTypes map[string]map[string]string,
	functionReturnTypes map[string]string,
	importAliases map[string]string,
) string {
	trimmed := normalizePHPParenthesizedReceiverExpression(raw)
	if trimmed == "" {
		return ""
	}

	segments := strings.Split(trimmed, "->")
	if len(segments) < 2 {
		return ""
	}

	root := strings.TrimSpace(segments[0])
	if root == "" {
		return ""
	}

	remainder := segments[1:]
	if inferred := inferPHPFunctionCallType(root, functionReturnTypes, importAliases); inferred != "" {
		return resolvePHPReferenceChainType(inferred, remainder, classPropertyTypes, methodReturnTypes, functionReturnTypes, importAliases)
	}
	if inferred := inferPHPMethodCallType(root, classContext, classParentTypes, classPropertyTypes, localVariableTypes, methodReturnTypes, functionReturnTypes, importAliases); inferred != "" {
		return resolvePHPReferenceChainType(inferred, remainder, classPropertyTypes, methodReturnTypes, functionReturnTypes, importAliases)
	}
	if inferred := resolvePHPStaticPropertyRootType(root, classContext, classParentTypes, classPropertyTypes, importAliases); inferred != "" {
		return resolvePHPReferenceChainType(inferred, remainder, classPropertyTypes, methodReturnTypes, functionReturnTypes, importAliases)
	}

	return ""
}

func inferPHPMethodCallType(
	raw string,
	classContext string,
	classParentTypes map[string]string,
	classPropertyTypes map[string]map[string]string,
	localVariableTypes map[string]string,
	methodReturnTypes map[string]map[string]string,
	functionReturnTypes map[string]string,
	importAliases map[string]string,
) string {
	if len(methodReturnTypes) == 0 {
		return ""
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !strings.HasSuffix(trimmed, ")") {
		return ""
	}

	openIndex := strings.LastIndex(trimmed, "(")
	if openIndex < 0 {
		return ""
	}
	callHead := strings.TrimSpace(trimmed[:openIndex])
	if callHead == "" {
		return ""
	}

	separatorIndex := strings.LastIndex(callHead, "->")
	separator := "->"
	if staticIndex := strings.LastIndex(callHead, "::"); staticIndex > separatorIndex {
		separatorIndex = staticIndex
		separator = "::"
	}
	if separatorIndex < 0 {
		return ""
	}

	receiverExpr := strings.TrimSpace(callHead[:separatorIndex])
	methodName := strings.TrimSpace(callHead[separatorIndex+len(separator):])
	if receiverExpr == "" || methodName == "" {
		return ""
	}

	receiverType := resolvePHPReferenceRootType(receiverExpr, classContext, classParentTypes, classPropertyTypes, localVariableTypes, methodReturnTypes, functionReturnTypes, importAliases)
	if receiverType == "" {
		return ""
	}

	return lookupPHPMethodReturnType(receiverType, methodName, methodReturnTypes, importAliases)
}

func resolvePHPReferenceRootType(
	raw string,
	classContext string,
	classParentTypes map[string]string,
	classPropertyTypes map[string]map[string]string,
	localVariableTypes map[string]string,
	methodReturnTypes map[string]map[string]string,
	functionReturnTypes map[string]string,
	importAliases map[string]string,
) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if trimmed == "self" || trimmed == "static" {
		return classContext
	}
	if trimmed == "parent" {
		return resolvePHPParentClassType(classContext, classParentTypes)
	}

	if inferred := inferPHPReferenceType(trimmed, classContext, classParentTypes, classPropertyTypes, localVariableTypes, methodReturnTypes, functionReturnTypes, importAliases); inferred != "" {
		return inferred
	}

	if strings.HasPrefix(trimmed, "$") {
		return ""
	}

	return normalizePHPImportedTypeName(trimmed, importAliases)
}

func resolvePHPParentClassType(classContext string, classParentTypes map[string]string) string {
	if classContext == "" || len(classParentTypes) == 0 {
		return ""
	}
	return strings.TrimSpace(classParentTypes[classContext])
}

func resolvePHPStaticPropertyRootType(
	raw string,
	classContext string,
	classParentTypes map[string]string,
	classPropertyTypes map[string]map[string]string,
	importAliases map[string]string,
) string {
	matches := phpStaticPropertyPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if len(matches) != 3 {
		return ""
	}

	ownerType := normalizePHPStaticReceiver(matches[1], classContext, classParentTypes, importAliases)
	if ownerType == "" {
		return ""
	}

	propertyType := lookupPHPClassPropertyType(ownerType, matches[2], classParentTypes, classPropertyTypes)
	if propertyType == "" {
		return ""
	}
	return normalizePHPImportedTypeName(propertyType, importAliases)
}

func lookupPHPClassPropertyType(
	className string,
	propertyName string,
	classParentTypes map[string]string,
	classPropertyTypes map[string]map[string]string,
) string {
	trimmedClassName := strings.TrimSpace(className)
	trimmedPropertyName := strings.TrimSpace(propertyName)
	if trimmedClassName == "" || trimmedPropertyName == "" {
		return ""
	}

	seen := make(map[string]struct{})
	current := trimmedClassName
	for current != "" {
		if _, ok := seen[current]; ok {
			return ""
		}
		seen[current] = struct{}{}

		if propertyType := strings.TrimSpace(classPropertyTypes[current][trimmedPropertyName]); propertyType != "" {
			return propertyType
		}

		current = strings.TrimSpace(classParentTypes[current])
	}

	return ""
}

func inferPHPFunctionCallType(
	raw string,
	functionReturnTypes map[string]string,
	importAliases map[string]string,
) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !strings.HasSuffix(trimmed, ")") {
		return ""
	}

	openIndex := strings.LastIndex(trimmed, "(")
	if openIndex < 0 {
		return ""
	}

	callName := strings.TrimSpace(trimmed[:openIndex])
	if callName == "" || strings.Contains(callName, "->") || strings.Contains(callName, "::") {
		return ""
	}

	return lookupPHPFunctionReturnType(callName, functionReturnTypes, importAliases)
}

func lookupPHPFunctionReturnType(
	functionName string,
	functionReturnTypes map[string]string,
	importAliases map[string]string,
) string {
	if functionName == "" {
		return ""
	}

	normalizedFunctionName := normalizePHPImportedTypeName(functionName, importAliases)
	if normalizedFunctionName == "" {
		return ""
	}

	for _, candidateFunctionName := range []string{functionName, normalizedFunctionName} {
		if returns := normalizePHPImportedTypeName(functionReturnTypes[strings.TrimSpace(candidateFunctionName)], importAliases); returns != "" {
			switch returns {
			case "self", "static":
				return normalizedFunctionName
			case "parent":
				return ""
			default:
				return returns
			}
		}
	}

	return ""
}

func lookupPHPMethodReturnType(
	className string,
	methodName string,
	methodReturnTypes map[string]map[string]string,
	importAliases map[string]string,
) string {
	if className == "" || methodName == "" {
		return ""
	}

	normalizedClassName := normalizePHPImportedTypeName(className, importAliases)
	if normalizedClassName == "" {
		return ""
	}

	for _, candidateClassName := range []string{className, normalizedClassName} {
		if strings.TrimSpace(candidateClassName) == "" {
			continue
		}
		if returns := normalizePHPImportedTypeName(methodReturnTypes[strings.TrimSpace(candidateClassName)][methodName], importAliases); returns != "" {
			switch returns {
			case "self", "static":
				return normalizedClassName
			case "parent":
				return ""
			default:
				return returns
			}
		}
	}

	return ""
}

func extractPHPReturnType(lines []string, startIndex int, rawLine string) string {
	signature := collectPHPFunctionSignature(lines, startIndex, rawLine)
	matches := phpFunctionReturnPattern.FindStringSubmatch(signature)
	if len(matches) != 2 {
		return ""
	}
	return normalizePHPTypeName(matches[1])
}

func collectPHPFunctionSignature(lines []string, startIndex int, rawLine string) string {
	signature := strings.TrimSpace(rawLine)
	if signature == "" {
		return ""
	}
	if strings.Contains(signature, "{") || strings.Contains(signature, ";") {
		return signature
	}

	for index := startIndex + 1; index < len(lines); index++ {
		nextLine := strings.TrimSpace(lines[index])
		if nextLine == "" {
			continue
		}
		signature += " " + nextLine
		if strings.Contains(nextLine, "{") || strings.Contains(nextLine, ";") {
			break
		}
	}

	return signature
}

func resolvePHPReferenceChainType(
	rootType string,
	segments []string,
	classPropertyTypes map[string]map[string]string,
	methodReturnTypes map[string]map[string]string,
	functionReturnTypes map[string]string,
	importAliases map[string]string,
) string {
	currentType := strings.TrimSpace(rootType)
	if currentType == "" {
		return ""
	}
	for _, segment := range segments {
		segmentName := strings.TrimSpace(segment)
		if segmentName == "" {
			return ""
		}
		if strings.HasSuffix(segmentName, ")") {
			openIndex := strings.Index(segmentName, "(")
			if openIndex < 0 {
				return ""
			}
			methodName := strings.TrimSpace(segmentName[:openIndex])
			if methodName == "" {
				return ""
			}
			nextType := lookupPHPMethodReturnType(currentType, methodName, methodReturnTypes, importAliases)
			if nextType == "" {
				return ""
			}
			currentType = nextType
			continue
		}
		nextType := normalizePHPImportedTypeName(classPropertyTypes[currentType][segmentName], importAliases)
		if nextType == "" {
			return ""
		}
		currentType = nextType
	}
	return currentType
}

func normalizePHPImportedTypeName(raw string, importAliases map[string]string) string {
	normalized := normalizePHPTypeName(raw)
	if normalized == "" || len(importAliases) == 0 {
		return normalized
	}
	resolved := strings.TrimSpace(importAliases[normalized])
	if resolved == "" || resolved == normalized {
		return normalized
	}
	return normalizePHPImportedTypeName(resolved, importAliases)
}

func normalizePHPParenthesizedReceiverExpression(raw string) string {
	trimmed := strings.TrimSpace(raw)
	for trimmed != "" && strings.HasPrefix(trimmed, "(") {
		closeIndex := findPHPMatchingParen(trimmed)
		if closeIndex < 0 {
			return trimmed
		}
		remainder := strings.TrimSpace(trimmed[closeIndex+1:])
		if remainder != "" && !strings.HasPrefix(remainder, "->") && !strings.HasPrefix(remainder, "::") {
			return trimmed
		}
		trimmed = strings.TrimSpace(trimmed[1:closeIndex]) + remainder
	}
	return trimmed
}

func findPHPMatchingParen(raw string) int {
	depth := 0
	for index, r := range raw {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index
			}
			if depth < 0 {
				return -1
			}
		}
	}
	return -1
}
