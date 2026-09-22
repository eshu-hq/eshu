// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/golang/symbols"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goCollectFunctionValuesFromExpression(
	node *tree_sitter.Node,
	source []byte,
	functionNames map[string]struct{},
	methodKeys map[string]struct{},
	variableTypes map[string]string,
	localNameBindings []symbols.LocalNameBinding,
	functionRootKinds map[string][]string,
	lookup *symbols.ParentLookup,
) {
	if node == nil {
		return
	}
	switch node.Kind() {
	case "call_expression":
		for _, arg := range symbols.CallArgumentNodes(node) {
			if arg.Kind() == "func_literal" {
				goCollectFunctionLiteralReachableCalls(arg, source, functionNames, localNameBindings, functionRootKinds)
				continue
			}
			goCollectFunctionValuesFromExpression(arg, source, functionNames, methodKeys, variableTypes, localNameBindings, functionRootKinds, lookup)
		}
		return
	case "func_literal":
		if symbols.FunctionLiteralIsCompositeElement(node, lookup) {
			goCollectFunctionLiteralReachableCalls(node, source, functionNames, localNameBindings, functionRootKinds)
		}
		return
	case "identifier":
		rawName := strings.TrimSpace(shared.NodeText(node, source))
		name := strings.ToLower(rawName)
		if _, ok := functionNames[name]; ok && !symbols.NameIsLocallyBound(rawName, shared.NodeLine(node), localNameBindings) {
			functionRootKinds[name] = symbols.AppendUniqueImportAlias(functionRootKinds[name], "go.function_value_reference")
		}
		return
	case "selector_expression":
		base, field, ok := symbols.SelectorBaseAndField(node, source)
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
				functionRootKinds[key] = symbols.AppendUniqueImportAlias(functionRootKinds[key], "go.method_value_reference")
			}
		}
		return
	}

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		goCollectFunctionValuesFromExpression(&child, source, functionNames, methodKeys, variableTypes, localNameBindings, functionRootKinds, lookup)
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
	return strings.ToLower(symbols.NormalizeTypeName(trimmed[:index]))
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
		return strings.ToLower(symbols.NormalizeTypeName(shared.NodeText(functionNode, source)))
	default:
		return ""
	}
}

func goCollectFunctionLiteralReachableCalls(
	node *tree_sitter.Node,
	source []byte,
	functionNames map[string]struct{},
	localNameBindings []symbols.LocalNameBinding,
	functionRootKinds map[string][]string,
) {
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "call_expression" {
			return
		}
		functionNode := child.ChildByFieldName("function")
		if functionNode == nil || functionNode.Kind() != "identifier" {
			return
		}
		rawName := strings.TrimSpace(shared.NodeText(functionNode, source))
		name := strings.ToLower(rawName)
		if _, ok := functionNames[name]; !ok || symbols.NameIsLocallyBound(rawName, shared.NodeLine(functionNode), localNameBindings) {
			return
		}
		functionRootKinds[name] = symbols.AppendUniqueImportAlias(functionRootKinds[name], "go.function_literal_reachable_call")
	})
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
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "composite_literal" {
			return
		}
		if concreteType := symbols.ConcreteTypeFromExpression(child, source, structTypes); concreteType != "" {
			types = symbols.AppendUniqueImportAlias(types, concreteType)
		}
	})
	return types
}

func goStructFieldInterfaceTargets(
	root *tree_sitter.Node,
	source []byte,
	interfaceMethods map[string][]string,
) map[string]map[string]symbols.InterfaceTarget {
	targets := make(map[string]map[string]symbols.InterfaceTarget)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "type_spec" {
			return
		}
		typeName := strings.ToLower(strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source)))
		typeNode := node.ChildByFieldName("type")
		if typeName == "" || typeNode == nil || typeNode.Kind() != "struct_type" {
			return
		}
		shared.WalkNamed(typeNode, func(child *tree_sitter.Node) {
			if child.Kind() != "field_declaration" {
				return
			}
			target := symbols.InterfaceTargetFromTypeNode(child.ChildByFieldName("type"), source, interfaceMethods)
			if !target.Modeled() {
				return
			}
			for _, fieldName := range symbols.IdentifierNames(child.ChildByFieldName("name"), source) {
				if _, ok := targets[typeName]; !ok {
					targets[typeName] = make(map[string]symbols.InterfaceTarget)
				}
				targets[typeName][fieldName] = target
			}
		})
	})
	return targets
}

func goMergeImportedInterfaceParamTargets(
	targets map[string]map[int]symbols.InterfaceTarget,
	importedMethods shared.GoImportedInterfaceParamMethods,
) {
	for functionName, byIndex := range importedMethods {
		if _, ok := targets[functionName]; !ok {
			targets[functionName] = make(map[int]symbols.InterfaceTarget)
		}
		for index, methods := range byIndex {
			existing := targets[functionName][index]
			if existing.LocalInterface != "" {
				continue
			}
			existing.Imported = true
			existing.ImportedMethods = symbols.AppendUniqueMethods(existing.ImportedMethods, methods)
			if len(methods) == 0 {
				existing.AllowExportedMethods = true
			}
			targets[functionName][index] = existing
		}
	}
}

func goFunctionParamCallbackIndexes(
	root *tree_sitter.Node,
	source []byte,
	functionTypeNames map[string]struct{},
) map[string]map[int]struct{} {
	targets := make(map[string]map[int]struct{})
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		name := strings.ToLower(strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source)))
		if name == "" {
			return
		}
		paramIndex := 0
		params := node.ChildByFieldName("parameters")
		symbols.WalkDirectNamed(params, func(param *tree_sitter.Node) {
			if param.Kind() != "parameter_declaration" {
				return
			}
			typeNode := param.ChildByFieldName("type")
			nameCount := len(symbols.IdentifierNames(param.ChildByFieldName("name"), source))
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
	for _, name := range symbols.IdentifierNames(node, source) {
		if _, ok := functionTypeNames[name]; ok {
			return true
		}
	}
	return false
}
