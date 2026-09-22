// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// FirstNamedDescendant returns the first named descendant of node (in
// shared.WalkNamed pre-order) whose Kind() matches one of kinds, or nil when
// none does. The returned node is a shared.CloneNode copy, safe to retain
// past the walk that found it.
func FirstNamedDescendant(node *tree_sitter.Node, kinds ...string) *tree_sitter.Node {
	var result *tree_sitter.Node
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if result != nil {
			return
		}
		for _, kind := range kinds {
			if child.Kind() == kind {
				result = shared.CloneNode(child)
				return
			}
		}
	})
	return result
}

// WalkScopeBindings visits scope and its named descendants but does not
// descend into nested function_declaration, method_declaration, or func_literal
// subtrees. The walker preserves Go lexical scoping for variable-type indices:
// a `var x = ...` inside an inner closure must not leak into the outer
// function's binding table, otherwise call-expression metadata in the outer
// scope would inherit shadowed identifiers from a body that never executes
// there. Visited nodes are passed by pointer; callers that retain a binding
// must copy the node value, because the underlying *tree_sitter.Node points
// at a stack-allocated local inside the recursive walk.
func WalkScopeBindings(scope *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	if scope == nil {
		return
	}
	var walk func(*tree_sitter.Node)
	walk = func(current *tree_sitter.Node) {
		if current == nil {
			return
		}
		visit(current)
		cursor := current.Walk()
		defer cursor.Close()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			if isNestedDefinition(child.Kind()) {
				continue
			}
			walk(&child)
		}
	}
	walk(scope)
}

func isNestedDefinition(kind string) bool {
	switch kind {
	case "function_declaration", "method_declaration", "func_literal":
		return true
	default:
		return false
	}
}

// WalkDirectNamed visits node's direct named children only (no recursion into
// grandchildren). Callers use it to enumerate one syntax level — a parameter
// list's parameter_declaration children, an argument_list's arguments — without
// walking into nested expressions. Visited nodes are passed by pointer; a
// caller that retains a binding must copy the node value, because the
// underlying *tree_sitter.Node points at a stack-allocated local inside this
// walk.
func WalkDirectNamed(node *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	if node == nil {
		return
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		visit(&child)
	}
}
