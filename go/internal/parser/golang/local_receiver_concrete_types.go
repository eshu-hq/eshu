// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goLocalInterfaceNames(root *tree_sitter.Node, source []byte) map[string]struct{} {
	names := make(map[string]struct{})
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "type_spec" {
			return
		}
		typeNode := node.ChildByFieldName("type")
		if typeNode == nil || typeNode.Kind() != "interface_type" {
			return
		}
		name := goNormalizeTypeName(nodeText(node.ChildByFieldName("name"), source))
		if name != "" {
			names[strings.ToLower(name)] = struct{}{}
		}
	})
	return names
}

func goTypeNameIsLocalInterface(typeName string, localInterfaces map[string]struct{}) bool {
	if typeName == "" || len(localInterfaces) == 0 {
		return false
	}
	_, ok := localInterfaces[strings.ToLower(goNormalizeTypeName(typeName))]
	return ok
}

func goConcreteReceiverTypeFromExpression(
	node *tree_sitter.Node,
	source []byte,
	constructorReturns map[string]string,
) string {
	node = goUnwrapSingleExpression(node)
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "call_expression":
		return goConstructorTypeFromExpression(node, source, constructorReturns)
	case "composite_literal":
		return goNormalizeTypeName(goCompositeLiteralTypeName(node.ChildByFieldName("type"), source))
	case "expression_list", "literal_element", "parenthesized_expression", "unary_expression":
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			child := child
			if typeName := goConcreteReceiverTypeFromExpression(&child, source, constructorReturns); typeName != "" {
				return typeName
			}
		}
	}
	return ""
}

func goAssignableIdentifierNodes(node *tree_sitter.Node, source []byte) []*tree_sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() == "identifier" && strings.TrimSpace(nodeText(node, source)) != "_" {
		return []*tree_sitter.Node{node}
	}
	if node.Kind() != "expression_list" && node.Kind() != "parameter_list" {
		return nil
	}
	nodes := make([]*tree_sitter.Node, 0)
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != "identifier" || strings.TrimSpace(nodeText(&child, source)) == "_" {
			continue
		}
		nodes = append(nodes, &child)
	}
	return nodes
}
