// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// pythonImportContext reports whether an import statement is type-only or
// deferred, from the statements that enclose it (issue #7344).
//
// A statement is type-only when it sits in the consequence of an `if` or `elif`
// guarded by TYPE_CHECKING: the branch never runs, so the import cannot close a
// runtime cycle. The `else` branch of that guard, and a negated guard, are not
// type-only. A statement is deferred when a function or lambda body encloses
// it, because it then runs at call time instead of at module load. A class body
// runs at definition time, so it is neither. Both flags can hold at once, and
// the walk keeps climbing past a function so a guard outside it still counts.
func pythonImportContext(node *tree_sitter.Node, source []byte) (typeOnly bool, deferred bool) {
	child := node
	for current := node.Parent(); current != nil; child, current = current, current.Parent() {
		switch current.Kind() {
		case "function_definition", "lambda":
			deferred = true
		case "if_statement", "elif_clause":
			consequence := current.ChildByFieldName("consequence")
			if consequence == nil || consequence.Id() != child.Id() {
				continue
			}
			if pythonIsTypeCheckingGuard(current.ChildByFieldName("condition"), source) {
				typeOnly = true
			}
		}
	}
	return typeOnly, deferred
}

// pythonIsTypeCheckingGuard reports whether a condition is the bare name
// TYPE_CHECKING or an attribute access ending in it (typing.TYPE_CHECKING).
// Anything composite, such as `not TYPE_CHECKING`, is not a type-only guard.
func pythonIsTypeCheckingGuard(condition *tree_sitter.Node, source []byte) bool {
	if condition == nil {
		return false
	}
	switch condition.Kind() {
	case "identifier":
		return nodeText(condition, source) == "TYPE_CHECKING"
	case "attribute":
		return nodeText(condition.ChildByFieldName("attribute"), source) == "TYPE_CHECKING"
	default:
		return false
	}
}
