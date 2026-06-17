package jsdataflow

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// paramNames returns the identifier parameter names of a function, in
// declaration order. Object and array destructuring patterns are skipped (v1).
func paramNames(node *tree_sitter.Node, source []byte) []string {
	params := node.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	var names []string
	cursor := params.Walk()
	defer cursor.Close()
	for _, decl := range params.NamedChildren(cursor) {
		decl := decl
		switch decl.Kind() {
		case "required_parameter", "optional_parameter":
			if pattern := decl.ChildByFieldName("pattern"); pattern != nil && pattern.Kind() == "identifier" {
				if name := nodeText(pattern, source); name != "" {
					names = append(names, name)
				}
			}
		case "identifier":
			if name := nodeText(&decl, source); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

// assignDefsUses splits an assignment or update expression into defined and used
// bindings. A plain identifier target is a definition; a member or subscript
// target reads its base (property flow is modeled later). An augmented assignment
// (+=) or an update (x++) also reads the target.
func assignDefsUses(node *tree_sitter.Node, source []byte) (defs, uses []string) {
	switch node.Kind() {
	case "assignment_expression":
		left := node.ChildByFieldName("left")
		if left != nil && left.Kind() == "identifier" {
			defs = append(defs, nodeText(left, source))
		} else if left != nil {
			uses = append(uses, exprUses(left, source)...)
		}
		if right := node.ChildByFieldName("right"); right != nil {
			uses = append(uses, exprUses(right, source)...)
		}
	case "augmented_assignment_expression":
		left := node.ChildByFieldName("left")
		if left != nil && left.Kind() == "identifier" {
			name := nodeText(left, source)
			defs = append(defs, name)
			uses = append(uses, name)
		} else if left != nil {
			uses = append(uses, exprUses(left, source)...)
		}
		if right := node.ChildByFieldName("right"); right != nil {
			uses = append(uses, exprUses(right, source)...)
		}
	case "update_expression":
		if arg := node.ChildByFieldName("argument"); arg != nil && arg.Kind() == "identifier" {
			name := nodeText(arg, source)
			defs = append(defs, name)
			uses = append(uses, name)
		}
	}
	return defs, uses
}

// exprUses returns the identifier names read within an expression subtree. It
// does not descend into nested function or arrow-function bodies, so a closure's
// captured variables are not attributed to the enclosing function (closures are
// modeled later).
func exprUses(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	var uses []string
	var visit func(*tree_sitter.Node)
	visit = func(current *tree_sitter.Node) {
		if current == nil || isNestedFunction(current.Kind()) {
			return
		}
		if current.Kind() == "identifier" {
			if name := nodeText(current, source); name != "" {
				uses = append(uses, name)
			}
		}
		cursor := current.Walk()
		defer cursor.Close()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			visit(&child)
		}
	}
	visit(node)
	return uses
}

// isNestedFunction reports whether a node kind starts a nested function scope
// that must not be descended into for the enclosing function's uses.
func isNestedFunction(kind string) bool {
	switch kind {
	case "function", "function_declaration", "function_expression",
		"arrow_function", "generator_function", "generator_function_declaration",
		"method_definition":
		return true
	default:
		return false
	}
}
