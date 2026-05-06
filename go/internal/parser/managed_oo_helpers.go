package parser

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func appendNamedType(
	payload map[string]any,
	bucket string,
	node *tree_sitter.Node,
	source []byte,
	lang string,
) {
	nameNode := node.ChildByFieldName("name")
	name := nodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}
	appendBucket(payload, bucket, map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"end_line":    nodeEndLine(node),
		"lang":        lang,
	})
}

func appendFunctionWithContext(
	payload map[string]any,
	node *tree_sitter.Node,
	source []byte,
	lang string,
	options Options,
	contextKinds ...string,
) {
	nameNode := node.ChildByFieldName("name")
	name := nodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}

	item := map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"end_line":    nodeEndLine(node),
		"decorators":  []string{},
		"lang":        lang,
	}
	if classContext := nearestNamedAncestor(node, source, contextKinds...); classContext != "" {
		item["class_context"] = classContext
	}
	if options.IndexSource {
		item["source"] = nodeText(node, source)
	}
	appendBucket(payload, "functions", item)
}

func nearestNamedAncestor(node *tree_sitter.Node, source []byte, kinds ...string) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		for _, kind := range kinds {
			if current.Kind() != kind {
				continue
			}
			nameNode := current.ChildByFieldName("name")
			return nodeText(nameNode, source)
		}
	}
	return ""
}

func appendCall(payload map[string]any, nameNode *tree_sitter.Node, source []byte, lang string) {
	name := nodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}
	appendBucket(payload, "function_calls", map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"lang":        lang,
	})
}
