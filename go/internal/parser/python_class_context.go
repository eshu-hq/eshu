package parser

import (
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func pythonEnclosingClassName(node *tree_sitter.Node, source []byte) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() != "class_definition" {
			continue
		}
		name := strings.TrimSpace(nodeText(current.ChildByFieldName("name"), source))
		if name != "" {
			return name
		}
	}
	return ""
}

func pythonClassBaseNames(node *tree_sitter.Node, source []byte) []string {
	classSource := nodeText(node, source)
	matches := pythonClassHeaderRe.FindStringSubmatch(classSource)
	if len(matches) != 2 {
		return nil
	}
	bases := make([]string, 0)
	for _, argument := range splitPythonParameters(matches[1]) {
		name, _, hasAssignment := strings.Cut(argument, "=")
		if hasAssignment {
			continue
		}
		base := pythonTrailingName(strings.TrimSpace(name))
		if base == "" {
			continue
		}
		bases = appendUniqueString(bases, base)
	}
	slices.Sort(bases)
	return bases
}
