package javascript

import (
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptImplementedInterfaces(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	var values []string
	walkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "implements_clause" {
			return
		}
		if !javaScriptNearestClassAncestorMatches(child, node) {
			return
		}
		values = append(values, javaScriptImplementedInterfaceList(nodeText(child, source))...)
	})
	if len(values) == 0 {
		return nil
	}
	return dedupeSortedJavaScriptStrings(values)
}

func javaScriptNearestClassAncestorMatches(child *tree_sitter.Node, want *tree_sitter.Node) bool {
	if child == nil || want == nil {
		return false
	}
	for current := child.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "abstract_class_declaration":
			return current.StartByte() == want.StartByte() && current.EndByte() == want.EndByte()
		}
	}
	return false
}

func javaScriptImplementedInterfaceList(raw string) []string {
	tail := strings.TrimSpace(raw)
	tail = strings.TrimPrefix(tail, "implements")
	if index := strings.Index(tail, "{"); index >= 0 {
		tail = tail[:index]
	}
	parts := splitJavaScriptTypeList(tail)
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		name := javaScriptImplementedInterfaceName(part)
		if name != "" && !javaScriptIsBuiltinTypeName(name) {
			values = append(values, name)
		}
	}
	return values
}

func splitJavaScriptTypeList(raw string) []string {
	parts := make([]string, 0, 2)
	var current strings.Builder
	genericDepth := 0
	for _, r := range raw {
		switch r {
		case '<':
			genericDepth++
			current.WriteRune(r)
		case '>':
			if genericDepth > 0 {
				genericDepth--
			}
			current.WriteRune(r)
		case ',':
			if genericDepth == 0 {
				parts = append(parts, strings.TrimSpace(current.String()))
				current.Reset()
				continue
			}
			current.WriteRune(r)
		default:
			current.WriteRune(r)
		}
	}
	if tail := strings.TrimSpace(current.String()); tail != "" {
		parts = append(parts, tail)
	}
	return parts
}

func javaScriptImplementedInterfaceName(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if index := strings.Index(value, "<"); index >= 0 {
		value = value[:index]
	}
	return javaScriptTypeReferenceLeafName(value)
}

func dedupeSortedJavaScriptStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}
