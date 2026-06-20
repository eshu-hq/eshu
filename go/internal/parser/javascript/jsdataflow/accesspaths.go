package jsdataflow

import (
	"strings"
	"unicode"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// jsDefaultAccessPathParts matches cfg.DefaultLimits().MaxAccessPathParts for
// helper callers that do not thread a CFG limit object.
const jsDefaultAccessPathParts = 4
const jsSubscriptMarker = "[*]"

// jsAccessPathOptions carries the field-sensitivity depth cap and the shared
// truncation counter. truncated, when non-nil, is incremented once per access
// path that exceeds maxParts so over-approximation is counted, never silent.
type jsAccessPathOptions struct {
	maxParts  int
	truncated *int
}

func (o jsAccessPathOptions) normalizedMaxParts() int {
	if o.maxParts <= 0 {
		return jsDefaultAccessPathParts
	}
	return o.maxParts
}

// jsAccessPathWithOptions renders a member/subscript expression as a
// field-sensitive binding string (obj.field, arr[*], obj.a.b). A path deeper
// than maxParts truncates to its prefix plus a "*" segment and counts an
// overflow, so the write and read of the same deep path still match. The base
// segment of a multi-part path is resolved through the reference-alias map so a
// field write through an alias normalizes to the aliased object.
func jsAccessPathWithOptions(node *tree_sitter.Node, source []byte, aliases jsBindingAliases, options jsAccessPathOptions) (string, bool) {
	parts, ok := jsAccessPathParts(node, source)
	if !ok || len(parts) == 0 {
		return "", ok
	}
	return jsRenderAccessPathPartsWithOptions(parts, aliases, options), true
}

func jsRenderAccessPathPartsWithOptions(parts []string, aliases jsBindingAliases, options jsAccessPathOptions) string {
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

// jsAccessPathParts decomposes a member/subscript expression into its path
// segments. A bare identifier is a single segment; a member access appends the
// property name; a subscript appends a "[*]" whole-container marker to the base
// segment. Unsupported operands (a call result, a literal) yield no path.
func jsAccessPathParts(node *tree_sitter.Node, source []byte) ([]string, bool) {
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
	case "member_expression":
		base, ok := jsAccessPathParts(node.ChildByFieldName("object"), source)
		if !ok || len(base) == 0 {
			return nil, false
		}
		field := nodeText(node.ChildByFieldName("property"), source)
		if field == "" {
			return nil, false
		}
		// Copy before appending so the returned path never shares a backing array
		// with the recursive base (symmetric with the subscript case below).
		path := append([]string{}, base...)
		return append(path, field), true
	case "subscript_expression":
		base, ok := jsAccessPathParts(node.ChildByFieldName("object"), source)
		if !ok || len(base) == 0 {
			return nil, false
		}
		indexed := append([]string{}, base...)
		indexed[len(indexed)-1] += jsSubscriptMarker
		return indexed, true
	case "parenthesized_expression":
		return jsAccessPathParts(firstNamedChild(node), source)
	}
	return nil, false
}

// jsAssignTargetPathWithOptions renders an assignment target as a precise
// binding: a bare identifier keeps its name (never alias-resolved, so a plain
// reference copy retains its own reaching-def identity); a member/subscript
// target becomes a field-sensitive access path.
func jsAssignTargetPathWithOptions(node *tree_sitter.Node, source []byte, aliases jsBindingAliases, options jsAccessPathOptions) (string, bool) {
	if node == nil {
		return "", false
	}
	if node.Kind() == "identifier" {
		return nodeText(node, source), true
	}
	return jsAccessPathWithOptions(node, source, aliases, options)
}

// appendBaseAccessPath records the base object of a member read as an additional
// use, so reassigning the whole object also reaches a read of one of its fields.
func appendBaseAccessPath(uses []string, node *tree_sitter.Node, source []byte, aliases jsBindingAliases, options jsAccessPathOptions) []string {
	object := node.ChildByFieldName("object")
	if object == nil {
		return uses
	}
	base, ok := jsAccessPathWithOptions(object, source, aliases, options)
	if !ok || base == "" {
		return uses
	}
	return appendUnique(uses, base)
}

// jsBindingAliases maps a binding name to the object it references. Reference
// aliasing in JS/TS has no address-of operator: a plain `let a = obj` makes a
// and obj the same object, so a write to a.field is observable as obj.field.
// Only the base segment of a multi-part access path is resolved through this
// map; bare identifier reads keep their own identity so simple value flow and
// reaching-definition truth are unchanged.
type jsBindingAliases map[string]string

// resolve follows the alias chain to a fixed point, stopping on a cycle.
func (a jsBindingAliases) resolve(name string) string {
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

// resolveBase rewrites the leading segment of an access path through the alias
// map. A single-segment bare identifier is returned unchanged so plain copies
// keep their reaching-def identity; a single-segment subscript path still
// resolves its base before keeping the [*] approximation.
func (a jsBindingAliases) resolveBase(parts []string) []string {
	if len(a) == 0 || len(parts) == 0 {
		return parts
	}
	base, suffix := splitSubscriptMarker(parts[0])
	if len(parts) == 1 && suffix == "" {
		return parts
	}
	resolved := a.resolve(base)
	if resolved == "" || resolved == base {
		return parts
	}
	rebased := strings.Split(resolved, ".")
	rebased[len(rebased)-1] += suffix
	return append(rebased, parts[1:]...)
}

func splitSubscriptMarker(part string) (base, suffix string) {
	if strings.HasSuffix(part, jsSubscriptMarker) {
		return strings.TrimSuffix(part, jsSubscriptMarker), jsSubscriptMarker
	}
	return part, ""
}

func (a jsBindingAliases) clone() jsBindingAliases {
	if len(a) == 0 {
		return jsBindingAliases{}
	}
	out := make(jsBindingAliases, len(a))
	for k, v := range a {
		out[k] = v
	}
	return out
}

// applyAssignment updates the alias map for a plain assignment. An
// identifier-to-identifier assignment (a = b) records a as an alias of b's
// referent; any other right-hand side clears a's alias.
func (a jsBindingAliases) applyAssignment(target string, right *tree_sitter.Node, source []byte) {
	if target == "" {
		return
	}
	delete(a, target)
	if aliased, ok := jsAliasTarget(right, source, a); ok && aliased != target {
		a[target] = aliased
	}
}

// jsMergeAliases keeps only the aliases that agree on both control-flow paths,
// so a binding aliased differently across branches is conservatively dropped.
func jsMergeAliases(a, b jsBindingAliases) jsBindingAliases {
	out := jsBindingAliases{}
	for k, av := range a {
		if bv, ok := b[k]; ok && bv == av {
			out[k] = av
		}
	}
	return out
}

// jsAliasTarget returns the referent recorded by an identifier right-hand side,
// resolving through any existing alias chain. Non-identifier values (a call, a
// literal, a member access) do not create an alias.
func jsAliasTarget(node *tree_sitter.Node, source []byte, aliases jsBindingAliases) (string, bool) {
	if node == nil || node.Kind() != "identifier" {
		return "", false
	}
	text := strings.TrimSpace(nodeText(node, source))
	if !jsIdentifierLike(text) {
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

// jsIdentifierLike reports whether text is a plain identifier (so it can name an
// alias target) rather than a more complex expression.
func jsIdentifierLike(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if i == 0 {
			if r != '_' && r != '$' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && r != '$' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
