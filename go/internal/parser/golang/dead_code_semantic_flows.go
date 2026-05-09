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
	interfaceMethods map[string][]string,
	interfaceConcreteTypes map[string][]string,
	methodNamesByReceiver map[string][]string,
	functionRootKinds map[string][]string,
	structRootKinds map[string][]string,
) {
	functionName := goCallFunctionName(node, source)
	if functionName == "" || len(functionParamTargets[functionName]) == 0 {
		return
	}
	for index, arg := range goCallArgumentNodes(node) {
		target, ok := functionParamTargets[functionName][index]
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
	for _, methodName := range methodNamesByReceiver[concreteType] {
		if len(target.importedMethods) > 0 && !slices.Contains(target.importedMethods, methodName) {
			continue
		}
		key := concreteType + "." + methodName
		functionRootKinds[key] = appendUniqueImportAlias(functionRootKinds[key], "go.interface_method_implementation")
	}
	structRootKinds[concreteType] = appendUniqueImportAlias(structRootKinds[concreteType], "go.interface_implementation_type")
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
