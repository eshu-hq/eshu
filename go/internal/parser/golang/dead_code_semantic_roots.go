// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type goInterfaceTarget struct {
	localInterface       string
	imported             bool
	importedMethods      []string
	allowExportedMethods bool
}

func (target goInterfaceTarget) modeled() bool {
	return target.localInterface != "" || target.imported
}

func goCollectSemanticDeadCodeRoots(
	root *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	importedParamMethods GoImportedInterfaceParamMethods,
	localNameBindings []goLocalNameBinding,
	constructorReturns map[string]string,
	functionRootKinds map[string][]string,
	interfaceRootKinds map[string][]string,
	structRootKinds map[string][]string,
	lookup *goParentLookup,
) {
	if root == nil {
		return
	}

	functionNames := make(map[string]struct{})
	functionTypeNames := make(map[string]struct{})
	methodKeys := make(map[string]struct{})
	methodNamesByReceiver := make(map[string][]string)
	exportedMethodNamesByReceiver := make(map[string][]string)
	structTypes := make(map[string]struct{})
	interfaceMethods := make(map[string][]string)

	// Gather resolution-candidate node pointers during the declaration
	// walk so they can be resolved in plain in-memory loops instead of
	// a second full-tree traversal. Tree-sitter *tree_sitter.Node values
	// point at stack-allocated cursors during the recursive walk; every
	// gathered node is cloned (shared.CloneNode) exactly as the
	// existing variable_type_index.go and helpers.go sites do for
	// retained nodes.
	var gatheredVarSpecs []*tree_sitter.Node
	var gatheredShortVarDecls []*tree_sitter.Node
	var gatheredAssignmentStmts []*tree_sitter.Node
	var gatheredCompositeLiterals []*tree_sitter.Node
	var gatheredParamDecls []*tree_sitter.Node
	var gatheredFieldDecls []*tree_sitter.Node
	var gatheredFuncDecls []*tree_sitter.Node
	var gatheredReturnStmts []*tree_sitter.Node
	var gatheredCallExprs []*tree_sitter.Node
	var gatheredTypeParamDecls []*tree_sitter.Node

	// Walk-1: collect declarations AND gather resolution-candidate nodes
	// for post-walk in-memory iteration. Appending during pre-order gives
	// pre-order slices, matching the original walk-2's visitation order.
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration":
			name := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
			if name != "" {
				functionNames[name] = struct{}{}
			}
			gatheredFuncDecls = append(gatheredFuncDecls, shared.CloneNode(node))
		case "method_declaration":
			name := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
			receiver := strings.ToLower(goReceiverContext(node, source))
			if name != "" && receiver != "" {
				methodKeys[receiver+"."+name] = struct{}{}
				methodNamesByReceiver[receiver] = appendUniqueImportAlias(methodNamesByReceiver[receiver], name)
				if goIdentifierIsExported(nodeText(node.ChildByFieldName("name"), source)) {
					exportedMethodNamesByReceiver[receiver] = appendUniqueImportAlias(exportedMethodNamesByReceiver[receiver], name)
				}
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
		case "var_spec":
			gatheredVarSpecs = append(gatheredVarSpecs, shared.CloneNode(node))
		case "short_var_declaration":
			gatheredShortVarDecls = append(gatheredShortVarDecls, shared.CloneNode(node))
		case "assignment_statement":
			gatheredAssignmentStmts = append(gatheredAssignmentStmts, shared.CloneNode(node))
		case "composite_literal":
			gatheredCompositeLiterals = append(gatheredCompositeLiterals, shared.CloneNode(node))
		case "parameter_declaration":
			gatheredParamDecls = append(gatheredParamDecls, shared.CloneNode(node))
		case "field_declaration":
			gatheredFieldDecls = append(gatheredFieldDecls, shared.CloneNode(node))
		case "return_statement":
			gatheredReturnStmts = append(gatheredReturnStmts, shared.CloneNode(node))
		case "call_expression":
			gatheredCallExprs = append(gatheredCallExprs, shared.CloneNode(node))
		case "type_parameter_declaration":
			gatheredTypeParamDecls = append(gatheredTypeParamDecls, shared.CloneNode(node))
		}
	})

	interfaceConcreteTypes := make(map[string][]string)
	structFieldTypes := goStructFieldConcreteTypes(root, source, structTypes)
	structFieldTargets := goStructFieldInterfaceTargets(root, source, interfaceMethods)
	functionParamTargets := goFunctionParamInterfaceTargets(root, source, interfaceMethods)
	goMergeImportedInterfaceParamTargets(functionParamTargets, importedParamMethods)
	functionCallbackParams := goFunctionParamCallbackIndexes(root, source, functionTypeNames)
	// Variable-type lookups happen once per resolution node in the
	// gathered loops below. The index folds the file's package-level
	// walk into a single pass and memoises per-scope binding lists;
	// without it each call cost a fresh full tree walk (see #161
	// follow-up).
	variableTypeIndex := goBuildVariableTypeIndex(root, source, structTypes, constructorReturns, lookup)

	// Walk-2 replacement: in-memory loops over the gathered resolution-
	// candidate nodes, one slice per node kind. Each loop runs the
	// identical logic the original walk-2's switch cases ran, preserving
	// call order, arguments, and additive-map semantics. The slices are
	// pre-ordered by walk-1's pre-order traversal, matching the original
	// walk-2's visitation order within each kind.

	for _, node := range gatheredVarSpecs {
		scopedVariableTypes := variableTypeIndex.ForNode(root, node)
		typeNode := node.ChildByFieldName("type")
		valueNode := node.ChildByFieldName("value")
		for _, interfaceName := range goReferencedLocalInterfaces(typeNode, source, interfaceMethods) {
			interfaceRootKinds[interfaceName] = appendUniqueImportAlias(interfaceRootKinds[interfaceName], "go.interface_type_reference")
			for _, concreteType := range goConcreteTypesInExpression(valueNode, source, structTypes) {
				interfaceConcreteTypes[interfaceName] = appendUniqueImportAlias(interfaceConcreteTypes[interfaceName], concreteType)
			}
		}
		goCollectFunctionValuesFromExpression(valueNode, source, functionNames, methodKeys, scopedVariableTypes, localNameBindings, functionRootKinds, lookup)
	}

	for _, node := range gatheredShortVarDecls {
		scopedVariableTypes := variableTypeIndex.ForNode(root, node)
		goCollectFunctionValuesFromExpression(node.ChildByFieldName("right"), source, functionNames, methodKeys, scopedVariableTypes, localNameBindings, functionRootKinds, lookup)
	}

	for _, node := range gatheredAssignmentStmts {
		scopedVariableTypes := variableTypeIndex.ForNode(root, node)
		goCollectFunctionValuesFromExpression(node.ChildByFieldName("right"), source, functionNames, methodKeys, scopedVariableTypes, localNameBindings, functionRootKinds, lookup)
	}

	for _, node := range gatheredCompositeLiterals {
		scopedVariableTypes := variableTypeIndex.ForNode(root, node)
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
		goCollectFunctionValuesFromExpression(node, source, functionNames, methodKeys, scopedVariableTypes, localNameBindings, functionRootKinds, lookup)
		goMarkCompositeLiteralInterfaceFields(
			node,
			source,
			structTypes,
			structFieldTargets,
			interfaceMethods,
			interfaceConcreteTypes,
			methodNamesByReceiver,
			exportedMethodNamesByReceiver,
			functionRootKinds,
			structRootKinds,
		)
	}

	for _, node := range gatheredParamDecls {
		for _, interfaceName := range goReferencedLocalInterfaces(node.ChildByFieldName("type"), source, interfaceMethods) {
			interfaceRootKinds[interfaceName] = appendUniqueImportAlias(interfaceRootKinds[interfaceName], "go.interface_type_reference")
		}
	}

	for _, node := range gatheredFieldDecls {
		for _, interfaceName := range goReferencedLocalInterfaces(node.ChildByFieldName("type"), source, interfaceMethods) {
			interfaceRootKinds[interfaceName] = appendUniqueImportAlias(interfaceRootKinds[interfaceName], "go.interface_type_reference")
		}
	}

	for _, node := range gatheredFuncDecls {
		goCollectInterfaceReturnConcreteTypes(
			node,
			source,
			structTypes,
			interfaceMethods,
			interfaceRootKinds,
			interfaceConcreteTypes,
			methodNamesByReceiver,
			exportedMethodNamesByReceiver,
			functionRootKinds,
			structRootKinds,
		)
	}

	for _, node := range gatheredReturnStmts {
		scopedVariableTypes := variableTypeIndex.ForNode(root, node)
		goCollectFunctionValuesFromExpression(node, source, functionNames, methodKeys, scopedVariableTypes, localNameBindings, functionRootKinds, lookup)
	}

	for _, node := range gatheredCallExprs {
		scopedVariableTypes := variableTypeIndex.ForNode(root, node)
		goCollectFunctionValuesFromExpression(node, source, functionNames, methodKeys, scopedVariableTypes, localNameBindings, functionRootKinds, lookup)
		goCollectDirectMethodCallRoot(node, source, methodKeys, scopedVariableTypes, structFieldTypes, functionRootKinds, lookup)
		goCollectFmtStringerRoot(node, source, importAliases, methodKeys, scopedVariableTypes, structTypes, functionRootKinds)
		goCollectDependencyInjectionCallbacksFromCall(
			node,
			source,
			functionNames,
			methodKeys,
			scopedVariableTypes,
			functionCallbackParams,
			functionRootKinds,
		)
		goMarkCallArgumentInterfaceMethods(
			node,
			source,
			structTypes,
			scopedVariableTypes,
			functionParamTargets,
			importAliases,
			interfaceMethods,
			interfaceConcreteTypes,
			methodNamesByReceiver,
			exportedMethodNamesByReceiver,
			functionRootKinds,
			structRootKinds,
		)
	}

	// Walk-3 replacement: in-memory loop over gathered type-parameter
	// nodes instead of a third full-tree traversal.
	goMarkGenericConstraintInterfaceRoots(gatheredTypeParamDecls, source, interfaceMethods, methodKeys, functionRootKinds, interfaceRootKinds, structRootKinds)

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

