package pydataflow

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// paramNames returns the local parameter binding names of a function definition,
// in declaration order, including *args and **kwargs splats.
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
		case "list_splat_pattern", "dictionary_splat_pattern":
			names = append(names, pyBindingTargetNames(&param, source)...)
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

// assignDefsUsesWithOptions splits a Python assignment or augmented assignment
// into defined and used bindings. A plain identifier (or a tuple/list of them)
// target is a definition; an attribute/subscript target is a field-sensitive
// access-path definition. An augmented assignment (+=) also reads the target.
func assignDefsUsesWithOptions(node *tree_sitter.Node, source []byte, aliases pyBindingAliases, options pyAccessPathOptions) (defs, uses []string) {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	switch node.Kind() {
	case "assignment":
		defs, uses = pyTargetDefUses(left, source, aliases, options, false)
	case "augmented_assignment":
		defs, uses = pyTargetDefUses(left, source, aliases, options, true)
	}
	if right != nil {
		uses = append(uses, exprUsesWithOptions(right, source, aliases, options)...)
		uses = append(uses, pyUnpackSourceUses(left, right, source, aliases, options)...)
	}
	return defs, uses
}

// pyTargetDefUses renders an assignment target. A bare identifier or a
// tuple/list element is a definition; an attribute/subscript target is a
// field-sensitive access-path definition (and, when compound, also a read of
// itself). A target that is not a precise path reads its components but defines
// nothing.
func pyTargetDefUses(left *tree_sitter.Node, source []byte, aliases pyBindingAliases, options pyAccessPathOptions, readsTarget bool) (defs, uses []string) {
	if left == nil {
		return nil, nil
	}
	switch left.Kind() {
	case "identifier":
		name := nodeText(left, source)
		defs = append(defs, name)
		if readsTarget {
			uses = append(uses, name)
		}
	case "pattern_list", "tuple_pattern", "list_pattern":
		cursor := left.Walk()
		defer cursor.Close()
		for _, child := range left.NamedChildren(cursor) {
			child := child
			d, u := pyTargetDefUses(&child, source, aliases, options, readsTarget)
			defs = append(defs, d...)
			uses = append(uses, u...)
		}
	case "list_splat_pattern", "dictionary_splat_pattern":
		cursor := left.Walk()
		defer cursor.Close()
		for _, child := range left.NamedChildren(cursor) {
			child := child
			d, u := pyTargetDefUses(&child, source, aliases, options, readsTarget)
			defs = append(defs, d...)
			uses = append(uses, u...)
		}
	case "attribute", "subscript":
		if name, ok := pyAssignTargetPathWithOptions(left, source, aliases, options); ok && name != "" {
			defs = append(defs, name)
			if readsTarget {
				uses = append(uses, name)
			}
		} else {
			uses = append(uses, exprUsesWithOptions(left, source, aliases, options)...)
		}
	default:
		uses = append(uses, exprUsesWithOptions(left, source, aliases, options)...)
	}
	return defs, uses
}

// assignTargets returns the bare identifier definition targets of an assignment
// left side: a bare identifier, or identifiers inside tuple/list/star patterns.
// Attribute and subscript targets define no bare aliases here.
func assignTargets(left *tree_sitter.Node, source []byte) []string {
	return pyBindingTargetNames(left, source)
}

func pyBindingTargetNames(left *tree_sitter.Node, source []byte) []string {
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
			child := child
			targets = append(targets, pyBindingTargetNames(&child, source)...)
		}
		return targets
	case "list_splat_pattern", "dictionary_splat_pattern":
		var targets []string
		cursor := left.Walk()
		defer cursor.Close()
		for _, child := range left.NamedChildren(cursor) {
			child := child
			targets = append(targets, pyBindingTargetNames(&child, source)...)
		}
		return targets
	default:
		return nil
	}
}

func pyUnpackSourceUses(left, right *tree_sitter.Node, source []byte, aliases pyBindingAliases, options pyAccessPathOptions) []string {
	if !pyIsUnpackTarget(left) {
		return nil
	}
	parts, ok := pyAccessPathParts(right, source)
	if !ok || len(parts) == 0 {
		return nil
	}
	parts = append([]string{}, parts...)
	parts[len(parts)-1] += "[*]"
	use := pyRenderAccessPathPartsWithOptions(parts, aliases, options)
	if use == "" {
		return nil
	}
	return []string{use}
}

func pyIsUnpackTarget(left *tree_sitter.Node) bool {
	if left == nil {
		return false
	}
	switch left.Kind() {
	case "pattern_list", "tuple_pattern", "list_pattern", "list_splat_pattern":
		return true
	default:
		return false
	}
}

