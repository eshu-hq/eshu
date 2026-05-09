package javascript

import (
	"strconv"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var javaScriptBuiltinTypeNames = map[string]struct{}{
	"Array":     {},
	"Boolean":   {},
	"Date":      {},
	"Error":     {},
	"Map":       {},
	"Number":    {},
	"Object":    {},
	"Promise":   {},
	"Readonly":  {},
	"Record":    {},
	"Set":       {},
	"String":    {},
	"boolean":   {},
	"never":     {},
	"null":      {},
	"number":    {},
	"object":    {},
	"string":    {},
	"symbol":    {},
	"undefined": {},
	"unknown":   {},
	"void":      {},
}

func appendJavaScriptTypeReferenceCalls(payload map[string]any, root *tree_sitter.Node, source []byte, lang string) {
	switch lang {
	case "typescript", "tsx":
	default:
		return
	}
	seen := make(map[string]struct{})
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "type_annotation", "type_arguments", "extends_type_clause", "implements_clause", "as_expression", "type_assertion", "satisfies_expression":
			appendJavaScriptTypeReferencesFromNode(payload, node, source, lang, seen)
		}
	})
}

func appendJavaScriptTypeReferencesFromNode(
	payload map[string]any,
	node *tree_sitter.Node,
	source []byte,
	lang string,
	seen map[string]struct{},
) {
	walkNamed(node, func(child *tree_sitter.Node) {
		switch child.Kind() {
		case "type_identifier", "nested_type_identifier", "scoped_type_identifier":
		default:
			return
		}
		fullName := strings.TrimSpace(nodeText(child, source))
		name := javaScriptTypeReferenceLeafName(fullName)
		if name == "" || javaScriptIsBuiltinTypeName(name) {
			return
		}
		key := fullName + "|" + strconv.Itoa(nodeLine(child))
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		appendBucket(payload, "function_calls", map[string]any{
			"name":        name,
			"full_name":   fullName,
			"call_kind":   "typescript.type_reference",
			"line_number": nodeLine(child),
			"lang":        lang,
		})
	})
}

func javaScriptTypeReferenceLeafName(fullName string) string {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return ""
	}
	fields := strings.FieldsFunc(fullName, func(r rune) bool {
		return r == '.' || r == ':'
	})
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(fields[len(fields)-1])
}

func javaScriptIsBuiltinTypeName(name string) bool {
	_, ok := javaScriptBuiltinTypeNames[strings.TrimSpace(name)]
	return ok
}
