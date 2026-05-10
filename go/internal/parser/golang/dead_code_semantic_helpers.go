package golang

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goInterfaceMethodNames(node *tree_sitter.Node, source []byte) []string {
	names := make([]string, 0)
	walkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "method_elem" {
			return
		}
		name := strings.ToLower(strings.TrimSpace(nodeText(child.ChildByFieldName("name"), source)))
		if name != "" {
			names = appendUniqueImportAlias(names, name)
		}
	})
	return names
}

func goKnownLocalVariableTypes(
	root *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
) map[string]string {
	variableTypes := make(map[string]string)
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "short_var_declaration", "assignment_statement":
			leftNames := goIdentifierNames(node.ChildByFieldName("left"), source)
			concreteType := goConcreteTypeFromExpression(goUnwrapSingleExpression(node.ChildByFieldName("right")), source, structTypes)
			if concreteType == "" {
				return
			}
			for _, name := range leftNames {
				variableTypes[name] = concreteType
			}
		case "var_spec":
			names := goIdentifierNames(node.ChildByFieldName("name"), source)
			concreteType := goConcreteTypeFromExpression(node.ChildByFieldName("value"), source, structTypes)
			if concreteType == "" {
				concreteType = goConcreteTypeFromTypeNode(node.ChildByFieldName("type"), source, structTypes)
			}
			if concreteType == "" {
				return
			}
			for _, name := range names {
				variableTypes[name] = concreteType
			}
		}
	})
	return variableTypes
}

func goCollectFunctionValuesFromExpression(
	node *tree_sitter.Node,
	source []byte,
	functionNames map[string]struct{},
	methodKeys map[string]struct{},
	variableTypes map[string]string,
	localNameBindings []goLocalNameBinding,
	functionRootKinds map[string][]string,
) {
	if node == nil {
		return
	}
	switch node.Kind() {
	case "call_expression":
		for _, arg := range goCallArgumentNodes(node) {
			if arg.Kind() == "func_literal" {
				goCollectFunctionLiteralReachableCalls(arg, source, functionNames, localNameBindings, functionRootKinds)
				continue
			}
			goCollectFunctionValuesFromExpression(arg, source, functionNames, methodKeys, variableTypes, localNameBindings, functionRootKinds)
		}
		return
	case "func_literal":
		if goFunctionLiteralIsCompositeElement(node) {
			goCollectFunctionLiteralReachableCalls(node, source, functionNames, localNameBindings, functionRootKinds)
		}
		return
	case "identifier":
		rawName := strings.TrimSpace(nodeText(node, source))
		name := strings.ToLower(rawName)
		if _, ok := functionNames[name]; ok && !goNameIsLocallyBound(rawName, nodeLine(node), localNameBindings) {
			functionRootKinds[name] = appendUniqueImportAlias(functionRootKinds[name], "go.function_value_reference")
		}
		return
	case "selector_expression":
		base, field, ok := goSelectorBaseAndField(node, source)
		if !ok {
			return
		}
		receiverType := variableTypes[strings.ToLower(base)]
		if receiverType == "" {
			receiverType = goSelectorConversionReceiverType(node, source)
		}
		if receiverType == "" {
			receiverType = goReceiverTypeFromConversionText(base)
		}
		key := receiverType + "." + strings.ToLower(field)
		if receiverType != "" {
			if _, ok := methodKeys[key]; ok {
				functionRootKinds[key] = appendUniqueImportAlias(functionRootKinds[key], "go.method_value_reference")
			}
		}
		return
	}

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		goCollectFunctionValuesFromExpression(&child, source, functionNames, methodKeys, variableTypes, localNameBindings, functionRootKinds)
	}
}

func goReceiverTypeFromConversionText(base string) string {
	trimmed := strings.TrimSpace(base)
	if trimmed == "" || !strings.HasSuffix(trimmed, ")") {
		return ""
	}
	index := strings.Index(trimmed, "(")
	if index <= 0 {
		return ""
	}
	return strings.ToLower(goNormalizeTypeName(trimmed[:index]))
}

