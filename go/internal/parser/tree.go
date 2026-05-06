package parser

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func walkNamed(node *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	if node == nil {
		return
	}

	visit(node)

	cursor := node.Walk()
	defer cursor.Close()

	for _, child := range node.NamedChildren(cursor) {
		child := child
		walkNamed(&child, visit)
	}
}

func nodeText(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return node.Utf8Text(source)
}

func nodeLine(node *tree_sitter.Node) int {
	if node == nil {
		return 1
	}
	return int(node.StartPosition().Row) + 1
}

func nodeEndLine(node *tree_sitter.Node) int {
	if node == nil {
		return 1
	}
	return int(node.EndPosition().Row) + 1
}

func cloneNode(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	cloned := *node
	return &cloned
}

func appendBucket(payload map[string]any, key string, item map[string]any) {
	items, _ := payload[key].([]map[string]any)
	payload[key] = append(items, item)
}

func basePayload(path string, lang string, isDependency bool) map[string]any {
	return map[string]any{
		"path":           path,
		"lang":           lang,
		"is_dependency":  isDependency,
		"functions":      []map[string]any{},
		"classes":        []map[string]any{},
		"variables":      []map[string]any{},
		"imports":        []map[string]any{},
		"function_calls": []map[string]any{},
	}
}
