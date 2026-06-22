package swift

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// swiftNamedChildren returns the named direct children of a node. Callers iterate
// the slice by value, so each child is copied; take a pointer with &child when a
// stable handle is needed past the loop.
func swiftNamedChildren(node *tree_sitter.Node) []tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	return node.NamedChildren(cursor)
}

// swiftFirstChildOfKind returns the first named child whose kind matches one of
// kinds, or nil. It is used to locate the type-name, body, and suffix nodes the
// Swift grammar attaches as positional children rather than named fields.
func swiftFirstChildOfKind(node *tree_sitter.Node, kinds ...string) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	for _, child := range swiftNamedChildren(node) {
		child := child
		for _, kind := range kinds {
			if child.Kind() == kind {
				return shared.CloneNode(&child)
			}
		}
	}
	return nil
}

// swiftDeclarationKeyword returns the leading declaration keyword (class, struct,
// enum, actor, extension, protocol) for a type declaration node by reading the
// source bytes before the type name. The grammar collapses class/struct/enum/
// actor/extension into a single class_declaration node, so the keyword text is
// the only discriminator.
func swiftDeclarationKeyword(node *tree_sitter.Node, nameStart uint, source []byte) string {
	if node == nil || nameStart < node.StartByte() {
		return ""
	}
	prefix := string(source[node.StartByte():nameStart])
	for _, keyword := range []string{"extension", "protocol", "actor", "class", "struct", "enum"} {
		if swiftTextHasToken(prefix, keyword) {
			return keyword
		}
	}
	return ""
}

// swiftTextHasToken reports whether text contains token as a whole
// identifier-delimited word, so "class" does not match inside "classifier".
func swiftTextHasToken(text string, token string) bool {
	fields := strings.FieldsFunc(text, func(character rune) bool {
		return character != '_' &&
			(character < '0' || character > '9') &&
			(character < 'A' || character > 'Z') &&
			(character < 'a' || character > 'z')
	})
	for _, field := range fields {
		if field == token {
			return true
		}
	}
	return false
}

// swiftInheritanceBases returns the conformance/superclass names from a
// declaration's inheritance_specifier children, short-named so generic and dotted
// types collapse to the leaf identifier.
func swiftInheritanceBases(node *tree_sitter.Node, source []byte) []string {
	bases := make([]string, 0, 2)
	for _, child := range swiftNamedChildren(node) {
		child := child
		if child.Kind() != "inheritance_specifier" {
			continue
		}
		shared.WalkNamed(&child, func(inherited *tree_sitter.Node) {
			if inherited.Kind() != "user_type" {
				return
			}
			base := strings.TrimSpace(shared.NodeText(inherited, source))
			if base != "" {
				bases = append(bases, swiftShortTypeName(base))
			}
		})
	}
	if len(bases) == 0 {
		return nil
	}
	return bases
}

// swiftParameterNames returns the external/internal parameter names declared by a
// function or initializer node. Wildcard `_` labels are dropped, matching the
// prior extractor and the call-graph contract that keys arguments by usable name.
func swiftParameterNames(node *tree_sitter.Node, source []byte) []string {
	args := make([]string, 0, 2)
	for _, child := range swiftNamedChildren(node) {
		child := child
		if child.Kind() != "parameter" {
			continue
		}
		name := swiftParameterName(&child, source)
		if name != "" && name != "_" {
			args = append(args, name)
		}
	}
	if len(args) == 0 {
		return nil
	}
	return args
}

// swiftParameterName returns the binding name for one parameter node. Swift
// parameters carry an optional external label followed by the internal name; the
// internal name is the last simple_identifier before the type annotation.
func swiftParameterName(node *tree_sitter.Node, source []byte) string {
	if named := node.ChildByFieldName("name"); named != nil {
		return strings.TrimSpace(shared.NodeText(named, source))
	}
	var last string
	for _, child := range swiftNamedChildren(node) {
		child := child
		if child.Kind() == "simple_identifier" {
			last = strings.TrimSpace(shared.NodeText(&child, source))
		} else if child.Kind() == "type_annotation" || child.Kind() == "user_type" {
			break
		}
	}
	return last
}

// swiftPatternName returns the bound identifier for a property pattern node.
func swiftPatternName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if node.Kind() == "simple_identifier" {
		return strings.TrimSpace(shared.NodeText(node, source))
	}
	identifier := swiftFirstChildOfKind(node, "simple_identifier")
	if identifier == nil {
		return ""
	}
	return strings.TrimSpace(shared.NodeText(identifier, source))
}

// swiftTypeAnnotationText returns the declared type text for a property's
// type_annotation child with the leading colon removed and whitespace trimmed.
// An empty string means the property relies on type inference.
func swiftTypeAnnotationText(node *tree_sitter.Node, source []byte) string {
	annotation := swiftFirstChildOfKind(node, "type_annotation")
	if annotation == nil {
		return ""
	}
	text := strings.TrimSpace(shared.NodeText(annotation, source))
	text = strings.TrimPrefix(text, ":")
	return strings.TrimSpace(text)
}