func goSelectorConversionReceiverType(node *tree_sitter.Node, source []byte) string {
	operand := node.ChildByFieldName("operand")
	if operand == nil {
		cursor := node.Walk()
		defer cursor.Close()
		children := node.NamedChildren(cursor)
		if len(children) > 0 {
			operand = &children[0]
		}
	}
	if operand == nil || operand.Kind() != "call_expression" {
		return ""
	}
	functionNode := operand.ChildByFieldName("function")
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

func goCollectFunctionLiteralReachableCalls(
	node *tree_sitter.Node,
	source []byte,
	functionNames map[string]struct{},
	localNameBindings []goLocalNameBinding,
	functionRootKinds map[string][]string,
) {
	walkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "call_expression" {
			return
		}
		functionNode := child.ChildByFieldName("function")
		if functionNode == nil || functionNode.Kind() != "identifier" {
			return
		}
		rawName := strings.TrimSpace(nodeText(functionNode, source))
		name := strings.ToLower(rawName)
		if _, ok := functionNames[name]; !ok || goNameIsLocallyBound(rawName, nodeLine(functionNode), localNameBindings) {
			return
		}
		functionRootKinds[name] = appendUniqueImportAlias(functionRootKinds[name], "go.function_literal_reachable_call")
	})
}

func goReferencedLocalInterfaces(
	node *tree_sitter.Node,
	source []byte,
	interfaceMethods map[string][]string,
) []string {
	if node == nil {
		return nil
	}
	refs := make([]string, 0)
	walkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "type_identifier" {
			return
		}
		name := strings.ToLower(strings.TrimSpace(nodeText(child, source)))
		if _, ok := interfaceMethods[name]; ok {
			refs = appendUniqueImportAlias(refs, name)
		}
	})
	return refs
}

func goConcreteTypesInExpression(
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
) []string {
	types := make([]string, 0)
	if node == nil {
		return types
	}
	walkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "composite_literal" {
			return
		}
		if concreteType := goConcreteTypeFromExpression(child, source, structTypes); concreteType != "" {
			types = appendUniqueImportAlias(types, concreteType)
		}
	})
	return types
}

func goConcreteTypeFromExpression(
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier":
		return ""
	case "composite_literal":
		return goConcreteTypeFromTypeNode(node.ChildByFieldName("type"), source, structTypes)
	case "expression_list", "literal_element", "parenthesized_expression", "unary_expression":
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			if concreteType := goConcreteTypeFromExpression(&child, source, structTypes); concreteType != "" {
				return concreteType
			}
		}
	}
	return ""
}

func goConcreteTypeFromTypeNode(
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
) string {
	if node == nil {
		return ""
	}
	name := ""
	if node.Kind() == "type_identifier" {
		name = strings.ToLower(strings.TrimSpace(nodeText(node, source)))
	} else {
		walkNamed(node, func(child *tree_sitter.Node) {
			if name != "" || child.Kind() != "type_identifier" {
				return
			}
			name = strings.ToLower(strings.TrimSpace(nodeText(child, source)))
		})
	}
	if _, ok := structTypes[name]; ok {
		return name
	}
	return ""
}

func goInterfaceTargetFromTypeNode(
	node *tree_sitter.Node,
	source []byte,
	interfaceMethods map[string][]string,
) goInterfaceTarget {
	if node == nil {
		return goInterfaceTarget{}
	}
	localRefs := goReferencedLocalInterfaces(node, source, interfaceMethods)
	if len(localRefs) > 0 {
		return goInterfaceTarget{localInterface: localRefs[0]}
	}
	text := strings.TrimSpace(nodeText(node, source))
	if strings.Contains(text, ".") {
		return goInterfaceTarget{imported: true, importedMethods: goKnownImportedInterfaceMethods(text)}
	}
	return goInterfaceTarget{}
}

func goKnownImportedInterfaceMethods(typeName string) []string {
	switch strings.TrimSpace(typeName) {
	case "io.Closer":
		return []string{"close"}
	case "sourcecypher.Executor", "cypher.Executor":
		return []string{"execute", "executegroup"}
	case "sourcecypher.GroupExecutor", "cypher.GroupExecutor":
		return []string{"executegroup"}
	default:
		return nil
	}
}

func goStructFieldInterfaceTargets(
	root *tree_sitter.Node,
	source []byte,
	interfaceMethods map[string][]string,
) map[string]map[string]goInterfaceTarget {
	targets := make(map[string]map[string]goInterfaceTarget)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "type_spec" {
			return
		}
		typeName := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
		typeNode := node.ChildByFieldName("type")
		if typeName == "" || typeNode == nil || typeNode.Kind() != "struct_type" {
			return
		}
		walkNamed(typeNode, func(child *tree_sitter.Node) {
			if child.Kind() != "field_declaration" {
				return
			}
			target := goInterfaceTargetFromTypeNode(child.ChildByFieldName("type"), source, interfaceMethods)
			if !target.modeled() {
				return
			}
			for _, fieldName := range goIdentifierNames(child.ChildByFieldName("name"), source) {
				if _, ok := targets[typeName]; !ok {
					targets[typeName] = make(map[string]goInterfaceTarget)
				}
				targets[typeName][fieldName] = target
			}
		})
	})
	return targets
}

