package golang

import (
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goCollectInterfaceReturnConcreteTypes(
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
	interfaceMethods map[string][]string,
	interfaceRootKinds map[string][]string,
	interfaceConcreteTypes map[string][]string,
	methodNamesByReceiver map[string][]string,
	exportedMethodNamesByReceiver map[string][]string,
	functionRootKinds map[string][]string,
	structRootKinds map[string][]string,
) {
	resultNode := node.ChildByFieldName("result")
	for _, interfaceName := range goReferencedLocalInterfaces(resultNode, source, interfaceMethods) {
		interfaceRootKinds[interfaceName] = appendUniqueImportAlias(interfaceRootKinds[interfaceName], "go.interface_type_reference")
		walkNamed(node, func(child *tree_sitter.Node) {
			if child.Kind() != "return_statement" {
				return
			}
			for _, concreteType := range goConcreteTypesInExpression(child, source, structTypes) {
				interfaceConcreteTypes[interfaceName] = appendUniqueImportAlias(interfaceConcreteTypes[interfaceName], concreteType)
			}
		})
	}

	importedTarget := goInterfaceTargetFromTypeNode(resultNode, source, interfaceMethods)
	if !importedTarget.imported {
		return
	}
	walkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "return_statement" {
			return
		}
		for _, concreteType := range goConcreteTypesInExpression(child, source, structTypes) {
			goMarkConcreteTypeForInterfaceTarget(
				concreteType,
				importedTarget,
				interfaceConcreteTypes,
				methodNamesByReceiver,
				exportedMethodNamesByReceiver,
				functionRootKinds,
				structRootKinds,
			)
		}
	})
}

func goMarkCompositeLiteralInterfaceFields(
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
	structFieldTargets map[string]map[string]goInterfaceTarget,
	interfaceMethods map[string][]string,
	interfaceConcreteTypes map[string][]string,
	methodNamesByReceiver map[string][]string,
	exportedMethodNamesByReceiver map[string][]string,
	functionRootKinds map[string][]string,
	structRootKinds map[string][]string,
) {
	structType := goConcreteTypeFromTypeNode(node.ChildByFieldName("type"), source, structTypes)
	if structType == "" || len(structFieldTargets[structType]) == 0 {
		return
	}
	walkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "keyed_element" {
			return
		}
		fieldName, valueNode := goKeyedElementFieldAndValue(child, source)
		if fieldName == "" || valueNode == nil {
			return
		}
		target, ok := structFieldTargets[structType][fieldName]
		if !ok {
			return
		}
		concreteType := goConcreteTypeFromExpression(valueNode, source, structTypes)
		goMarkConcreteTypeForInterfaceTarget(
			concreteType,
			target,
			interfaceConcreteTypes,
			methodNamesByReceiver,
			exportedMethodNamesByReceiver,
			functionRootKinds,
			structRootKinds,
		)
	})
}

func goMarkCallArgumentInterfaceMethods(
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
	variableTypes map[string]string,
	functionParamTargets map[string]map[int]goInterfaceTarget,
	importAliases map[string][]string,
	interfaceMethods map[string][]string,
	interfaceConcreteTypes map[string][]string,
	methodNamesByReceiver map[string][]string,
	exportedMethodNamesByReceiver map[string][]string,
	functionRootKinds map[string][]string,
	structRootKinds map[string][]string,
) {
	functionName := goCallFunctionName(node, source)
	qualifiedFunctionName := goQualifiedCallFunctionName(node, source, importAliases)
	if functionName == "" && qualifiedFunctionName == "" {
		return
	}
	for index, arg := range goCallArgumentNodes(node) {
		target, ok := functionParamTargets[qualifiedFunctionName][index]
		if !ok {
			target, ok = functionParamTargets[functionName][index]
		}
		if !ok {
			continue
		}
		concreteType := goConcreteTypeFromExpression(arg, source, structTypes)
		if concreteType == "" && arg.Kind() == "identifier" {
			concreteType = variableTypes[strings.ToLower(strings.TrimSpace(nodeText(arg, source)))]
		}
		goMarkConcreteTypeForInterfaceTarget(
			concreteType,
			target,
			interfaceConcreteTypes,
			methodNamesByReceiver,
			exportedMethodNamesByReceiver,
			functionRootKinds,
			structRootKinds,
		)
	}
}

