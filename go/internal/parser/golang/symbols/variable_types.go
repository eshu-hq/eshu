// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goLocalParameterTypes(
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
) map[string]string {
	types := make(map[string]string)
	params := node.ChildByFieldName("parameters")
	WalkDirectNamed(params, func(param *tree_sitter.Node) {
		if param.Kind() != "parameter_declaration" {
			return
		}
		concreteType := ConcreteTypeFromTypeNode(param.ChildByFieldName("type"), source, structTypes)
		if concreteType == "" {
			return
		}
		for _, name := range IdentifierNames(param.ChildByFieldName("name"), source) {
			types[name] = concreteType
		}
	})
	return types
}

func goConcreteTypeFromConstructorCall(
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
	constructorReturns map[string]string,
) string {
	if node == nil || node.Kind() != "call_expression" {
		return ""
	}
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil || functionNode.Kind() != "identifier" {
		return ""
	}
	constructorName := strings.TrimSpace(shared.NodeText(functionNode, source))
	typeName := strings.TrimSpace(constructorReturns[constructorName])
	if typeName == "" {
		typeName = strings.TrimSpace(constructorReturns[strings.ToLower(constructorName)])
	}
	typeName = strings.ToLower(NormalizeTypeName(typeName))
	if _, ok := structTypes[typeName]; ok {
		return typeName
	}
	return ""
}

func goKnownImportedVariableTypes(
	root *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	_ *ParentLookup,
) map[string]string {
	variableTypes := make(map[string]string)
	walkPackageScopeImportedVariableDeclarations(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "var_spec":
			goRecordImportedVarSpecTypes(node, source, importAliases, variableTypes)
		case "short_var_declaration", "assignment_statement":
			goRecordImportedAssignmentTypes(node, source, importAliases, variableTypes, nil, node.StartByte())
		}
	})
	return variableTypes
}

// walkPackageScopeImportedVariableDeclarations visits named package-scope
// declarations while skipping function bodies. Package-scope imported-variable
// discovery only needs top-level declarations, so asking every node whether it
// has an enclosing function burns ancestor checks on large Go repos.
func walkPackageScopeImportedVariableDeclarations(root *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	if root == nil {
		return
	}
	var walk func(*tree_sitter.Node)
	walk = func(current *tree_sitter.Node) {
		if current == nil {
			return
		}
		if current != root && isNestedDefinition(current.Kind()) {
			return
		}
		visit(current)
		cursor := current.Walk()
		defer cursor.Close()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			walk(&child)
		}
	}
	walk(root)
}

func goRecordImportedParameterTypes(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	variableTypes map[string]string,
) {
	typeName := goReceiverTypeFromAnyTypeNode(node.ChildByFieldName("type"), source, importAliases)
	if typeName == "" {
		return
	}
	for _, name := range IdentifierNames(node.ChildByFieldName("name"), source) {
		variableTypes[name] = typeName
	}
}

func goRecordImportedVarSpecTypes(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	variableTypes map[string]string,
) {
	typeName := goReceiverTypeFromAnyTypeNode(node.ChildByFieldName("type"), source, importAliases)
	if typeName == "" {
		typeName = goImportedTypeFromExpression(node.ChildByFieldName("value"), source, importAliases, nil, nil)
	}
	if typeName == "" {
		return
	}
	for _, name := range IdentifierNames(node.ChildByFieldName("name"), source) {
		variableTypes[name] = typeName
	}
}

func goRecordImportedAssignmentTypes(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	variableTypes map[string]string,
	interfaceMethodReturns map[string]string,
	maxStartByte uint,
) {
	if node.StartByte() > maxStartByte {
		return
	}
	typeName := goImportedTypeFromExpression(node.ChildByFieldName("right"), source, importAliases, variableTypes, interfaceMethodReturns)
	if typeName == "" {
		return
	}
	for _, name := range IdentifierNames(node.ChildByFieldName("left"), source) {
		variableTypes[name] = typeName
	}
}

// ImportedDirectMethodCallKey returns the "<receiver type>.<method>" key for
// a call whose receiver type resolves through variableTypes or
// interfaceMethodReturns, or "" when the receiver type cannot be resolved.
func ImportedDirectMethodCallKey(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	variableTypes map[string]string,
	interfaceMethodReturns map[string]string,
) string {
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil || functionNode.Kind() != "selector_expression" {
		return ""
	}
	methodName := strings.ToLower(strings.TrimSpace(shared.NodeText(functionNode.ChildByFieldName("field"), source)))
	if methodName == "" {
		_, field, ok := SelectorBaseAndField(functionNode, source)
		if !ok {
			return ""
		}
		methodName = strings.ToLower(strings.TrimSpace(field))
	}
	receiverNode := functionNode.ChildByFieldName("operand")
	if receiverNode == nil {
		cursor := functionNode.Walk()
		defer cursor.Close()
		children := functionNode.NamedChildren(cursor)
		if len(children) == 0 {
			return ""
		}
		receiverNode = &children[0]
	}
	receiverType := ""
	if receiverNode.Kind() == "identifier" {
		receiverType = variableTypes[strings.ToLower(strings.TrimSpace(shared.NodeText(receiverNode, source)))]
	} else {
		receiverType = goImportedTypeFromExpression(receiverNode, source, importAliases, variableTypes, interfaceMethodReturns)
	}
	if receiverType == "" {
		return ""
	}
	return receiverType + "." + methodName
}