func goFunctionParamInterfaceTargets(
	root *tree_sitter.Node,
	source []byte,
	interfaceMethods map[string][]string,
) map[string]map[int]goInterfaceTarget {
	targets := make(map[string]map[int]goInterfaceTarget)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		name := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
		if name == "" {
			return
		}
		paramIndex := 0
		params := node.ChildByFieldName("parameters")
		walkDirectNamed(params, func(param *tree_sitter.Node) {
			if param.Kind() != "parameter_declaration" {
				return
			}
			target := goInterfaceTargetFromTypeNode(param.ChildByFieldName("type"), source, interfaceMethods)
			nameCount := len(goIdentifierNames(param.ChildByFieldName("name"), source))
			if nameCount == 0 {
				nameCount = 1
			}
			for range nameCount {
				if target.modeled() {
					if _, ok := targets[name]; !ok {
						targets[name] = make(map[int]goInterfaceTarget)
					}
					targets[name][paramIndex] = target
				}
				paramIndex++
			}
		})
	})
	return targets
}

func goFunctionParamImportedInterfaceMethods(
	root *tree_sitter.Node,
	source []byte,
) GoImportedInterfaceParamMethods {
	targets := goFunctionParamInterfaceTargets(root, source, nil)
	importedMethods := make(GoImportedInterfaceParamMethods)
	for functionName, byIndex := range targets {
		for index, target := range byIndex {
			if !target.imported || len(target.importedMethods) == 0 {
				continue
			}
			if _, ok := importedMethods[functionName]; !ok {
				importedMethods[functionName] = make(map[int][]string)
			}
			importedMethods[functionName][index] = appendUniqueMethods(
				importedMethods[functionName][index],
				target.importedMethods,
			)
		}
	}
	return importedMethods
}

func goMergeImportedInterfaceParamTargets(
	targets map[string]map[int]goInterfaceTarget,
	importedMethods GoImportedInterfaceParamMethods,
) {
	for functionName, byIndex := range importedMethods {
		if _, ok := targets[functionName]; !ok {
			targets[functionName] = make(map[int]goInterfaceTarget)
		}
		for index, methods := range byIndex {
			existing := targets[functionName][index]
			if existing.localInterface != "" {
				continue
			}
			existing.imported = true
			existing.importedMethods = appendUniqueMethods(existing.importedMethods, methods)
			targets[functionName][index] = existing
		}
	}
}

func appendUniqueMethods(target []string, methods []string) []string {
	for _, method := range methods {
		if trimmed := strings.TrimSpace(method); trimmed != "" {
			target = appendUniqueImportAlias(target, strings.ToLower(trimmed))
		}
	}
	return target
}

func goFunctionParamCallbackIndexes(
	root *tree_sitter.Node,
	source []byte,
	functionTypeNames map[string]struct{},
) map[string]map[int]struct{} {
	targets := make(map[string]map[int]struct{})
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		name := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
		if name == "" {
			return
		}
		paramIndex := 0
		params := node.ChildByFieldName("parameters")
		walkDirectNamed(params, func(param *tree_sitter.Node) {
			if param.Kind() != "parameter_declaration" {
				return
			}
			typeNode := param.ChildByFieldName("type")
			nameCount := len(goIdentifierNames(param.ChildByFieldName("name"), source))
			if nameCount == 0 {
				nameCount = 1
			}
			for range nameCount {
				if goTypeNodeIsFunctionValue(typeNode, source, functionTypeNames) {
					if _, ok := targets[name]; !ok {
						targets[name] = make(map[int]struct{})
					}
					targets[name][paramIndex] = struct{}{}
				}
				paramIndex++
			}
		})
	})
	return targets
}

func goTypeNodeIsFunctionValue(
	node *tree_sitter.Node,
	source []byte,
	functionTypeNames map[string]struct{},
) bool {
	if node == nil {
		return false
	}
	if node.Kind() == "function_type" {
		return true
	}
	for _, name := range goIdentifierNames(node, source) {
		if _, ok := functionTypeNames[name]; ok {
			return true
		}
	}
	return false
}

func walkDirectNamed(node *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	if node == nil {
		return
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		visit(&child)
	}
}