// goMarkGenericConstraintInterfaceRoots marks interface root kinds and
// generic constraint method roots for each gathered type-parameter
// declaration that mentions a known interface. gatheredTypeParams must
// contain every type_parameter_declaration node in the file in source
// order (it is iterated in order and any repeat or omission changes the
// output identical to the original full-tree walk).
func goMarkGenericConstraintInterfaceRoots(
	gatheredTypeParams []*tree_sitter.Node,
	source []byte,
	interfaceMethods map[string][]string,
	methodKeys map[string]struct{},
	functionRootKinds map[string][]string,
	interfaceRootKinds map[string][]string,
	structRootKinds map[string][]string,
) {
	if len(interfaceMethods) == 0 || len(gatheredTypeParams) == 0 {
		return
	}
	for _, node := range gatheredTypeParams {
		text := strings.ToLower(nodeText(node, source))
		for interfaceName, methodNames := range interfaceMethods {
			if !goTypeParameterMentionsConstraint(text, interfaceName) {
				continue
			}
			interfaceRootKinds[interfaceName] = appendUniqueImportAlias(interfaceRootKinds[interfaceName], "go.interface_type_reference")
			for _, methodName := range methodNames {
				for methodKey := range methodKeys {
					receiver, candidate, ok := strings.Cut(methodKey, ".")
					if !ok || candidate != methodName {
						continue
					}
					functionRootKinds[methodKey] = appendUniqueImportAlias(functionRootKinds[methodKey], "go.generic_constraint_method")
					structRootKinds[receiver] = appendUniqueImportAlias(structRootKinds[receiver], "go.interface_implementation_type")
				}
			}
		}
	}
}

func goTypeParameterMentionsConstraint(text string, interfaceName string) bool {
	for _, field := range goTypeParameterConstraintCandidates(text) {
		if field == interfaceName {
			return true
		}
	}
	return false
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
