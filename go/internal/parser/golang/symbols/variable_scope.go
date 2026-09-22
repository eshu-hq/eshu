// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goKnownLocalPackageVariableTypes(
	root *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
	constructorReturns map[string]string,
	lookup *ParentLookup,
) map[string]string {
	variableTypes := make(map[string]string)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if goEnclosingFunctionScope(node, lookup) != nil {
			return
		}
		switch node.Kind() {
		case "var_spec":
			goRecordLocalVarSpecTypes(node, source, structTypes, constructorReturns, variableTypes)
		case "short_var_declaration", "assignment_statement":
			goRecordLocalAssignmentTypes(node, source, structTypes, constructorReturns, variableTypes)
		}
	})
	return variableTypes
}

func goRecordLocalParameterTypes(
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
	variableTypes map[string]string,
) {
	for name, typeName := range goLocalParameterTypes(node, source, structTypes) {
		variableTypes[name] = typeName
	}
}

func goRecordLocalVarSpecTypes(
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
	constructorReturns map[string]string,
	variableTypes map[string]string,
) {
	names := IdentifierNames(node.ChildByFieldName("name"), source)
	concreteType := ConcreteTypeFromExpression(node.ChildByFieldName("value"), source, structTypes)
	if concreteType == "" {
		concreteType = goConcreteTypeFromConstructorCall(
			node.ChildByFieldName("value"),
			source,
			structTypes,
			constructorReturns,
		)
	}
	if concreteType == "" {
		concreteType = ConcreteTypeFromTypeNode(node.ChildByFieldName("type"), source, structTypes)
	}
	if concreteType == "" {
		return
	}
	for _, name := range names {
		variableTypes[name] = concreteType
	}
}

func goRecordLocalAssignmentTypes(
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
	constructorReturns map[string]string,
	variableTypes map[string]string,
) {
	leftNames := IdentifierNames(node.ChildByFieldName("left"), source)
	concreteType := ConcreteTypeFromExpression(UnwrapSingleExpression(node.ChildByFieldName("right")), source, structTypes)
	if concreteType == "" {
		concreteType = goConcreteTypeFromConstructorCall(
			UnwrapSingleExpression(node.ChildByFieldName("right")),
			source,
			structTypes,
			constructorReturns,
		)
	}
	if concreteType == "" {
		return
	}
	for _, name := range leftNames {
		variableTypes[strings.ToLower(name)] = concreteType
	}
}
