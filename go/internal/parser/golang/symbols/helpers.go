// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// InsideFunction reports whether node has an enclosing function_declaration,
// method_declaration, or func_literal ancestor.
func InsideFunction(node *tree_sitter.Node, lookup *ParentLookup) bool {
	for current := lookup.Parent(node); current != nil; current = lookup.Parent(current) {
		switch current.Kind() {
		case "function_declaration", "method_declaration", "func_literal":
			return true
		}
	}
	return false
}

// VariableNames returns the single-element variable payload for a var_spec or
// const_spec node's name field, in the parser's function-call/variable
// payload shape.
func VariableNames(node *tree_sitter.Node, source []byte) []map[string]any {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return []map[string]any{{
		"name":        shared.NodeText(nameNode, source),
		"line_number": shared.NodeLine(nameNode),
		"end_line":    shared.NodeEndLine(node),
		"lang":        "go",
	}}
}

// ShortVariableNames returns one variable payload per identifier on the left
// side of a short_var_declaration node, skipping non-identifier targets.
func ShortVariableNames(node *tree_sitter.Node, source []byte) []map[string]any {
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
			"name":        shared.NodeText(&child, source),
			"line_number": shared.NodeLine(&child),
			"end_line":    shared.NodeEndLine(node),
			"lang":        "go",
		})
	}
	return items
}

// Docstring returns the contiguous line- or block-comment text immediately
// preceding node, joined and trimmed, or "" when node has no such comment.
func Docstring(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	lines := strings.Split(string(source), "\n")
	startLine := shared.NodeLine(node) - 2
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

// ReceiverContext returns the normalized receiver type name for a
// method_declaration node, or "" when node has no receiver.
func ReceiverContext(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	receiver := node.ChildByFieldName("receiver")
	if receiver == nil {
		return ""
	}

	typeNode := FirstNamedDescendant(
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

	return NormalizeTypeName(shared.NodeText(typeNode, source))
}
