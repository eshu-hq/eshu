// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/golang/symbols"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
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
	for _, interfaceName := range symbols.ReferencedLocalInterfaces(resultNode, source, interfaceMethods) {
		interfaceRootKinds[interfaceName] = symbols.AppendUniqueImportAlias(interfaceRootKinds[interfaceName], "go.interface_type_reference")
		shared.WalkNamed(node, func(child *tree_sitter.Node) {
			if child.Kind() != "return_statement" {
				return
			}
			for _, concreteType := range goConcreteTypesInExpression(child, source, structTypes) {
				interfaceConcreteTypes[interfaceName] = symbols.AppendUniqueImportAlias(interfaceConcreteTypes[interfaceName], concreteType)
			}
		})
	}

	importedTarget := symbols.InterfaceTargetFromTypeNode(resultNode, source, interfaceMethods)
	if !importedTarget.Imported {
		return
	}
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
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
	structFieldTargets map[string]map[string]symbols.InterfaceTarget,
	interfaceMethods map[string][]string,
	interfaceConcreteTypes map[string][]string,
	methodNamesByReceiver map[string][]string,
	exportedMethodNamesByReceiver map[string][]string,
	functionRootKinds map[string][]string,
	structRootKinds map[string][]string,
) {
	structType := symbols.ConcreteTypeFromTypeNode(node.ChildByFieldName("type"), source, structTypes)
	if structType == "" || len(structFieldTargets[structType]) == 0 {
		return
	}
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
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
		concreteType := symbols.ConcreteTypeFromExpression(valueNode, source, structTypes)
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
	functionParamTargets map[string]map[int]symbols.InterfaceTarget,
	importAliases map[string][]string,
	interfaceMethods map[string][]string,
	interfaceConcreteTypes map[string][]string,
	methodNamesByReceiver map[string][]string,
	exportedMethodNamesByReceiver map[string][]string,
	functionRootKinds map[string][]string,
	structRootKinds map[string][]string,
) {
	functionName := goCallFunctionName(node, source)
	qualifiedFunctionName := symbols.QualifiedCallFunctionName(node, source, importAliases)
	if functionName == "" && qualifiedFunctionName == "" {
		return
	}
	for index, arg := range symbols.CallArgumentNodes(node) {
		target, ok := functionParamTargets[qualifiedFunctionName][index]
		if !ok {
			target, ok = functionParamTargets[functionName][index]
		}
		if !ok {
			continue
		}
		concreteType := symbols.ConcreteTypeFromExpression(arg, source, structTypes)
		if concreteType == "" && arg.Kind() == "identifier" {
			concreteType = variableTypes[strings.ToLower(strings.TrimSpace(shared.NodeText(arg, source)))]
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
	target symbols.InterfaceTarget,
	interfaceConcreteTypes map[string][]string,
	methodNamesByReceiver map[string][]string,
	exportedMethodNamesByReceiver map[string][]string,
	functionRootKinds map[string][]string,
	structRootKinds map[string][]string,
) {
	if concreteType == "" || !target.Modeled() {
		return
	}
	if target.LocalInterface != "" {
		interfaceConcreteTypes[target.LocalInterface] = symbols.AppendUniqueImportAlias(interfaceConcreteTypes[target.LocalInterface], concreteType)
		structRootKinds[concreteType] = symbols.AppendUniqueImportAlias(structRootKinds[concreteType], "go.interface_implementation_type")
		return
	}
	if !target.Imported {
		return
	}
	if len(target.ImportedMethods) == 0 && target.AllowExportedMethods {
		for _, methodName := range exportedMethodNamesByReceiver[concreteType] {
			key := concreteType + "." + methodName
			functionRootKinds[key] = symbols.AppendUniqueImportAlias(functionRootKinds[key], "go.interface_method_implementation")
		}
		structRootKinds[concreteType] = symbols.AppendUniqueImportAlias(structRootKinds[concreteType], "go.interface_implementation_type")
		return
	}
	if len(target.ImportedMethods) == 0 {
		return
	}
	for _, methodName := range methodNamesByReceiver[concreteType] {
		if !slices.Contains(target.ImportedMethods, methodName) {
			continue
		}
		key := concreteType + "." + methodName
		functionRootKinds[key] = symbols.AppendUniqueImportAlias(functionRootKinds[key], "go.interface_method_implementation")
	}
	structRootKinds[concreteType] = symbols.AppendUniqueImportAlias(structRootKinds[concreteType], "go.interface_implementation_type")
}

func goCollectDirectMethodCallRoot(
	node *tree_sitter.Node,
	source []byte,
	methodKeys map[string]struct{},
	variableTypes map[string]string,
	structFieldTypes map[string]map[string]string,
	functionRootKinds map[string][]string,
	lookup *symbols.ParentLookup,
) {
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil || functionNode.Kind() != "selector_expression" {
		return
	}
	receiver, methodName, ok := symbols.SelectorBaseAndField(functionNode, source)
	if !ok {
		return
	}
	methodName = strings.ToLower(strings.TrimSpace(methodName))
	if methodName == "" {
		return
	}
	enclosingReceiver, enclosingType := symbols.EnclosingMethodReceiver(node, source, lookup)
	if strings.TrimSpace(receiver) == enclosingReceiver && enclosingType != "" {
		key := strings.ToLower(enclosingType) + "." + methodName
		if _, ok := methodKeys[key]; ok {
			functionRootKinds[key] = symbols.AppendUniqueImportAlias(functionRootKinds[key], "go.direct_method_call")
			return
		}
	}
	if receiverType := variableTypes[strings.ToLower(strings.TrimSpace(receiver))]; receiverType != "" {
		key := receiverType + "." + methodName
		if _, ok := methodKeys[key]; ok {
			functionRootKinds[key] = symbols.AppendUniqueImportAlias(functionRootKinds[key], "go.direct_method_call")
		}
	}
	if receiverType := goFieldSelectorReceiverType(receiver, enclosingReceiver, enclosingType, variableTypes, structFieldTypes); receiverType != "" {
		key := receiverType + "." + methodName
		if _, ok := methodKeys[key]; ok {
			functionRootKinds[key] = symbols.AppendUniqueImportAlias(functionRootKinds[key], "go.direct_method_call")
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
	if !symbols.CallIsFmtFormatting(node, source, importAliases) {
		return
	}
	firstValueArg := symbols.FmtStringerFirstValueArgIndex(node, source, importAliases)
	for index, arg := range symbols.CallArgumentNodes(node) {
		if index < firstValueArg {
			continue
		}
		receiverType := goKnownReceiverTypeFromExpression(arg, source, variableTypes, structTypes)
		if receiverType == "" {
			continue
		}
		key := receiverType + ".string"
		if _, ok := methodKeys[key]; ok {
			functionRootKinds[key] = symbols.AppendUniqueImportAlias(functionRootKinds[key], "go.fmt_stringer_method")
		}
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
		return variableTypes[strings.ToLower(strings.TrimSpace(shared.NodeText(node, source)))]
	case "composite_literal":
		return symbols.ConcreteTypeFromTypeNode(node.ChildByFieldName("type"), source, structTypes)
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
	symbols.WalkDirectNamed(node, func(child *tree_sitter.Node) {
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
		return strings.ToLower(symbols.NormalizeTypeName(shared.NodeText(functionNode, source)))
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
		return strings.ToLower(strings.TrimSpace(shared.NodeText(functionNode, source)))
	case "selector_expression":
		_, field, ok := symbols.SelectorBaseAndField(functionNode, source)
		if ok {
			return strings.ToLower(strings.TrimSpace(field))
		}
	}
	return ""
}

func goKeyedElementFieldAndValue(node *tree_sitter.Node, source []byte) (string, *tree_sitter.Node) {
	keyNode := node.ChildByFieldName("key")
	valueNode := node.ChildByFieldName("value")
	if keyNode != nil && valueNode != nil {
		return strings.ToLower(strings.TrimSpace(shared.NodeText(keyNode, source))), valueNode
	}
	children := make([]*tree_sitter.Node, 0, 2)
	symbols.WalkDirectNamed(node, func(child *tree_sitter.Node) {
		children = append(children, child)
	})
	if len(children) < 2 {
		return "", nil
	}
	return strings.ToLower(strings.TrimSpace(shared.NodeText(children[0], source))), children[1]
}
