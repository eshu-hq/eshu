// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/golang/symbols"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goFunctionValueReferenceCalls(
	root *tree_sitter.Node,
	source []byte,
	localNameBindings []symbols.LocalNameBinding,
	lookup *symbols.ParentLookup,
) []map[string]any {
	if root == nil {
		return nil
	}

	calls := make([]map[string]any, 0)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "identifier" || !goFunctionValueReferenceContext(node, lookup) {
			return
		}
		name := strings.TrimSpace(nodeText(node, source))
		if name == "" || name == "_" {
			return
		}
		if symbols.NameIsLocallyBound(name, nodeLine(node), localNameBindings) {
			return
		}
		calls = append(calls, map[string]any{
			"name":        name,
			"full_name":   name,
			"line_number": nodeLine(node),
			"call_kind":   "go.function_value_reference",
			"lang":        "go",
		})
	})
	return calls
}

// goFunctionValueReferenceContext walks ancestors via the per-parse parent
// lookup so the classification of each identifier costs O(depth) per call
// instead of O(depth^2); see #161.
func goFunctionValueReferenceContext(node *tree_sitter.Node, lookup *symbols.ParentLookup) bool {
	parent := lookup.Parent(node)
	if parent == nil {
		return false
	}
	switch parent.Kind() {
	case "call_expression":
		return false
	case "argument_list":
		return true
	case "selector_expression", "qualified_type", "field_declaration", "parameter_declaration":
		return false
	case "short_var_declaration", "assignment_statement", "var_spec":
		return goNodeMatchesField(parent, node, "right", lookup) || goNodeMatchesField(parent, node, "value", lookup)
	case "literal_element":
		return true
	case "keyed_element":
		return goNodeMatchesField(parent, node, "value", lookup)
	default:
		return goFunctionValueReferenceContext(parent, lookup)
	}
}

func goNodeMatchesField(parent *tree_sitter.Node, child *tree_sitter.Node, fieldName string, lookup *symbols.ParentLookup) bool {
	if parent == nil || child == nil {
		return false
	}
	fieldNode := parent.ChildByFieldName(fieldName)
	if fieldNode == nil {
		return false
	}
	if goSameNodeRange(fieldNode, child) {
		return true
	}
	for current := lookup.Parent(child); current != nil; current = lookup.Parent(current) {
		if goSameNodeRange(fieldNode, current) {
			return true
		}
		if goSameNodeRange(current, parent) {
			break
		}
	}
	return false
}

func goSameNodeRange(left *tree_sitter.Node, right *tree_sitter.Node) bool {
	if left == nil || right == nil {
		return false
	}
	return left.Kind() == right.Kind() &&
		left.StartByte() == right.StartByte() &&
		left.EndByte() == right.EndByte()
}
