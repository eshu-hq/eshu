package parser

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptTypeAliasItem(
	node *tree_sitter.Node,
	nameNode *tree_sitter.Node,
	source []byte,
	lang string,
) map[string]any {
	item := map[string]any{
		"name":            nodeText(nameNode, source),
		"line_number":     nodeLine(nameNode),
		"end_line":        nodeEndLine(node),
		"lang":            lang,
		"type_parameters": javaScriptTypeParameters(node, source),
	}
	if aliasKind := javaScriptTypeAliasKind(node); aliasKind != "" {
		item["type_alias_kind"] = aliasKind
	}
	return item
}

func javaScriptFunctionSemantics(node *tree_sitter.Node, lang string) map[string]any {
	if lang != "tsx" || !javaScriptContainsJSXFragmentShorthand(node) {
		return nil
	}
	return map[string]any{
		"jsx_fragment_shorthand": true,
	}
}

func javaScriptTypeAliasKind(node *tree_sitter.Node) string {
	if node == nil {
		return ""
	}
	valueNode := node.ChildByFieldName("value")
	if valueNode == nil {
		return ""
	}
	if javaScriptNodeContainsKind(valueNode, "conditional_type") {
		return "conditional_type"
	}
	if javaScriptNodeContainsKind(valueNode, "mapped_type_clause") {
		return "mapped_type"
	}
	return ""
}

func javaScriptNamespaceModuleItem(
	node *tree_sitter.Node,
	source []byte,
	lang string,
) map[string]any {
	if node == nil {
		return nil
	}
	nameNode := node.ChildByFieldName("name")
	name := strings.TrimSpace(nodeText(nameNode, source))
	if name == "" {
		return nil
	}
	return map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"end_line":    nodeEndLine(node),
		"lang":        lang,
		"module_kind": "namespace",
	}
}

func javaScriptNodeContainsKind(node *tree_sitter.Node, kind string) bool {
	if node == nil {
		return false
	}
	if node.Kind() == kind {
		return true
	}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		child := children[i]
		if javaScriptNodeContainsKind(&child, kind) {
			return true
		}
	}
	return false
}
