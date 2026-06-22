package swift

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// swiftNamedChildren returns the named direct children of a node. Callers must
// not retain the returned slice past the next cursor use; clone with
// shared.CloneNode when a node must outlive iteration.
func swiftNamedChildren(node *tree_sitter.Node) []tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	return node.NamedChildren(cursor)
}

// swiftDeclarationLine returns the 1-based line of a declaration's defining
// keyword (func, init, struct, class, enum, protocol), i.e. the first child
// that is not the leading `modifiers` block. This skips attribute/modifier
// lines so a declaration reports the keyword line rather than an attribute line
// placed above it.
func swiftDeclarationLine(node *tree_sitter.Node) int {
	if node == nil {
		return 1
	}
	for index := 0; index < int(node.ChildCount()); index++ {
		child := node.Child(uint(index))
		if child.Kind() != "modifiers" {
			return int(child.StartPosition().Row) + 1
		}
	}
	return int(node.StartPosition().Row) + 1
}

// swiftTrimText returns the trimmed source text covered by a node, or "" for a
// nil node.
func swiftTrimText(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(shared.NodeText(node, source))
}

// swiftSourceLines splits the source into lines once per file for the few
// places that still need the raw declaration header text (function source and
// call dedup discrimination).
func swiftSourceLines(source []byte) []string {
	return strings.Split(string(source), "\n")
}

// swiftDeclarationAttributes returns the attribute names attached to a
// declaration node via its modifiers, e.g. "main", "Test", or "available". The
// grammar nests them as modifiers > attribute > user_type > type_identifier.
func swiftDeclarationAttributes(node *tree_sitter.Node, source []byte) []string {
	modifiers := swiftChildByKind(node, "modifiers")
	if modifiers == nil {
		return nil
	}
	attributes := make([]string, 0, 2)
	for _, child := range swiftNamedChildren(modifiers) {
		child := child
		if child.Kind() != "attribute" {
			continue
		}
		if name := swiftAttributeName(&child, source); name != "" {
			attributes = append(attributes, name)
		}
	}
	if len(attributes) == 0 {
		return nil
	}
	return attributes
}

// swiftAttributeName returns the attribute identifier (without the leading @)
// for an attribute node.
func swiftAttributeName(node *tree_sitter.Node, source []byte) string {
	userType := swiftChildByKind(node, "user_type")
	if userType == nil {
		return ""
	}
	return swiftShortTypeName(swiftTrimText(userType, source))
}

// swiftStringSet returns the set of short type names for the given values.
func swiftStringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		shortName := swiftShortTypeName(value)
		if shortName != "" {
			set[shortName] = struct{}{}
		}
	}
	return set
}

// swiftShortTypeName reduces a possibly-qualified, optional, or generic type
// spelling to its bare type name.
func swiftShortTypeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, "?")
	if index := strings.Index(name, "<"); index >= 0 {
		name = name[:index]
	}
	if index := strings.LastIndex(name, "."); index >= 0 {
		name = name[index+1:]
	}
	return strings.TrimSpace(name)
}

// swiftHasAttribute reports whether the attribute name is present.
func swiftHasAttribute(attributes []string, name string) bool {
	return slices.Contains(attributes, name)
}
