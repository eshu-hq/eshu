package swift

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type swiftSyntaxIndex struct {
	types     []swiftTypeSpan
	functions []swiftFunctionSpan
}

type swiftTypeSpan struct {
	name      string
	kind      string
	nameLine  int
	startLine int
	endLine   int
	bases     []string
}

type swiftFunctionSpan struct {
	name         string
	classContext string
	scopeKind    string
	args         []string
	startLine    int
	endLine      int
}

func swiftSourceAndSyntax(path string, parser *tree_sitter.Parser) ([]byte, swiftSyntaxIndex, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, swiftSyntaxIndex{}, err
	}
	syntax, err := swiftTreeSyntax(source, parser)
	if err != nil {
		return nil, swiftSyntaxIndex{}, err
	}
	return source, syntax, nil
}

func swiftTreeSyntax(source []byte, parser *tree_sitter.Parser) (swiftSyntaxIndex, error) {
	if parser == nil {
		return swiftSyntaxIndex{}, fmt.Errorf("parse swift tree: nil parser")
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return swiftSyntaxIndex{}, fmt.Errorf("parse swift tree: parser returned nil tree")
	}
	defer tree.Close()

	index := swiftSyntaxIndex{}
	index.collect(tree.RootNode(), source, "", "")
	return index, nil
}

func (i *swiftSyntaxIndex) collect(node *tree_sitter.Node, source []byte, currentType string, currentKind string) {
	if node == nil {
		return
	}
	nextType := currentType
	nextKind := currentKind
	switch node.Kind() {
	case "class_declaration":
		nameNode := node.ChildByFieldName("name")
		name := strings.TrimSpace(shared.NodeText(nameNode, source))
		kind, bucket := swiftTreeTypeKind(node, nameNode, source)
		if name != "" && bucket != "" {
			nextType = name
			nextKind = kind
			i.types = append(i.types, swiftTypeSpan{
				name:      name,
				kind:      kind,
				nameLine:  shared.NodeLine(nameNode),
				startLine: shared.NodeLine(node),
				endLine:   shared.NodeEndLine(node),
				bases:     swiftTreeInheritance(node, source),
			})
		} else if extended := swiftExtensionTypeName(node, source); extended != "" {
			// `extension Foo { ... }` parses as a class_declaration whose name is
			// a user_type, not a type_identifier. Attribute members to the
			// extended type without emitting a new type entity.
			nextType = extended
			nextKind = "extension"
		}
	case "protocol_declaration":
		nameNode := node.ChildByFieldName("name")
		name := strings.TrimSpace(shared.NodeText(nameNode, source))
		if name != "" {
			nextType = name
			nextKind = "protocol"
			i.types = append(i.types, swiftTypeSpan{
				name:      name,
				kind:      "protocol",
				nameLine:  shared.NodeLine(nameNode),
				startLine: shared.NodeLine(node),
				endLine:   shared.NodeEndLine(node),
				bases:     swiftTreeInheritance(node, source),
			})
		}
	case "function_declaration", "protocol_function_declaration":
		nameNode := node.ChildByFieldName("name")
		name := strings.TrimSpace(shared.NodeText(nameNode, source))
		if name != "" {
			i.functions = append(i.functions, swiftFunctionSpan{
				name:         name,
				classContext: currentType,
				scopeKind:    currentKind,
				args:         swiftTreeFunctionArgs(node, source),
				startLine:    shared.NodeLine(node),
				endLine:      shared.NodeEndLine(node),
			})
		}
	case "init_declaration":
		i.functions = append(i.functions, swiftFunctionSpan{
			name:         "init",
			classContext: currentType,
			scopeKind:    currentKind,
			args:         swiftTreeFunctionArgs(node, source),
			startLine:    shared.NodeLine(node),
			endLine:      shared.NodeEndLine(node),
		})
	}

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		i.collect(&child, source, nextType, nextKind)
	}
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
			base := strings.TrimSpace(shared.NodeText(inherited, source))
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

func swiftTreeFunctionArgs(node *tree_sitter.Node, source []byte) []string {
	args := make([]string, 0, 2)
	for _, child := range swiftNamedChildren(node) {
		child := child
		if child.Kind() != "parameter" {
			continue
		}
		name := strings.TrimSpace(shared.NodeText(child.ChildByFieldName("name"), source))
		if name != "" && name != "_" {
			args = append(args, name)
		}
	}
	if len(args) == 0 {
		return nil
	}
	return args
}

func swiftNamedChildren(node *tree_sitter.Node) []tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	return node.NamedChildren(cursor)
}

func (i swiftSyntaxIndex) typeAtNameLine(name string, line int) swiftTypeSpan {
	for _, typ := range i.types {
		if typ.name == name && typ.nameLine == line {
			return typ
		}
	}
	return swiftTypeSpan{}
}

func (i swiftSyntaxIndex) functionAtStartLine(name string, line int) swiftFunctionSpan {
	for _, fn := range i.functions {
		if fn.name == name && fn.startLine == line {
			return fn
		}
	}
	return swiftFunctionSpan{}
}

func (i swiftSyntaxIndex) typeNameAtLineOr(current string, line int) string {
	if current != "" {
		return current
	}
	for index := len(i.types) - 1; index >= 0; index-- {
		typ := i.types[index]
		if typ.kind == "protocol" {
			continue
		}
		if line >= typ.startLine && line <= typ.endLine {
			return typ.name
		}
	}
	return ""
}

func (i swiftSyntaxIndex) typeConformances() map[string]map[string]struct{} {
	values := make(map[string]map[string]struct{}, len(i.types))
	for _, typ := range i.types {
		values[typ.name] = swiftStringSet(typ.bases)
	}
	return values
}

func (i swiftSyntaxIndex) protocolMethods() map[string]map[string]struct{} {
	values := make(map[string]map[string]struct{})
	for _, fn := range i.functions {
		if fn.scopeKind != "protocol" || fn.classContext == "" {
			continue
		}
		if values[fn.classContext] == nil {
			values[fn.classContext] = make(map[string]struct{})
		}
		values[fn.classContext][fn.name] = struct{}{}
	}
	return values
}
