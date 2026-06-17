package pydataflow

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// paramNames returns the identifier parameter names of a function definition, in
// declaration order. *args/**kwargs splats and complex patterns are skipped (v1).
func paramNames(node *tree_sitter.Node, source []byte) []string {
	params := node.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	var names []string
	cursor := params.Walk()
	defer cursor.Close()
	for _, param := range params.NamedChildren(cursor) {
		param := param
		switch param.Kind() {
		case "identifier":
			if name := nodeText(&param, source); name != "" {
				names = append(names, name)
			}
		case "default_parameter", "typed_default_parameter":
			if nameNode := param.ChildByFieldName("name"); nameNode != nil && nameNode.Kind() == "identifier" {
				names = append(names, nodeText(nameNode, source))
			}
		case "typed_parameter":
			if id := firstIdentifier(&param, source); id != "" {
				names = append(names, id)
			}
		}
	}
	return names
}

// firstIdentifier returns the text of the first direct identifier child.
func firstIdentifier(node *tree_sitter.Node, source []byte) string {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "identifier" {
			return nodeText(&child, source)
		}
	}
	return ""
}

// assignDefsUses splits a Python assignment or augmented assignment into defined
// and used bindings. A plain identifier (or a tuple/list of identifiers) target
// is a definition; an attribute or subscript target reads its base (attribute and
// element flow are modeled later). An augmented assignment (+=) also reads the
// target.
func assignDefsUses(node *tree_sitter.Node, source []byte) (defs, uses []string) {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	switch node.Kind() {
	case "assignment":
		defs = append(defs, assignTargets(left, source)...)
		uses = append(uses, targetBaseUses(left, source)...)
	case "augmented_assignment":
		targets := assignTargets(left, source)
		defs = append(defs, targets...)
		uses = append(uses, targets...) // augmented assignment reads its target
		uses = append(uses, targetBaseUses(left, source)...)
	}
	if right != nil {
		uses = append(uses, exprUses(right, source)...)
	}
	return defs, uses
}

// assignTargets returns the identifier definition targets of an assignment left
// side: a bare identifier, or the identifiers of a tuple/list pattern. Attribute
// and subscript targets define nothing (their base is read, see targetBaseUses).
func assignTargets(left *tree_sitter.Node, source []byte) []string {
	if left == nil {
		return nil
	}
	switch left.Kind() {
	case "identifier":
		return []string{nodeText(left, source)}
	case "pattern_list", "tuple_pattern", "list_pattern":
		var targets []string
		cursor := left.Walk()
		defer cursor.Close()
		for _, child := range left.NamedChildren(cursor) {
			if child.Kind() == "identifier" {
				targets = append(targets, nodeText(&child, source))
			}
		}
		return targets
	default:
		return nil
	}
}

// targetBaseUses returns the bindings read by a non-identifier assignment target
// (the base of an attribute or subscript target), which is a use, not a def.
func targetBaseUses(left *tree_sitter.Node, source []byte) []string {
	if left == nil {
		return nil
	}
	switch left.Kind() {
	case "identifier", "pattern_list", "tuple_pattern", "list_pattern":
		return nil
	default:
		return exprUses(left, source)
	}
}

// exprUses returns the identifier names read within an expression subtree. It
// does not descend into nested function definitions or lambdas, so a closure's
// captured variables are not attributed to the enclosing function. For an
// attribute access (a.b) only the object (a) is a use; the attribute name (b) is
// not a variable.
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
		if current.Kind() == "attribute" {
			if obj := current.ChildByFieldName("object"); obj != nil {
				visit(obj)
			}
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
	case "function_definition", "lambda":
		return true
	default:
		return false
	}
}
