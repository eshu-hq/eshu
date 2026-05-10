package golang

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type goInterfaceTarget struct {
	localInterface  string
	imported        bool
	importedMethods []string
}

func (target goInterfaceTarget) modeled() bool {
	return target.localInterface != "" || target.imported
}

func goCollectSemanticDeadCodeRoots(
	root *tree_sitter.Node,
	source []byte,
	importedParamMethods GoImportedInterfaceParamMethods,
	localNameBindings []goLocalNameBinding,
	functionRootKinds map[string][]string,
	interfaceRootKinds map[string][]string,
	structRootKinds map[string][]string,
) {
	if root == nil {
		return
	}

	functionNames := make(map[string]struct{})
	functionTypeNames := make(map[string]struct{})
	methodKeys := make(map[string]struct{})
	methodNamesByReceiver := make(map[string][]string)
	structTypes := make(map[string]struct{})
	interfaceMethods := make(map[string][]string)

	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration":
			name := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
			if name != "" {
				functionNames[name] = struct{}{}
			}
		case "method_declaration":
			name := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
			receiver := strings.ToLower(goReceiverContext(node, source))
			if name != "" && receiver != "" {
				methodKeys[receiver+"."+name] = struct{}{}
				methodNamesByReceiver[receiver] = appendUniqueImportAlias(methodNamesByReceiver[receiver], name)
			}
		case "type_spec":
			name := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
			typeNode := node.ChildByFieldName("type")
			if name == "" || typeNode == nil {
				return
			}
			switch typeNode.Kind() {
			case "struct_type":
				structTypes[name] = struct{}{}
			case "interface_type":
				interfaceMethods[name] = goInterfaceMethodNames(typeNode, source)
			case "function_type":
				functionTypeNames[name] = struct{}{}
			}
		}
	})

	variableTypes := goKnownLocalVariableTypes(root, source, structTypes)
	interfaceConcreteTypes := make(map[string][]string)
	structFieldTargets := goStructFieldInterfaceTargets(root, source, interfaceMethods)
	functionParamTargets := goFunctionParamInterfaceTargets(root, source, interfaceMethods)
	goMergeImportedInterfaceParamTargets(functionParamTargets, importedParamMethods)
	functionCallbackParams := goFunctionParamCallbackIndexes(root, source, functionTypeNames)

	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "var_spec":
			typeNode := node.ChildByFieldName("type")
			valueNode := node.ChildByFieldName("value")
			for _, interfaceName := range goReferencedLocalInterfaces(typeNode, source, interfaceMethods) {
				interfaceRootKinds[interfaceName] = appendUniqueImportAlias(interfaceRootKinds[interfaceName], "go.interface_type_reference")
				for _, concreteType := range goConcreteTypesInExpression(valueNode, source, structTypes) {
					interfaceConcreteTypes[interfaceName] = appendUniqueImportAlias(interfaceConcreteTypes[interfaceName], concreteType)
				}
			}
			goCollectFunctionValuesFromExpression(valueNode, source, functionNames, methodKeys, variableTypes, localNameBindings, functionRootKinds)
		case "short_var_declaration", "assignment_statement":
			goCollectFunctionValuesFromExpression(node.ChildByFieldName("right"), source, functionNames, methodKeys, variableTypes, localNameBindings, functionRootKinds)
		case "composite_literal":
			if concreteType := goConcreteTypeFromTypeNode(node.ChildByFieldName("type"), source, structTypes); concreteType != "" {
				structRootKinds[concreteType] = appendUniqueImportAlias(structRootKinds[concreteType], "go.type_reference")
			}
			typeNode := node.ChildByFieldName("type")
			for _, interfaceName := range goReferencedLocalInterfaces(typeNode, source, interfaceMethods) {
				interfaceRootKinds[interfaceName] = appendUniqueImportAlias(interfaceRootKinds[interfaceName], "go.interface_type_reference")
				for _, concreteType := range goConcreteTypesInExpression(node, source, structTypes) {
					interfaceConcreteTypes[interfaceName] = appendUniqueImportAlias(interfaceConcreteTypes[interfaceName], concreteType)
				}
			}
			goCollectFunctionValuesFromExpression(node, source, functionNames, methodKeys, variableTypes, localNameBindings, functionRootKinds)
			goMarkCompositeLiteralInterfaceFields(
				node,
				source,
				structTypes,
				structFieldTargets,
				interfaceMethods,
				interfaceConcreteTypes,
				methodNamesByReceiver,
				functionRootKinds,
				structRootKinds,
			)
		case "parameter_declaration":
			for _, interfaceName := range goReferencedLocalInterfaces(node.ChildByFieldName("type"), source, interfaceMethods) {
				interfaceRootKinds[interfaceName] = appendUniqueImportAlias(interfaceRootKinds[interfaceName], "go.interface_type_reference")
			}
		case "field_declaration":
			for _, interfaceName := range goReferencedLocalInterfaces(node.ChildByFieldName("type"), source, interfaceMethods) {
				interfaceRootKinds[interfaceName] = appendUniqueImportAlias(interfaceRootKinds[interfaceName], "go.interface_type_reference")
			}
		case "function_declaration":
			goCollectInterfaceReturnConcreteTypes(
				node,
				source,
				structTypes,
				interfaceMethods,
				interfaceRootKinds,
				interfaceConcreteTypes,
				methodNamesByReceiver,
				functionRootKinds,
				structRootKinds,
			)
		case "return_statement":
			goCollectFunctionValuesFromExpression(node, source, functionNames, methodKeys, variableTypes, localNameBindings, functionRootKinds)
		case "call_expression":
			goCollectFunctionValuesFromExpression(node, source, functionNames, methodKeys, variableTypes, localNameBindings, functionRootKinds)
			goCollectDependencyInjectionCallbacksFromCall(
				node,
				source,
				functionNames,
				methodKeys,
				variableTypes,
				functionCallbackParams,
				functionRootKinds,
			)
			goMarkCallArgumentInterfaceMethods(
				node,
				source,
				structTypes,
				variableTypes,
				functionParamTargets,
				interfaceMethods,
				interfaceConcreteTypes,
				methodNamesByReceiver,
				functionRootKinds,
				structRootKinds,
			)
		}
	})

	for interfaceName, concreteTypes := range interfaceConcreteTypes {
		for _, methodName := range interfaceMethods[interfaceName] {
			for _, concreteType := range concreteTypes {
				key := concreteType + "." + methodName
				if _, ok := methodKeys[key]; ok {
					functionRootKinds[key] = appendUniqueImportAlias(functionRootKinds[key], "go.interface_method_implementation")
					structRootKinds[concreteType] = appendUniqueImportAlias(structRootKinds[concreteType], "go.interface_implementation_type")
				}
			}
		}
	}
}

