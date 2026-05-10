package golang

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goKnownLocalVariableTypesForNode(
	root *tree_sitter.Node,
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
	constructorReturns map[string]string,
) map[string]string {
	variableTypes := goKnownLocalPackageVariableTypes(root, source, structTypes, constructorReturns)
	scope := goEnclosingFunctionScope(node)
	if scope == nil {
		return variableTypes
	}
	walkNamed(scope, func(child *tree_sitter.Node) {
		if child.StartByte() > node.StartByte() {
			return
		}
		switch child.Kind() {
		case "function_declaration", "method_declaration", "func_literal":
			if child.StartByte() == scope.StartByte() {
				goRecordLocalParameterTypes(child, source, structTypes, variableTypes)
			}
		case "var_spec":
			goRecordLocalVarSpecTypes(child, source, structTypes, constructorReturns, variableTypes)
		case "short_var_declaration", "assignment_statement":
			goRecordLocalAssignmentTypes(child, source, structTypes, constructorReturns, variableTypes)
		}
	})
	return variableTypes
}

func goKnownLocalPackageVariableTypes(
	root *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
	constructorReturns map[string]string,
) map[string]string {
	variableTypes := make(map[string]string)
	walkNamed(root, func(node *tree_sitter.Node) {
		if goEnclosingFunctionScope(node) != nil {
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
	names := goIdentifierNames(node.ChildByFieldName("name"), source)
	concreteType := goConcreteTypeFromExpression(node.ChildByFieldName("value"), source, structTypes)
	if concreteType == "" {
		concreteType = goConcreteTypeFromConstructorCall(
			node.ChildByFieldName("value"),
			source,
			structTypes,
			constructorReturns,
		)
	}
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

func goRecordLocalAssignmentTypes(
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
	constructorReturns map[string]string,
	variableTypes map[string]string,
) {
	leftNames := goIdentifierNames(node.ChildByFieldName("left"), source)
	concreteType := goConcreteTypeFromExpression(goUnwrapSingleExpression(node.ChildByFieldName("right")), source, structTypes)
	if concreteType == "" {
		concreteType = goConcreteTypeFromConstructorCall(
			goUnwrapSingleExpression(node.ChildByFieldName("right")),
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
