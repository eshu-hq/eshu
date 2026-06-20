package jsdataflow

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type jsParamBinding struct {
	name  string
	index int
}

// paramNames returns the local parameter binding names of a function, in
// declaration order, including object and array destructuring patterns.
func paramNames(node *tree_sitter.Node, source []byte) []string {
	bindings := paramBindings(node, source)
	names := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		names = append(names, binding.name)
	}
	return names
}

func paramBindings(node *tree_sitter.Node, source []byte) []jsParamBinding {
	// An unparenthesized single-parameter arrow function (req => ...) carries the
	// parameter under the singular `parameter` field as a bare identifier.
	if single := node.ChildByFieldName("parameter"); single != nil && single.Kind() == "identifier" {
		if name := nodeText(single, source); name != "" {
			return []jsParamBinding{{name: name, index: 0}}
		}
	}
	params := node.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	var bindings []jsParamBinding
	cursor := params.Walk()
	defer cursor.Close()
	formalIndex := 0
	for _, decl := range params.NamedChildren(cursor) {
		decl := decl
		switch decl.Kind() {
		case "required_parameter", "optional_parameter":
			if pattern := decl.ChildByFieldName("pattern"); pattern != nil {
				for _, name := range jsPatternBindingNames(pattern, source) {
					bindings = append(bindings, jsParamBinding{name: name, index: formalIndex})
				}
			}
			formalIndex++
		case "identifier":
			if name := nodeText(&decl, source); name != "" {
				bindings = append(bindings, jsParamBinding{name: name, index: formalIndex})
			}
			formalIndex++
		}
	}
	return bindings
}

// forInTargets returns the identifier definition targets of a for-in/for-of left
// side: a bare identifier, or the identifiers of an array/object destructuring
// pattern. The grammar's loop binding (const/let/var) appears as a separate
// keyword token, so the left field is the binding pattern itself.
func forInTargets(left *tree_sitter.Node, source []byte) []string {
	if left == nil {
		return nil
	}
	return jsPatternBindingNames(left, source)
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

type jsPatternBinding struct {
	name string
	path []string
}

func jsPatternBindingNames(pattern *tree_sitter.Node, source []byte) []string {
	bindings := jsPatternBindings(pattern, source, nil)
	names := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		if binding.name != "" {
			names = append(names, binding.name)
		}
	}
	return names
}

func jsPatternSourceUses(pattern, value *tree_sitter.Node, source []byte, aliases jsBindingAliases, options jsAccessPathOptions) []string {
	base, ok := jsAccessPathParts(value, source)
	if !ok || len(base) == 0 {
		return nil
	}
	var uses []string
	for _, binding := range jsPatternBindings(pattern, source, base) {
		if len(binding.path) == 0 {
			continue
		}
		use := jsRenderAccessPathPartsWithOptions(binding.path, aliases, options)
		if use != "" {
			uses = appendUnique(uses, use)
		}
	}
	return uses
}

func jsPatternBindings(pattern *tree_sitter.Node, source []byte, sourcePath []string) []jsPatternBinding {
	if pattern == nil {
		return nil
	}
	switch pattern.Kind() {
	case "identifier":
		return []jsPatternBinding{{name: nodeText(pattern, source), path: cloneParts(sourcePath)}}
	case "shorthand_property_identifier_pattern":
		name := nodeText(pattern, source)
		return []jsPatternBinding{{name: name, path: appendPathPart(sourcePath, name)}}
	case "object_pattern":
		return jsObjectPatternBindings(pattern, source, sourcePath)
	case "array_pattern":
		return jsArrayPatternBindings(pattern, source, sourcePath)
	case "pair_pattern":
		return jsPairPatternBindings(pattern, source, sourcePath)
	case "rest_pattern":
		return jsRestPatternBindings(pattern, source, sourcePath)
	case "assignment_pattern":
		if left := pattern.ChildByFieldName("left"); left != nil {
			return jsPatternBindings(left, source, sourcePath)
		}
		if pat := pattern.ChildByFieldName("pattern"); pat != nil {
			return jsPatternBindings(pat, source, sourcePath)
		}
	}
	return nil
}

func jsObjectPatternBindings(pattern *tree_sitter.Node, source []byte, sourcePath []string) []jsPatternBinding {
	var bindings []jsPatternBinding
	cursor := pattern.Walk()
	defer cursor.Close()
	for _, child := range pattern.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "pair_pattern":
			bindings = append(bindings, jsPairPatternBindings(&child, source, sourcePath)...)
		case "shorthand_property_identifier_pattern":
			name := nodeText(&child, source)
			bindings = append(bindings, jsPatternBinding{name: name, path: appendPathPart(sourcePath, name)})
		case "rest_pattern":
			bindings = append(bindings, jsRestPatternBindings(&child, source, sourcePath)...)
		}
	}
	return bindings
}

func jsPairPatternBindings(pattern *tree_sitter.Node, source []byte, sourcePath []string) []jsPatternBinding {
	key := jsPatternKey(pattern, source)
	value := pattern.ChildByFieldName("value")
	if value == nil {
		return nil
	}
	nextPath := cloneParts(sourcePath)
	if key != "" {
		nextPath = appendPathPart(sourcePath, key)
	}
	return jsPatternBindings(value, source, nextPath)
}

func jsArrayPatternBindings(pattern *tree_sitter.Node, source []byte, sourcePath []string) []jsPatternBinding {
	var bindings []jsPatternBinding
	elementPath := appendArrayElementPath(sourcePath)
	cursor := pattern.Walk()
	defer cursor.Close()
	for _, child := range pattern.NamedChildren(cursor) {
		child := child
		if child.Kind() == "rest_pattern" {
			bindings = append(bindings, jsRestPatternBindings(&child, source, elementPath)...)
			continue
		}
		bindings = append(bindings, jsPatternBindings(&child, source, elementPath)...)
	}
	return bindings
}

func jsRestPatternBindings(pattern *tree_sitter.Node, source []byte, sourcePath []string) []jsPatternBinding {
	cursor := pattern.Walk()
	defer cursor.Close()
	for _, child := range pattern.NamedChildren(cursor) {
		child := child
		bindings := jsPatternBindings(&child, source, sourcePath)
		if len(bindings) > 0 {
			return bindings
		}
	}
	return nil
}

func jsPatternKey(pattern *tree_sitter.Node, source []byte) string {
	key := pattern.ChildByFieldName("key")
	if key == nil {
		return ""
	}
	switch key.Kind() {
	case "property_identifier", "identifier", "shorthand_property_identifier_pattern":
		return nodeText(key, source)
	default:
		return ""
	}
}

func cloneParts(parts []string) []string {
	if len(parts) == 0 {
		return nil
	}
	return append([]string{}, parts...)
}

func appendPathPart(parts []string, part string) []string {
	if part == "" {
		return cloneParts(parts)
	}
	out := cloneParts(parts)
	return append(out, part)
}

func appendArrayElementPath(parts []string) []string {
	if len(parts) == 0 {
		return nil
	}
	out := cloneParts(parts)
	out[len(out)-1] += jsSubscriptMarker
	return out
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
			if name := decl.ChildByFieldName("name"); name != nil {
				for _, binding := range jsPatternBindingNames(name, source) {
					local[binding] = struct{}{}
				}
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
