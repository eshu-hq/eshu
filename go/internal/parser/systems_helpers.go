package parser

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func firstNamedDescendant(node *tree_sitter.Node, kinds ...string) *tree_sitter.Node {
	var result *tree_sitter.Node
	walkNamed(node, func(child *tree_sitter.Node) {
		if result != nil {
			return
		}
		for _, kind := range kinds {
			if child.Kind() == kind {
				result = cloneNode(child)
				return
			}
		}
	})
	return result
}

func appendImportFromNode(payload map[string]any, node *tree_sitter.Node, source []byte, lang string) {
	name := strings.Trim(nodeText(node, source), `<>"`)
	if strings.TrimSpace(name) == "" {
		return
	}
	appendBucket(payload, "imports", map[string]any{
		"name":        name,
		"line_number": nodeLine(node),
		"lang":        lang,
	})
}

func appendMacro(payload map[string]any, node *tree_sitter.Node, source []byte, lang string) {
	nameNode := firstNamedDescendant(node, "identifier")
	name := nodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}
	appendBucket(payload, "macros", map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"end_line":    nodeEndLine(node),
		"lang":        lang,
	})
}

func sortSystemsPayload(payload map[string]any, keys ...string) {
	for _, key := range keys {
		sortNamedBucket(payload, key)
	}
}
