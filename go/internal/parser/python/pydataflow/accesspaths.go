// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pydataflow

import (
	"strings"
	"unicode"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// pyDefaultAccessPathParts matches cfg.DefaultLimits().MaxAccessPathParts for
// helper callers that do not thread a CFG limit object.
const pyDefaultAccessPathParts = 4

// pyAccessPathOptions carries the field-sensitivity depth cap and the shared
// truncation counter. truncated, when non-nil, is incremented once per access
// path that exceeds maxParts so over-approximation is counted, never silent.
type pyAccessPathOptions struct {
	maxParts  int
	truncated *int
}

func (o pyAccessPathOptions) normalizedMaxParts() int {
	if o.maxParts <= 0 {
		return pyDefaultAccessPathParts
	}
	return o.maxParts
}

// pyAccessPathWithOptions renders an attribute/subscript expression as a
// field-sensitive binding string (obj.attr, d[*], obj.a.b). A path deeper than
// maxParts truncates to its prefix plus a "*" segment and counts an overflow, so
// the write and read of the same deep path still match. The base segment of a
// multi-part path is resolved through the reference-alias map so an attribute
// write through an alias normalizes to the aliased object.
func pyAccessPathWithOptions(node *tree_sitter.Node, source []byte, aliases pyBindingAliases, options pyAccessPathOptions) (string, bool) {
	parts, ok := pyAccessPathParts(node, source)
	if !ok || len(parts) == 0 {
		return "", ok
	}
	return pyRenderAccessPathPartsWithOptions(parts, aliases, options), true
}

func pyRenderAccessPathPartsWithOptions(parts []string, aliases pyBindingAliases, options pyAccessPathOptions) string {
	parts = aliases.resolveBase(parts)
	maxParts := options.normalizedMaxParts()
	if len(parts) > maxParts {
		if options.truncated != nil {
			*options.truncated = *options.truncated + 1
		}
		truncated := append([]string{}, parts[:maxParts]...)
		truncated = append(truncated, "*")
		return strings.Join(truncated, ".")
	}
	return strings.Join(parts, ".")
}

// pyAccessPathParts decomposes an attribute/subscript expression into its path
// segments. A bare identifier is a single segment; an attribute appends the
// attribute name; a subscript appends a "[*]" whole-container marker to the base
// segment. Unsupported operands (a call result, a literal) yield no path.
func pyAccessPathParts(node *tree_sitter.Node, source []byte) ([]string, bool) {
	if node == nil {
		return nil, false
	}
	switch node.Kind() {
	case "identifier":
		name := nodeText(node, source)
		if name == "" {
			return nil, false
		}
		return []string{name}, true
	case "attribute":
		base, ok := pyAccessPathParts(node.ChildByFieldName("object"), source)
		if !ok || len(base) == 0 {
			return nil, false
		}
		field := nodeText(node.ChildByFieldName("attribute"), source)
		if field == "" {
			return nil, false
		}
		path := append([]string{}, base...)
		return append(path, field), true
	case "subscript":
		base, ok := pyAccessPathParts(node.ChildByFieldName("value"), source)
		if !ok || len(base) == 0 {
			return nil, false
		}
		indexed := append([]string{}, base...)
		indexed[len(indexed)-1] += "[*]"
		return indexed, true
	case "parenthesized_expression":
		return pyAccessPathParts(firstNamedChild(node), source)
	}
	return nil, false
}

// pyAssignTargetPathWithOptions renders an assignment target as a precise
// binding: a bare identifier keeps its name (never alias-resolved, so a plain
// reference copy retains its own reaching-def identity); an attribute/subscript
// target becomes a field-sensitive access path.
func pyAssignTargetPathWithOptions(node *tree_sitter.Node, source []byte, aliases pyBindingAliases, options pyAccessPathOptions) (string, bool) {
	if node == nil {
		return "", false
	}
	if node.Kind() == "identifier" {
		return nodeText(node, source), true
	}
	return pyAccessPathWithOptions(node, source, aliases, options)
}

// appendBaseAccessPath records the base object of an attribute read as an
// additional use, so reassigning the whole object also reaches a read of one of
// its attributes.
func appendBaseAccessPath(uses []string, node *tree_sitter.Node, source []byte, aliases pyBindingAliases, options pyAccessPathOptions) []string {
	object := node.ChildByFieldName("object")
	if object == nil {
		return uses
	}
	base, ok := pyAccessPathWithOptions(object, source, aliases, options)
	if !ok || base == "" {
		return uses
	}
	uses = appendUnique(uses, base)
	parts := strings.Split(base, ".")
	for i := len(parts) - 1; i > 0; i-- {
		uses = appendUnique(uses, strings.Join(parts[:i], "."))
	}
	return uses
}

// pyBindingAliases maps a binding name to the object it references. Reference
// aliasing in Python has no address-of operator: a plain `a = obj` makes a and
// obj the same mutable object, so a write to a.attr is observable as obj.attr.
// Only the base segment of a multi-part access path is resolved through this
// map; bare identifier reads keep their own identity so simple value flow and
// reaching-definition truth are unchanged.
type pyBindingAliases map[string]string

// resolve follows the alias chain to a fixed point, stopping on a cycle.
func (a pyBindingAliases) resolve(name string) string {
	if name == "" {
		return ""
	}
	seen := map[string]struct{}{}
	for {
		next, ok := a[name]
		if !ok || next == "" {
			return name
		}
		if _, cycle := seen[name]; cycle {
			return name
		}
		seen[name] = struct{}{}
		name = next
	}
}

// resolveBase rewrites the leading segment of a multi-part access path through
// the alias map. A single-segment path (a bare identifier) is returned
// unchanged so plain copies keep their reaching-def identity.
func (a pyBindingAliases) resolveBase(parts []string) []string {
	if len(a) == 0 || len(parts) <= 1 {
		return parts
	}
	resolved := a.resolve(parts[0])
	if resolved == "" || resolved == parts[0] {
		return parts
	}
	rebased := strings.Split(resolved, ".")
	return append(rebased, parts[1:]...)
}

func (a pyBindingAliases) clone() pyBindingAliases {
	if len(a) == 0 {
		return pyBindingAliases{}
	}
	out := make(pyBindingAliases, len(a))
	for k, v := range a {
		out[k] = v
	}
	return out
}

// applyAssignment updates the alias map for a plain assignment. An
// identifier-to-identifier assignment (a = b) records a as an alias of b's
// referent; any other right-hand side clears a's alias.
func (a pyBindingAliases) applyAssignment(target string, right *tree_sitter.Node, source []byte) {
	if target == "" {
		return
	}
	delete(a, target)
	if aliased, ok := pyAliasTarget(right, source, a); ok && aliased != target {
		a[target] = aliased
	}
}

// pyMergeAliases keeps only the aliases that agree on both control-flow paths,
// so a binding aliased differently across branches is conservatively dropped.
func pyMergeAliases(a, b pyBindingAliases) pyBindingAliases {
	out := pyBindingAliases{}
	for k, av := range a {
		if bv, ok := b[k]; ok && bv == av {
			out[k] = av
		}
	}
	return out
}

// pyMergeAllAliases intersects every reaching alias state, so an alias survives
// a merge only when every path that reaches it agrees.
func pyMergeAllAliases(states []pyBindingAliases) pyBindingAliases {
	if len(states) == 0 {
		return pyBindingAliases{}
	}
	out := states[0].clone()
	for _, s := range states[1:] {
		out = pyMergeAliases(out, s)
	}
	return out
}

// pyAliasTarget returns the referent recorded by an identifier right-hand side,
// resolving through any existing alias chain. Non-identifier values (a call, a
// literal, an attribute access) do not create an alias.
func pyAliasTarget(node *tree_sitter.Node, source []byte, aliases pyBindingAliases) (string, bool) {
	if node == nil || node.Kind() != "identifier" {
		return "", false
	}
	text := strings.TrimSpace(nodeText(node, source))
	if !pyIdentifierLike(text) {
		return "", false
	}
	return aliases.resolve(text), true
}

// firstNamedChild returns the first named child of a node, or nil.
func firstNamedChild(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	children := node.NamedChildren(cursor)
	if len(children) == 0 {
		return nil
	}
	first := children[0]
	return &first
}

// appendUnique appends value when it is not already present, preserving order.
func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// pyIdentifierLike reports whether text is a plain identifier (so it can name an
// alias target) rather than a more complex expression.
func pyIdentifierLike(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// accessPathBase returns the leading binding name of an access path, so a
// captured access path (v.attr) is matched against a shadowing local (v).
func accessPathBase(path string) string {
	if i := strings.IndexAny(path, ".["); i >= 0 {
		return path[:i]
	}
	return path
}
