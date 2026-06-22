package swift

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// swiftTypeNameAndKind returns the declared name and kind for a nominal type
// declaration node. The Swift grammar models class, actor, struct, and enum as a
// single class_declaration node, so the leading keyword between the node start
// and the name disambiguates the kind. It returns empty strings for nodes whose
// name field is a user_type (extensions) rather than a type_identifier.
func swiftTypeNameAndKind(node *tree_sitter.Node, source []byte) (string, string) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil || nameNode.Kind() != "type_identifier" {
		return "", ""
	}
	name := swiftTrimText(nameNode, source)
	kind, _ := swiftTreeTypeKind(node, nameNode, source)
	if name == "" || kind == "" {
		return "", ""
	}
	return name, kind
}

// swiftEmitType appends a nominal type row (class, actor, struct, enum, or
// protocol) and returns its kind and bucket. Extensions are handled separately
// and never reach this path.
func (b *swiftPayloadBuilder) emitTypeDeclaration(node *tree_sitter.Node, source []byte, attributes []string) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := swiftTrimText(nameNode, source)
	kind, bucket := swiftTreeTypeKind(node, nameNode, source)
	if name == "" || bucket == "" {
		return
	}
	bases := swiftTreeInheritance(node, source)
	item := map[string]any{
		"name":        name,
		"line_number": swiftDeclarationLine(node),
		"end_line":    shared.NodeEndLine(node),
		"bases":       bases,
		"lang":        "swift",
	}
	if rootKinds := swiftTypeDeadCodeRootKinds(kind, bases, attributes); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	shared.AppendBucket(b.payload, bucket, item)
}

// emitProtocolDeclaration appends a protocol row.
func (b *swiftPayloadBuilder) emitProtocolDeclaration(node *tree_sitter.Node, source []byte, attributes []string) {
	nameNode := node.ChildByFieldName("name")
	name := swiftTrimText(nameNode, source)
	if name == "" {
		return
	}
	bases := swiftTreeInheritance(node, source)
	item := map[string]any{
		"name":        name,
		"line_number": swiftDeclarationLine(node),
		"end_line":    shared.NodeEndLine(node),
		"bases":       bases,
		"lang":        "swift",
	}
	if rootKinds := swiftTypeDeadCodeRootKinds("protocol", bases, attributes); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	shared.AppendBucket(b.payload, "protocols", item)
}

func swiftTreeTypeKind(node *tree_sitter.Node, nameNode *tree_sitter.Node, source []byte) (string, string) {
	if node == nil || nameNode == nil || nameNode.StartByte() < node.StartByte() {
		return "", ""
	}
	text := string(source[node.StartByte():nameNode.StartByte()])
	for _, typed := range []struct {
		token  string
		kind   string
		bucket string
	}{
		{token: "actor", kind: "class", bucket: "classes"},
		{token: "class", kind: "class", bucket: "classes"},
		{token: "enum", kind: "enum", bucket: "enums"},
		{token: "struct", kind: "struct", bucket: "structs"},
	} {
		if swiftTextHasToken(text, typed.token) {
			return typed.kind, typed.bucket
		}
	}
	return "", ""
}

// swiftExtensionTypeName returns the extended type name for an `extension`
// declaration. The Swift grammar models `extension Foo { ... }` as a
// class_declaration whose extended type is a direct user_type child rather than
// a type_identifier name field. The leading `extension` keyword must be present
// in the text before the type so true class/struct/enum declarations are not
// misread as extensions.
func swiftExtensionTypeName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	var userType *tree_sitter.Node
	for _, child := range swiftNamedChildren(node) {
		child := child
		if child.Kind() == "user_type" {
			userType = shared.CloneNode(&child)
			break
		}
	}
	if userType == nil {
		return ""
	}
	prefix := string(source[node.StartByte():userType.StartByte()])
	if !swiftTextHasToken(prefix, "extension") {
		return ""
	}
	return swiftShortTypeName(strings.TrimSpace(shared.NodeText(userType, source)))
}

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

// swiftTreeInheritance returns the inheritance/conformance clause types for a
// type declaration. Generic parameter lists (`<T>`) are modeled as separate
// nodes, not inheritance_specifier children, so constrained generics such as
// `enum Result<Success, Failure: Error>` contribute no bases.
func swiftTreeInheritance(node *tree_sitter.Node, source []byte) []string {
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
			base := swiftTrimText(inherited, source)
			if base != "" {
				bases = append(bases, base)
			}
		})
	}
	if len(bases) == 0 {
		return nil
	}
	return bases
}