// exprUses returns the field-sensitive binding names read within an expression
// subtree with no alias context, for callers (with/except value reads) that do
// not thread the lowerer's alias map.
func exprUses(node *tree_sitter.Node, source []byte) []string {
	return exprUsesWithOptions(node, source, pyBindingAliases{}, pyAccessPathOptions{})
}

// exprUsesWithOptions returns the field-sensitive binding names read within an
// expression subtree. An attribute read records its access path plus the base
// object; a subscript read records the whole-container approximation and its
// components. By default it does not descend into nested function definitions or
// lambdas; a lambda passed as a call argument is an exception (see
// pyFuncLiteralCaptureUses) so a captured variable is attributed to the
// enclosing function.
func exprUsesWithOptions(node *tree_sitter.Node, source []byte, aliases pyBindingAliases, options pyAccessPathOptions) []string {
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
				uses = append(uses, pyFuncLiteralCaptureUses(current, source, aliases, options)...)
			}
			return
		}
		if current.Kind() == "keyword_argument" {
			// Only the value of name=value is a use; the keyword name is not a
			// variable, so visiting it would invent a use of a same-named binding.
			if value := current.ChildByFieldName("value"); value != nil {
				visit(value, includeFuncLiteralCaptures)
			}
			return
		}
		if name, ok := pyAccessPathWithOptions(current, source, aliases, options); ok {
			if name != "" {
				uses = append(uses, name)
				if current.Kind() == "attribute" {
					uses = appendBaseAccessPath(uses, current, source, aliases, options)
				}
			}
			// An attribute read records its path plus base object and stops: the
			// attribute name is not a use. A subscript read records its whole-
			// container path but does NOT stop, so the child walk still collects
			// the index expression's uses (and the base object identifier).
			if current.Kind() == "attribute" {
				return
			}
		}
		cursor := current.Walk()
		defer cursor.Close()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			visit(&child, includeFuncLiteralCaptures || pyInvokesFuncLiteral(current.Kind(), child.Kind()))
		}
	}
	visit(node, false)
	return uses
}

// pyInvokesFuncLiteral reports whether a child of a call is in argument position
// (or is the callee of an immediately-invoked lambda), so a lambda there is
// invoked and its captured variables should be attributed to the enclosing
// function. In Python only a lambda can be a function literal in expression
// position; a nested def is a statement and never an argument.
func pyInvokesFuncLiteral(parentKind, childKind string) bool {
	if parentKind != "call" {
		return false
	}
	return childKind == "argument_list" || isNestedFunction(childKind)
}

// pyFuncLiteralCaptureUses returns the free variables an invoked closure reads:
// the uses in its body minus its own parameters and inner-scope assignment
// targets, so inner-scope shadowing (and lambda-parameter shadowing) is
// respected.
func pyFuncLiteralCaptureUses(node *tree_sitter.Node, source []byte, aliases pyBindingAliases, options pyAccessPathOptions) []string {
	local := map[string]struct{}{}
	for _, name := range paramNames(node, source) {
		local[name] = struct{}{}
	}
	body := node.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	walkScopeBindings(body, func(child *tree_sitter.Node) {
		switch child.Kind() {
		case "assignment", "augmented_assignment":
			if left := child.ChildByFieldName("left"); left != nil {
				for _, def := range assignTargets(left, source) {
					local[def] = struct{}{}
				}
			}
		case "named_expression":
			if name := child.ChildByFieldName("name"); name != nil && name.Kind() == "identifier" {
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

// walkScopeBindings visits a closure body without descending into further nested
// function scopes, so only the closure's own inner-scope definitions are
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

// asPatternDefsUses splits an `expr as target` pattern (used by with-items and
// except clauses): the expression is a use and the alias target's identifiers
// are defs. A node that is not an as_pattern is treated as a plain expression
// use.
func asPatternDefsUses(node *tree_sitter.Node, source []byte) (defs, uses []string) {
	if node == nil {
		return nil, nil
	}
	if node.Kind() != "as_pattern" {
		return nil, exprUses(node, source)
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "as_pattern_target" {
			defs = append(defs, identifierNames(&child, source)...)
			continue
		}
		uses = append(uses, exprUses(&child, source)...)
	}
	return defs, uses
}

// identifierNames returns the text of every identifier within a subtree. It is
// used for `as` alias targets, which are bare identifiers (or a tuple of them).
func identifierNames(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	var names []string
	var visit func(*tree_sitter.Node)
	visit = func(current *tree_sitter.Node) {
		if current == nil {
			return
		}
		if current.Kind() == "identifier" {
			if name := nodeText(current, source); name != "" {
				names = append(names, name)
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
	visit(node)
	return names
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