func goMarkConcreteTypeForInterfaceTarget(
	concreteType string,
	target goInterfaceTarget,
	interfaceConcreteTypes map[string][]string,
	methodNamesByReceiver map[string][]string,
	exportedMethodNamesByReceiver map[string][]string,
	functionRootKinds map[string][]string,
	structRootKinds map[string][]string,
) {
	if concreteType == "" || !target.modeled() {
		return
	}
	if target.localInterface != "" {
		interfaceConcreteTypes[target.localInterface] = appendUniqueImportAlias(interfaceConcreteTypes[target.localInterface], concreteType)
		structRootKinds[concreteType] = appendUniqueImportAlias(structRootKinds[concreteType], "go.interface_implementation_type")
		return
	}
	if !target.imported {
		return
	}
	if len(target.importedMethods) == 0 && target.allowExportedMethods {
		for _, methodName := range exportedMethodNamesByReceiver[concreteType] {
			key := concreteType + "." + methodName
			functionRootKinds[key] = appendUniqueImportAlias(functionRootKinds[key], "go.interface_method_implementation")
		}
		structRootKinds[concreteType] = appendUniqueImportAlias(structRootKinds[concreteType], "go.interface_implementation_type")
		return
	}
	if len(target.importedMethods) == 0 {
		return
	}
	for _, methodName := range methodNamesByReceiver[concreteType] {
		if !slices.Contains(target.importedMethods, methodName) {
			continue
		}
		key := concreteType + "." + methodName
		functionRootKinds[key] = appendUniqueImportAlias(functionRootKinds[key], "go.interface_method_implementation")
	}
	structRootKinds[concreteType] = appendUniqueImportAlias(structRootKinds[concreteType], "go.interface_implementation_type")
}

func goCollectDirectMethodCallRoot(
	node *tree_sitter.Node,
	source []byte,
	methodKeys map[string]struct{},
	variableTypes map[string]string,
	structFieldTypes map[string]map[string]string,
	functionRootKinds map[string][]string,
) {
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil || functionNode.Kind() != "selector_expression" {
		return
	}
	receiver, methodName, ok := goSelectorBaseAndField(functionNode, source)
	if !ok {
		return
	}
	methodName = strings.ToLower(strings.TrimSpace(methodName))
	if methodName == "" {
		return
	}
	enclosingReceiver, enclosingType := goEnclosingMethodReceiver(node, source)
	if strings.TrimSpace(receiver) == enclosingReceiver && enclosingType != "" {
		key := strings.ToLower(enclosingType) + "." + methodName
		if _, ok := methodKeys[key]; ok {
			functionRootKinds[key] = appendUniqueImportAlias(functionRootKinds[key], "go.direct_method_call")
			return
		}
	}
	if receiverType := variableTypes[strings.ToLower(strings.TrimSpace(receiver))]; receiverType != "" {
		key := receiverType + "." + methodName
		if _, ok := methodKeys[key]; ok {
			functionRootKinds[key] = appendUniqueImportAlias(functionRootKinds[key], "go.direct_method_call")
		}
	}
	if receiverType := goFieldSelectorReceiverType(receiver, enclosingReceiver, enclosingType, variableTypes, structFieldTypes); receiverType != "" {
		key := receiverType + "." + methodName
		if _, ok := methodKeys[key]; ok {
			functionRootKinds[key] = appendUniqueImportAlias(functionRootKinds[key], "go.direct_method_call")
		}
	}
}

func goFieldSelectorReceiverType(
	receiver string,
	enclosingReceiver string,
	enclosingType string,
	variableTypes map[string]string,
	structFieldTypes map[string]map[string]string,
) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(receiver)), ".")
	if len(parts) < 2 {
		return ""
	}
	currentType := variableTypes[parts[0]]
	if currentType == "" && parts[0] == strings.ToLower(strings.TrimSpace(enclosingReceiver)) {
		currentType = strings.ToLower(strings.TrimSpace(enclosingType))
	}
	for _, field := range parts[1:] {
		if currentType == "" {
			return ""
		}
		currentType = structFieldTypes[currentType][field]
	}
	return currentType
}

func goCollectFmtStringerRoot(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	methodKeys map[string]struct{},
	variableTypes map[string]string,
	structTypes map[string]struct{},
	functionRootKinds map[string][]string,
) {
	if !goCallIsFmtFormatting(node, source, importAliases) {
		return
	}
	firstValueArg := goFmtStringerFirstValueArgIndex(node, source, importAliases)
	for index, arg := range goCallArgumentNodes(node) {
		if index < firstValueArg {
			continue
		}
		receiverType := goKnownReceiverTypeFromExpression(arg, source, variableTypes, structTypes)
		if receiverType == "" {
			continue
		}
		key := receiverType + ".string"
		if _, ok := methodKeys[key]; ok {
			functionRootKinds[key] = appendUniqueImportAlias(functionRootKinds[key], "go.fmt_stringer_method")
		}
	}
}

