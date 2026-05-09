package kotlin

import "strings"

func kotlinInferReceiverType(
	receiver string,
	variableTypes map[string]string,
	classPropertyTypes map[string]map[string]string,
	currentClass string,
	packageName string,
	functionReturnTypes map[string]string,
	classTypeParameters map[string][]string,
) string {
	receiver = strings.TrimSpace(receiver)
	if receiver == "" {
		return ""
	}
	receiver = kotlinNormalizeParenthesizedReceivers(receiver)
	receiver = strings.TrimPrefix(receiver, "this.")
	parts := strings.Split(receiver, ".")
	if len(parts) == 0 {
		return ""
	}

	currentType := ""
	root := strings.TrimSpace(parts[0])
	currentType = kotlinInferReceiverSegmentType(
		root,
		variableTypes,
		classPropertyTypes,
		currentClass,
		packageName,
		functionReturnTypes,
		classTypeParameters,
	)
	if currentType == "" {
		return ""
	}
	if len(parts) == 1 {
		return currentType
	}

	for _, segment := range parts[1:] {
		name := strings.TrimSpace(segment)
		if name == "" {
			return ""
		}
		if strings.Contains(name, "(") && strings.HasSuffix(name, ")") {
			currentType = kotlinInferReceiverMethodReturnType(
				currentType,
				strings.TrimSuffix(name, "()"),
				kotlinBaseTypeName(currentType),
				packageName,
				functionReturnTypes,
				classTypeParameters,
			)
			if currentType == "" {
				return ""
			}
			continue
		}
		properties := classPropertyTypes[kotlinBaseTypeName(currentType)]
		if len(properties) == 0 {
			return ""
		}
		nextType := strings.TrimSpace(properties[name])
		if nextType == "" {
			return ""
		}
		currentType = kotlinResolveTypeReference(nextType, currentType, classTypeParameters)
	}
	return currentType
}

func kotlinInferFunctionCallReturnType(
	callExpression string,
	variableTypes map[string]string,
	classPropertyTypes map[string]map[string]string,
	currentClass string,
	packageName string,
	functionReturnTypes map[string]string,
	classTypeParameters map[string][]string,
) string {
	callExpression = strings.TrimSpace(callExpression)
	if callExpression == "" {
		return ""
	}

	if strings.Contains(callExpression, "(") && strings.HasSuffix(callExpression, ")") {
		return kotlinInferMethodCallReturnType(
			callExpression,
			variableTypes,
			classPropertyTypes,
			currentClass,
			packageName,
			functionReturnTypes,
			classTypeParameters,
		)
	}

	return kotlinInferMethodCallReturnType(
		callExpression+"()",
		variableTypes,
		classPropertyTypes,
		currentClass,
		packageName,
		functionReturnTypes,
		classTypeParameters,
	)
}

func kotlinInferMethodCallReturnType(
	callExpression string,
	variableTypes map[string]string,
	classPropertyTypes map[string]map[string]string,
	currentClass string,
	packageName string,
	functionReturnTypes map[string]string,
	classTypeParameters map[string][]string,
) string {
	callExpression = strings.TrimSpace(callExpression)
	if callExpression == "" {
		return ""
	}
	callExpression = kotlinNormalizeParenthesizedReceivers(callExpression)
	for {
		trimmedCallExpression := kotlinStripWrappingParentheses(callExpression)
		if trimmedCallExpression == callExpression {
			break
		}
		callExpression = trimmedCallExpression
	}

	callHead := callExpression
	if idx := strings.LastIndex(callExpression, "("); idx >= 0 && strings.HasSuffix(callExpression, ")") {
		callHead = strings.TrimSpace(callExpression[:idx])
	}
	if callHead == "" {
		return ""
	}

	receiver := ""
	name := callHead
	if idx := strings.LastIndex(callHead, "."); idx >= 0 {
		receiver = strings.TrimSpace(callHead[:idx])
		name = strings.TrimSpace(callHead[idx+1:])
	}
	if name == "" {
		return ""
	}

	if receiver == "" {
		if kotlinLooksLikeTypeName(name) {
			return kotlinCanonicalTypeReference(name)
		}
		return kotlinLookupFunctionReturnType(functionReturnTypes, packageName, currentClass, name)
	}

	inferredReceiverType := kotlinInferReceiverType(
		receiver,
		variableTypes,
		classPropertyTypes,
		currentClass,
		packageName,
		functionReturnTypes,
		classTypeParameters,
	)
	if inferredReceiverType == "" {
		return ""
	}
	return kotlinResolveTypeReference(
		kotlinLookupFunctionReturnType(functionReturnTypes, packageName, kotlinBaseTypeName(inferredReceiverType), name),
		inferredReceiverType,
		classTypeParameters,
	)
}

func kotlinInferReceiverSegmentType(
	segment string,
	variableTypes map[string]string,
	classPropertyTypes map[string]map[string]string,
	currentClass string,
	packageName string,
	functionReturnTypes map[string]string,
	classTypeParameters map[string][]string,
) string {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return ""
	}
	segment = kotlinNormalizeParenthesizedReceivers(segment)
	segment = kotlinStripWrappingParentheses(segment)

	if strings.Contains(segment, "(") && strings.HasSuffix(segment, ")") {
		return kotlinInferMethodCallReturnType(
			segment,
			variableTypes,
			classPropertyTypes,
			currentClass,
			packageName,
			functionReturnTypes,
			classTypeParameters,
		)
	}

	if inferredType := strings.TrimSpace(variableTypes[segment]); inferredType != "" {
		return kotlinCanonicalTypeReference(inferredType)
	}
	if currentClass != "" {
		if inferredType := strings.TrimSpace(classPropertyTypes[currentClass][segment]); inferredType != "" {
			return kotlinCanonicalTypeReference(inferredType)
		}
	}
	if kotlinLooksLikeTypeName(segment) {
		return kotlinCanonicalTypeReference(segment)
	}
	return ""
}

func kotlinInferReceiverMethodReturnType(
	receiverType string,
	methodName string,
	currentClass string,
	packageName string,
	functionReturnTypes map[string]string,
	classTypeParameters map[string][]string,
) string {
	receiverType = kotlinCanonicalTypeReference(receiverType)
	currentClass = strings.TrimSpace(currentClass)
	methodName = strings.TrimSpace(methodName)
	if receiverType == "" || methodName == "" {
		return ""
	}

	returnType := kotlinLookupFunctionReturnType(functionReturnTypes, packageName, currentClass, methodName)
	if returnType == "" {
		return ""
	}
	return kotlinResolveTypeReference(returnType, receiverType, classTypeParameters)
}