func goCollectDependencyInjectionCallbacksFromCall(
	node *tree_sitter.Node,
	source []byte,
	functionNames map[string]struct{},
	methodKeys map[string]struct{},
	variableTypes map[string]string,
	functionCallbackParams map[string]map[int]struct{},
	functionRootKinds map[string][]string,
) {
	functionName := goCallFunctionName(node, source)
	if functionName == "" || len(functionCallbackParams[functionName]) == 0 {
		return
	}
	argumentsNode := node.ChildByFieldName("arguments")
	argIndex := 0
	walkDirectNamed(argumentsNode, func(child *tree_sitter.Node) {
		if child.Kind() == "," {
			return
		}
		if _, ok := functionCallbackParams[functionName][argIndex]; !ok {
			argIndex++
			return
		}
		goCollectFunctionCallbackArgument(child, source, functionNames, methodKeys, variableTypes, functionRootKinds)
		argIndex++
	})
}

func goCollectFunctionCallbackArgument(
	node *tree_sitter.Node,
	source []byte,
	functionNames map[string]struct{},
	methodKeys map[string]struct{},
	variableTypes map[string]string,
	functionRootKinds map[string][]string,
) {
	if node == nil {
		return
	}
	switch node.Kind() {
	case "identifier":
		name := strings.ToLower(strings.TrimSpace(nodeText(node, source)))
		if _, ok := functionNames[name]; ok {
			functionRootKinds[name] = appendUniqueImportAlias(functionRootKinds[name], "go.dependency_injection_callback")
		}
	case "selector_expression":
		base, field, ok := goSelectorBaseAndField(node, source)
		if !ok {
			return
		}
		receiverType := variableTypes[strings.ToLower(base)]
		key := receiverType + "." + strings.ToLower(field)
		if _, ok := methodKeys[key]; ok {
			functionRootKinds[key] = appendUniqueImportAlias(functionRootKinds[key], "go.dependency_injection_callback")
		}
	}
}
