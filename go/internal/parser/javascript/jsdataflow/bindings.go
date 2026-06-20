package jsdataflow

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// paramNames returns the identifier parameter names of a function, in
// declaration order. Object and array destructuring patterns are skipped (v1).
func paramNames(node *tree_sitter.Node, source []byte) []string {
	// An unparenthesized single-parameter arrow function (req => ...) carries the
	// parameter under the singular `parameter` field as a bare identifier.
	if single := node.ChildByFieldName("parameter"); single != nil && single.Kind() == "identifier" {
		if name := nodeText(single, source); name != "" {
			return []string{name}
		}
	}
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

// forInTargets returns the identifier definition targets of a for-in/for-of left
// side: a bare identifier, or the identifiers of an array/object destructuring
// pattern. The grammar's loop binding (const/let/var) appears as a separate
// keyword token, so the left field is the binding pattern itself.
func forInTargets(left *tree_sitter.Node, source []byte) []string {
	if left == nil {
		return nil
	}
	if left.Kind() == "identifier" {
		if name := nodeText(left, source); name != "" {
			return []string{name}
		}
		return nil
	}
	// array_pattern / object_pattern: collect the bound identifiers.
	var targets []string
	var visit func(*tree_sitter.Node)
	visit = func(current *tree_sitter.Node) {
		if current == nil {
			return
		}
		if current.Kind() == "identifier" {
			if name := nodeText(current, source); name != "" {
				targets = append(targets, name)
			}
			return
		}
		cursor := current.Walk()
		defer cursor.Close()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			visit(&child)
		}
	}
	visit(left)
	return targets
}

// assignDefsUsesWithOptions splits an assignment or update expression into
// defined and used bindings. A plain identifier target is a definition; a
// member/subscript target is a field-sensitive access-path definition. An
// augmented assignment (+=) or an update (x++) also reads the target. A target
// that is not a precise access path (for example a destructuring pattern) reads
// its components but defines nothing in this field-sensitive pass.
func assignDefsUsesWithOptions(node *tree_sitter.Node, source []byte, aliases jsBindingAliases, options jsAccessPathOptions) (defs, uses []string) {
	switch node.Kind() {
	case "assignment_expression":
		defs, uses = targetDefUses(node.ChildByFieldName("left"), source, aliases, options, false)
		if right := node.ChildByFieldName("right"); right != nil {
			uses = append(uses, exprUsesWithOptions(right, source, aliases, options)...)
		}
	case "augmented_assignment_expression":
		defs, uses = targetDefUses(node.ChildByFieldName("left"), source, aliases, options, true)
		if right := node.ChildByFieldName("right"); right != nil {
			uses = append(uses, exprUsesWithOptions(right, source, aliases, options)...)
		}
	case "update_expression":
		defs, uses = targetDefUses(node.ChildByFieldName("argument"), source, aliases, options, true)
	}
	return defs, uses
}

// targetDefUses renders an assignment target. A precise access-path target is a
// definition (and, when compound, also a read of itself). A non-path target
// reads its components but defines nothing.
func targetDefUses(left *tree_sitter.Node, source []byte, aliases jsBindingAliases, options jsAccessPathOptions, readsTarget bool) (defs, uses []string) {
	if left == nil {
		return nil, nil
	}
	if name, ok := jsAssignTargetPathWithOptions(left, source, aliases, options); ok && name != "" {
		defs = append(defs, name)
		if readsTarget {
			uses = append(uses, name)
		}
		return defs, uses
	}
	return nil, exprUsesWithOptions(left, source, aliases, options)
}

// exprUsesWithOptions returns the field-sensitive binding names read within an
// expression subtree. A member read records its access path plus the base
// object; a subscript read records the whole-container approximation and its
// components. By default it does not descend into nested function or
// arrow-function bodies; a function literal passed as a call argument is an
// exception (see jsFuncLiteralCaptureUses) so a captured variable is attributed
// to the enclosing function.
func exprUsesWithOptions(node *tree_sitter.Node, source []byte, aliases jsBindingAliases, options jsAccessPathOptions) []string {
	if node == nil {
		return nil
	}
	var uses []string
	var visit func(*tree_sitter.Node, bool)
	visit = func(current *tree_sitter.Node, includeFuncLiteralCaptures bool) {
		if current == nil {
			return
		}
		if isNestedFunction(current.Kind()) {
			if includeFuncLiteralCaptures {
				uses = append(uses, jsFuncLiteralCaptureUses(current, source, aliases, options)...)
			}
			return
		}
		if name, ok := jsAccessPathWithOptions(current, source, aliases, options); ok {
			if name != "" {
				uses = append(uses, name)
				if current.Kind() == "member_expression" {
					uses = appendBaseAccessPath(uses, current, source, aliases, options)
				}
			}
			// A member read records its path plus base object and stops: the
			// property name is not a use. A subscript read records its whole-
			// container path but does NOT stop, so the child walk still collects
			// the index expression's uses (and the base object identifier).
			if current.Kind() == "member_expression" {
				return
			}
		}
		cursor := current.Walk()
		defer cursor.Close()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			visit(&child, includeFuncLiteralCaptures || invokesFuncLiteral(current.Kind(), child.Kind()))
		}
	}
	visit(node, false)
	return uses
}

// invokesFuncLiteral reports whether a child of a call expression is in argument
// position (or is the callee of an immediately-invoked function), so a function
// literal there is invoked and its captured variables should be attributed to
// the enclosing function.
func invokesFuncLiteral(parentKind, childKind string) bool {
	if parentKind != "call_expression" {
		return false
	}
	return childKind == "arguments" || isNestedFunction(childKind)
}

// jsFuncLiteralCaptureUses returns the free variables an invoked closure reads:
// the uses in its body minus its own parameters and inner-scope (let/const/var)
// definitions, so inner-scope shadowing is respected.
func jsFuncLiteralCaptureUses(node *tree_sitter.Node, source []byte, aliases jsBindingAliases, options jsAccessPathOptions) []string {
	local := map[string]struct{}{}
	for _, name := range paramNames(node, source) {
		local[name] = struct{}{}
	}
	body := node.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	walkScopeBindings(body, func(child *tree_sitter.Node) {
		if child.Kind() != "lexical_declaration" && child.Kind() != "variable_declaration" {
			return
		}
		cursor := child.Walk()
		defer cursor.Close()
		for _, decl := range child.NamedChildren(cursor) {
			if decl.Kind() != "variable_declarator" {
				continue
			}
			if name := decl.ChildByFieldName("name"); name != nil && name.Kind() == "identifier" {
				local[nodeText(name, source)] = struct{}{}
			}
		}
	})
	uses := exprUsesWithOptions(body, source, aliases, options)
	out := make([]string, 0, len(uses))
	for _, use := range uses {
		if _, shadowed := local[accessPathBase(use)]; shadowed {
			continue
		}
		out = appendUnique(out, use)
	}
	return out
}

// accessPathBase returns the leading binding name of an access path, so a
// captured access path (v.field) is matched against a shadowing local (v).
func accessPathBase(path string) string {
	if i := strings.IndexAny(path, ".["); i >= 0 {
		return path[:i]
	}
	return path
}

// walkScopeBindings visits a closure body without descending into further
// nested function scopes, so only the closure's own inner-scope definitions are
// collected (a doubly-nested closure has its own scope).
func walkScopeBindings(scope *tree_sitter.Node, visit func(*tree_sitter.Node)) {
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
			if isNestedFunction(child.Kind()) {
				continue
			}
			walk(&child)
		}
	}
	walk(scope)
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