// ImportedFmtStringerCallKeys returns "<type>.string" keys for the value
// arguments of a recognized fmt-formatting call whose resolved type contains
// a package qualifier, for matching against known Stringer implementations.
func ImportedFmtStringerCallKeys(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	variableTypes map[string]string,
	interfaceMethodReturns map[string]string,
) []string {
	if !CallIsFmtFormatting(node, source, importAliases) {
		return nil
	}
	keys := make([]string, 0)
	firstValueArg := FmtStringerFirstValueArgIndex(node, source, importAliases)
	for index, arg := range CallArgumentNodes(node) {
		if index < firstValueArg {
			continue
		}
		typeName := goImportedTypeFromExpression(arg, source, importAliases, variableTypes, interfaceMethodReturns)
		if typeName == "" && arg.Kind() == "identifier" {
			typeName = variableTypes[strings.ToLower(strings.TrimSpace(shared.NodeText(arg, source)))]
		}
		if typeName != "" && strings.Contains(typeName, ".") {
			keys = AppendUniqueImportAlias(keys, typeName+".string")
		}
	}
	return keys
}

func goImportedTypeFromExpression(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	variableTypes map[string]string,
	interfaceMethodReturns map[string]string,
) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier":
		if variableTypes != nil {
			return variableTypes[strings.ToLower(strings.TrimSpace(shared.NodeText(node, source)))]
		}
	case "composite_literal":
		return goImportedTypeFromTypeNode(node.ChildByFieldName("type"), source, importAliases)
	case "expression_list", "literal_element", "parenthesized_expression", "unary_expression":
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			child := child
			if typeName := goImportedTypeFromExpression(&child, source, importAliases, variableTypes, interfaceMethodReturns); typeName != "" {
				return typeName
			}
		}
	case "call_expression":
		functionNode := node.ChildByFieldName("function")
		if functionNode != nil && functionNode.Kind() == "selector_expression" {
			if typeName := goImportedTypeFromReceiverMethodCall(functionNode, source, variableTypes, interfaceMethodReturns); typeName != "" {
				return typeName
			}
			return goImportedTypeFromSelectorOperand(functionNode.ChildByFieldName("operand"), source, importAliases)
		}
	}
	return ""
}

func goImportedTypeFromReceiverMethodCall(
	functionNode *tree_sitter.Node,
	source []byte,
	variableTypes map[string]string,
	interfaceMethodReturns map[string]string,
) string {
	if variableTypes == nil || interfaceMethodReturns == nil {
		return ""
	}
	receiver, methodName, ok := SelectorBaseAndField(functionNode, source)
	if !ok {
		return ""
	}
	receiverType := variableTypes[strings.ToLower(strings.TrimSpace(receiver))]
	if receiverType == "" {
		return ""
	}
	return interfaceMethodReturns[receiverType+"."+strings.ToLower(strings.TrimSpace(methodName))]
}

func goImportedTypeFromTypeNode(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
) string {
	if node == nil {
		return ""
	}
	if typeName := goImportedTypeFromSelectorOperand(node, source, importAliases); typeName != "" {
		return typeName
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if typeName := goImportedTypeFromTypeNode(&child, source, importAliases); typeName != "" {
			return typeName
		}
	}
	return ""
}

func goImportedTypeFromSelectorOperand(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
) string {
	if node == nil {
		return ""
	}
	var base, field string
	if node.Kind() == "selector_expression" {
		var ok bool
		base, field, ok = SelectorBaseAndField(node, source)
		if !ok {
			return ""
		}
	} else if node.Kind() == "qualified_type" {
		parts := strings.Split(strings.TrimSpace(shared.NodeText(node, source)), ".")
		if len(parts) < 2 {
			return ""
		}
		base = parts[0]
		field = parts[len(parts)-1]
	} else {
		return ""
	}
	importPath := ImportPathForAlias(base, importAliases)
	if importPath == "" {
		return ""
	}
	return strings.ToLower(importPath + "." + NormalizeTypeName(field))
}

func goReceiverTypeFromAnyTypeNode(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
) string {
	if typeName := goImportedTypeFromTypeNode(node, source, importAliases); typeName != "" {
		return typeName
	}
	return strings.ToLower(NormalizeTypeName(shared.NodeText(node, source)))
}

// LocalInterfaceImportedMethodReturns maps "<local interface name>.<method>"
// to the imported receiver type each package-local interface method's
// declared result type resolves to, for interfaces whose method results name
// an imported type.
func LocalInterfaceImportedMethodReturns(
	root *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
) map[string]string {
	returns := make(map[string]string)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "type_spec" {
			return
		}
		interfaceName := strings.ToLower(strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source)))
		typeNode := node.ChildByFieldName("type")
		if interfaceName == "" || typeNode == nil || typeNode.Kind() != "interface_type" {
			return
		}
		shared.WalkNamed(typeNode, func(child *tree_sitter.Node) {
			if child.Kind() != "method_elem" {
				return
			}
			methodName := strings.ToLower(strings.TrimSpace(shared.NodeText(child.ChildByFieldName("name"), source)))
			returnType := goImportedTypeFromTypeNode(child.ChildByFieldName("result"), source, importAliases)
			if methodName != "" && returnType != "" {
				returns[interfaceName+"."+methodName] = returnType
			}
		})
	})
	return returns
}

// ImportPathForAlias returns the import path that binds alias in
// importAliases, or "" when no import path does. When several paths bind one
// alias, it returns the lexicographically smallest so the resolution stays
// deterministic run to run (issue #6947).
func ImportPathForAlias(alias string, importAliases map[string][]string) string {
	if paths := ImportPathsForAlias(alias, importAliases); len(paths) > 0 {
		return paths[0]
	}
	return ""
}
