// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goInsideFunction(node *tree_sitter.Node, lookup *goParentLookup) bool {
	for current := lookup.Parent(node); current != nil; current = lookup.Parent(current) {
		switch current.Kind() {
		case "function_declaration", "method_declaration", "func_literal":
			return true
		}
	}
	return false
}

func goVariableNames(node *tree_sitter.Node, source []byte) []map[string]any {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return []map[string]any{{
		"name":        nodeText(nameNode, source),
		"line_number": nodeLine(nameNode),
		"end_line":    nodeEndLine(node),
		"lang":        "go",
	}}
}

func goShortVariableNames(node *tree_sitter.Node, source []byte) []map[string]any {
	left := node.ChildByFieldName("left")
	if left == nil {
		return nil
	}

	var items []map[string]any
	cursor := left.Walk()
	defer cursor.Close()
	for _, child := range left.NamedChildren(cursor) {
		child := child
		if child.Kind() != "identifier" {
			continue
		}
		items = append(items, map[string]any{
			"name":        nodeText(&child, source),
			"line_number": nodeLine(&child),
			"end_line":    nodeEndLine(node),
			"lang":        "go",
		})
	}
	return items
}

func goDocstring(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	lines := strings.Split(string(source), "\n")
	startLine := nodeLine(node) - 2
	if startLine < 0 || startLine >= len(lines) {
		return ""
	}

	comments := make([]string, 0)
	for index := startLine; index >= 0; index-- {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" {
			if len(comments) == 0 {
				return ""
			}
			break
		}
		if strings.HasPrefix(trimmed, "//") {
			comments = append([]string{strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))}, comments...)
			continue
		}
		if strings.HasPrefix(trimmed, "/*") && strings.HasSuffix(trimmed, "*/") {
			comments = append([]string{strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "/*"), "*/"))}, comments...)
			continue
		}
		break
	}

	return strings.TrimSpace(strings.Join(comments, "\n"))
}

func goReceiverContext(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	receiver := node.ChildByFieldName("receiver")
	if receiver == nil {
		return ""
	}

	typeNode := firstNamedDescendant(
		receiver,
		"type_identifier",
		"qualified_type",
		"generic_type",
		"pointer_type",
		"array_type",
		"slice_type",
	)
	if typeNode == nil {
		return ""
	}

	return goNormalizeTypeName(nodeText(typeNode, source))
}
