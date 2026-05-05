package parser

import (
	"fmt"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func pythonLambdaAssignmentItem(
	node *tree_sitter.Node,
	source []byte,
	options Options,
) (map[string]any, bool) {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	if left == nil || right == nil || right.Kind() != "lambda" {
		return nil, false
	}

	name := pythonLambdaAssignmentTargetName(left, source)
	if strings.TrimSpace(name) == "" {
		return nil, false
	}

	item := pythonLambdaFunctionItem(
		name,
		left,
		node,
		right,
		right.ChildByFieldName("parameters"),
		source,
		options,
	)
	return item, true
}

func pythonAnonymousLambdaItem(
	node *tree_sitter.Node,
	source []byte,
	options Options,
) (map[string]any, bool) {
	if node == nil || node.Kind() != "lambda" {
		return nil, false
	}

	parent := node.Parent()
	if parent != nil && parent.Kind() == "assignment" {
		right := parent.ChildByFieldName("right")
		if right != nil && right == node {
			return nil, false
		}
	}

	name := fmt.Sprintf(
		"lambda@%d_%d",
		nodeLine(node),
		int(node.StartPosition().Column)+1,
	)
	return pythonLambdaFunctionItem(
		name,
		node,
		node,
		node,
		node.ChildByFieldName("parameters"),
		source,
		options,
	), true
}

func pythonLambdaFunctionItem(
	name string,
	lineNode *tree_sitter.Node,
	endNode *tree_sitter.Node,
	sourceNode *tree_sitter.Node,
	parametersNode *tree_sitter.Node,
	source []byte,
	options Options,
) map[string]any {
	item := map[string]any{
		"name":                  name,
		"line_number":           nodeLine(lineNode),
		"end_line":              nodeEndLine(endNode),
		"args":                  pythonParameterNames(parametersNode, source),
		"decorators":            []string{},
		"lang":                  "python",
		"async":                 false,
		"cyclomatic_complexity": 1,
		"semantic_kind":         "lambda",
	}
	if options.IndexSource {
		item["source"] = nodeText(sourceNode, source)
	}
	return item
}

func pythonLambdaAssignmentTargetName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	switch node.Kind() {
	case "identifier", "attribute":
		return nodeText(node, source)
	default:
		return ""
	}
}

func pythonParameterNames(parametersNode *tree_sitter.Node, source []byte) []string {
	if parametersNode == nil {
		return nil
	}

	args := make([]string, 0)
	cursor := parametersNode.Walk()
	defer cursor.Close()
	for _, child := range parametersNode.NamedChildren(cursor) {
		child := child
		arg := pythonParameterName(&child, source)
		if arg == "" {
			continue
		}
		args = append(args, arg)
	}
	return args
}

func pythonParameterName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier":
		return nodeText(node, source)
	case "default_parameter", "typed_parameter", "typed_default_parameter":
		return nodeText(node.ChildByFieldName("name"), source)
	case "list_splat_pattern", "dictionary_splat_pattern":
		return nodeText(node, source)
	default:
		return ""
	}
}
