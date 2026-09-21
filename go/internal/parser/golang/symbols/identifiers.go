// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// IdentifierNames returns the lower-cased identifier, field-identifier, or
// type-identifier text found anywhere under node.
func IdentifierNames(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "field_identifier", "identifier", "type_identifier":
		name := strings.TrimSpace(shared.NodeText(node, source))
		if name == "" {
			return nil
		}
		return []string{strings.ToLower(name)}
	default:
		cursor := node.Walk()
		defer cursor.Close()
		values := make([]string, 0)
		for _, child := range node.NamedChildren(cursor) {
			values = append(values, IdentifierNames(&child, source)...)
		}
		return values
	}
}

// SelectorBaseAndField splits a selector_expression node into its operand
// text and field text. ok is false when node is not a selector_expression or
// has no field.
func SelectorBaseAndField(node *tree_sitter.Node, source []byte) (string, string, bool) {
	if node == nil || node.Kind() != "selector_expression" {
		return "", "", false
	}
	fieldNode := node.ChildByFieldName("field")
	if fieldNode == nil {
		return "", "", false
	}
	baseNode := node.ChildByFieldName("operand")
	if baseNode == nil {
		cursor := node.Walk()
		defer cursor.Close()
		children := node.NamedChildren(cursor)
		if len(children) == 0 {
			return "", "", false
		}
		baseNode = &children[0]
	}
	return strings.TrimSpace(shared.NodeText(baseNode, source)), strings.TrimSpace(shared.NodeText(fieldNode, source)), true
}

// UnwrapSingleExpression returns node's sole child when node is an
// expression_list with exactly one named child, and node otherwise.
func UnwrapSingleExpression(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() != "expression_list" {
		return node
	}
	cursor := node.Walk()
	defer cursor.Close()
	children := node.NamedChildren(cursor)
	if len(children) != 1 {
		return node
	}
	return &children[0]
}

// NameIsLocallyBound reports whether name is shadowed at line by a local
// binding in the same lexical scope.
func NameIsLocallyBound(name string, line int, bindings []LocalNameBinding) bool {
	name = strings.TrimSpace(name)
	if name == "" || line <= 0 {
		return false
	}
	for _, binding := range bindings {
		if binding.variable != name ||
			binding.line > line ||
			line < binding.scopeStart ||
			line > binding.scopeEnd {
			continue
		}
		return true
	}
	return false
}

// FunctionLiteralIsCompositeElement keeps registry-style function literals as
// reachability evidence without treating assigned local closures as roots. The
// lookup parameter is required so the ancestor walk is O(depth) instead of
// O(depth^2) via tree-sitter's root-walking Node.Parent(); see #161.
func FunctionLiteralIsCompositeElement(node *tree_sitter.Node, lookup *ParentLookup) bool {
	for current := lookup.Parent(node); current != nil; current = lookup.Parent(current) {
		switch current.Kind() {
		case "composite_literal":
			return true
		case "function_declaration", "method_declaration", "func_literal", "short_var_declaration", "assignment_statement", "var_spec":
			return false
		}
	}
	return false
}
