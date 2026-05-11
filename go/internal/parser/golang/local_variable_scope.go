package golang

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goKnownLocalPackageVariableTypes(
	root *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
	constructorReturns map[string]string,
	lookup *goParentLookup,
) map[string]string {
	variableTypes := make(map[string]string)
	walkNamed(root, func(node *tree_sitter.Node) {
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