func goCallIsFmtFormatting(node *tree_sitter.Node, source []byte, importAliases map[string][]string) bool {
	functionName := goQualifiedCallFunctionName(node, source, importAliases)
	switch functionName {
	case "fmt.sprint", "fmt.sprintln", "fmt.sprintf", "fmt.fprint", "fmt.fprintln", "fmt.fprintf":
		return true
	default:
		return false
	}
}

func goFmtStringerFirstValueArgIndex(node *tree_sitter.Node, source []byte, importAliases map[string][]string) int {
	functionName := goQualifiedCallFunctionName(node, source, importAliases)
	switch functionName {
	case "fmt.sprint", "fmt.sprintln":
		return 0
	case "fmt.sprintf", "fmt.fprint", "fmt.fprintln":
		return 1
	case "fmt.fprintf":
		return 2
	default:
		return 0
	}
}

func goKnownReceiverTypeFromExpression(
	node *tree_sitter.Node,
	source []byte,
	variableTypes map[string]string,
	structTypes map[string]struct{},
) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier":
		return variableTypes[strings.ToLower(strings.TrimSpace(nodeText(node, source)))]
	case "composite_literal":
		return goConcreteTypeFromTypeNode(node.ChildByFieldName("type"), source, structTypes)
	case "call_expression":
		return goSelectorConversionReceiverTypeFromCall(node, source)
	case "parenthesized_expression", "unary_expression":
		return goKnownReceiverTypeFromWrappedExpression(node, source, variableTypes, structTypes)
	}
	return ""
}

func goKnownReceiverTypeFromWrappedExpression(
	node *tree_sitter.Node,
	source []byte,
	variableTypes map[string]string,
	structTypes map[string]struct{},
) string {
	var receiverType string
	walkDirectNamed(node, func(child *tree_sitter.Node) {
		if receiverType != "" {
			return
		}
		receiverType = goKnownReceiverTypeFromExpression(child, source, variableTypes, structTypes)
	})
	return receiverType
}

func goSelectorConversionReceiverTypeFromCall(node *tree_sitter.Node, source []byte) string {
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil {
		return ""
	}
	switch functionNode.Kind() {
	case "identifier", "type_identifier":
		return strings.ToLower(goNormalizeTypeName(nodeText(functionNode, source)))
	default:
		return ""
	}
}

func goCallFunctionName(node *tree_sitter.Node, source []byte) string {
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil {
		return ""
	}
	switch functionNode.Kind() {
	case "identifier":
		return strings.ToLower(strings.TrimSpace(nodeText(functionNode, source)))
	case "selector_expression":
		_, field, ok := goSelectorBaseAndField(functionNode, source)
		if ok {
			return strings.ToLower(strings.TrimSpace(field))
		}
	}
	return ""
}

func goQualifiedCallFunctionName(node *tree_sitter.Node, source []byte, importAliases map[string][]string) string {
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil || functionNode.Kind() != "selector_expression" {
		return ""
	}
	base, field, ok := goSelectorBaseAndField(functionNode, source)
	if !ok {
		return ""
	}
	base = strings.TrimSpace(base)
	field = strings.TrimSpace(field)
	if base == "" || field == "" {
		return ""
	}
	for importPath, aliases := range importAliases {
		for _, alias := range aliases {
			if alias == base {
				return strings.ToLower(importPath + "." + field)
			}
		}
	}
	return ""
}

func goCallArgumentNodes(node *tree_sitter.Node) []*tree_sitter.Node {
	args := make([]*tree_sitter.Node, 0)
	walkDirectNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "argument_list" {
			return
		}
		walkDirectNamed(child, func(arg *tree_sitter.Node) {
			args = append(args, arg)
		})
	})
	return args
}

func goKeyedElementFieldAndValue(node *tree_sitter.Node, source []byte) (string, *tree_sitter.Node) {
	keyNode := node.ChildByFieldName("key")
	valueNode := node.ChildByFieldName("value")
	if keyNode != nil && valueNode != nil {
		return strings.ToLower(strings.TrimSpace(nodeText(keyNode, source))), valueNode
	}
	children := make([]*tree_sitter.Node, 0, 2)
	walkDirectNamed(node, func(child *tree_sitter.Node) {
		children = append(children, child)
	})
	if len(children) < 2 {
		return "", nil
	}
	return strings.ToLower(strings.TrimSpace(nodeText(children[0], source))), children[1]
}
