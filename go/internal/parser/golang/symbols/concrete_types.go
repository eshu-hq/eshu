// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// ConcreteTypeFromExpression returns the lower-cased struct type name a
// composite_literal expression (or an expression that unwraps to one) names,
// restricted to names present in structTypes.
func ConcreteTypeFromExpression(
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier":
		return ""
	case "composite_literal":
		return ConcreteTypeFromTypeNode(node.ChildByFieldName("type"), source, structTypes)
	case "expression_list", "literal_element", "parenthesized_expression", "unary_expression":
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			if concreteType := ConcreteTypeFromExpression(&child, source, structTypes); concreteType != "" {
				return concreteType
			}
		}
	}
	return ""
}

// ConcreteTypeFromTypeNode returns the lower-cased struct type name a type
// node (or the first type_identifier reachable under it) names, restricted to
// names present in structTypes.
func ConcreteTypeFromTypeNode(
	node *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
) string {
	if node == nil {
		return ""
	}
	name := ""
	if node.Kind() == "type_identifier" {
		name = strings.ToLower(strings.TrimSpace(shared.NodeText(node, source)))
	} else {
		shared.WalkNamed(node, func(child *tree_sitter.Node) {
			if name != "" || child.Kind() != "type_identifier" {
				return
			}
			name = strings.ToLower(strings.TrimSpace(shared.NodeText(child, source)))
		})
	}
	if _, ok := structTypes[name]; ok {
		return name
	}
	return ""
}

// CompositeLiteralTypeName returns the type_identifier text under a composite
// literal's type node.
func CompositeLiteralTypeName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if node.Kind() == "type_identifier" {
		return shared.NodeText(node, source)
	}
	nameNode := FirstNamedDescendant(node, "type_identifier")
	return shared.NodeText(nameNode, source)
}

// EnclosingMethodReceiver returns the receiver name and type of the nearest
// enclosing method_declaration for callNode, using lookup for O(depth)
// ancestor traversal.
func EnclosingMethodReceiver(callNode *tree_sitter.Node, source []byte, lookup *ParentLookup) (string, string) {
	for current := callNode; current != nil; current = lookup.Parent(current) {
		if current.Kind() != "method_declaration" {
			continue
		}
		return MethodReceiverBinding(current, source)
	}
	return "", ""
}

// MethodReceiverBinding returns the receiver parameter's name and normalized
// type for one method_declaration node.
func MethodReceiverBinding(node *tree_sitter.Node, source []byte) (string, string) {
	if node == nil {
		return "", ""
	}

	receiver := node.ChildByFieldName("receiver")
	if receiver == nil {
		return "", ""
	}

	cursor := receiver.Walk()
	defer cursor.Close()
	for _, child := range receiver.NamedChildren(cursor) {
		child := child
		if child.Kind() != "parameter_declaration" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		receiverName := strings.TrimSpace(shared.NodeText(nameNode, source))
		receiverType := ReceiverContext(node, source)
		if receiverName != "" || receiverType != "" {
			return receiverName, receiverType
		}
	}

	receiverType := ReceiverContext(node, source)
	if receiverType == "" {
		return "", ""
	}
	nameNode := FirstNamedDescendant(receiver, "identifier")
	return strings.TrimSpace(shared.NodeText(nameNode, source)), receiverType
}
