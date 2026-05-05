package parser

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func pythonAnnotatedAssignmentItem(node *tree_sitter.Node, source []byte) (map[string]any, bool) {
	if node == nil || node.Kind() != "assignment" {
		return nil, false
	}

	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		return nil, false
	}

	left := node.ChildByFieldName("left")
	if left == nil {
		return nil, false
	}

	name := strings.TrimSpace(pythonCallFullName(left, source))
	if name == "" {
		return nil, false
	}

	item := map[string]any{
		"name":            name,
		"line_number":     nodeLine(node),
		"type":            pythonNormalizedAnnotation(nodeText(typeNode, source)),
		"annotation_kind": "assignment",
		"lang":            "python",
	}
	if context := pythonAnnotatedAssignmentContext(node, source); context != "" {
		item["context"] = context
	}
	return item, true
}

func pythonAnnotatedAssignmentContext(node *tree_sitter.Node, source []byte) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_definition":
			nameNode := current.ChildByFieldName("name")
			return strings.TrimSpace(nodeText(nameNode, source))
		case "function_definition", "lambda", "module":
			return ""
		}
	}
	return ""
}
