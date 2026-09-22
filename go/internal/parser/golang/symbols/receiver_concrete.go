// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// goCollectLocalInterfaceName records the lower-cased local interface name
// for one type_spec node into names, if any. It is the single-node visitor
// shared by goCollectLocalMapValueTypesAndInterfaceNames (the merged walk
// used by LocalReceiverBindings; see #4839), which replaced the former
// standalone goLocalInterfaceNames full-tree walk.
func goCollectLocalInterfaceName(node *tree_sitter.Node, source []byte, names map[string]struct{}) {
	if node.Kind() != "type_spec" {
		return
	}
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil || typeNode.Kind() != "interface_type" {
		return
	}
	name := NormalizeTypeName(shared.NodeText(node.ChildByFieldName("name"), source))
	if name != "" {
		names[strings.ToLower(name)] = struct{}{}
	}
}

func goTypeNameIsLocalInterface(typeName string, localInterfaces map[string]struct{}) bool {
	if typeName == "" || len(localInterfaces) == 0 {
		return false
	}
	_, ok := localInterfaces[strings.ToLower(NormalizeTypeName(typeName))]
	return ok
}

func goConcreteReceiverTypeFromExpression(
	node *tree_sitter.Node,
	source []byte,
	constructorReturns map[string]string,
) string {
	node = UnwrapSingleExpression(node)
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "call_expression":
		return goConstructorTypeFromExpression(node, source, constructorReturns)
	case "composite_literal":
		return NormalizeTypeName(CompositeLiteralTypeName(node.ChildByFieldName("type"), source))
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

// AssignableIdentifierNodes returns node itself when it is a non-blank
// identifier, or each non-blank identifier among its direct named children
// when node is an expression_list or parameter_list, restricted to
// assignment-target positions.
func AssignableIdentifierNodes(node *tree_sitter.Node, source []byte) []*tree_sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() == "identifier" && strings.TrimSpace(shared.NodeText(node, source)) != "_" {
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
		if child.Kind() != "identifier" || strings.TrimSpace(shared.NodeText(&child, source)) == "_" {
			continue
		}
		nodes = append(nodes, &child)
	}
	return nodes
}
