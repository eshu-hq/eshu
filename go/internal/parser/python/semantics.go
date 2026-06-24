// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func pythonDocstring(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	body := node.ChildByFieldName("body")
	if body == nil {
		if node.Kind() == "module" {
			body = node
		} else {
			return ""
		}
	}
	if body == nil {
		return ""
	}

	cursor := body.Walk()
	defer cursor.Close()

	children := body.NamedChildren(cursor)
	if len(children) == 0 {
		return ""
	}

	child := children[0]
	if child.Kind() != "expression_statement" {
		return ""
	}

	stringNode := firstNamedDescendant(&child, "string", "concatenated_string")
	if stringNode == nil {
		return ""
	}
	return cleanPythonDocstringLiteral(nodeText(stringNode, source))
}

func cleanPythonDocstringLiteral(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	for len(trimmed) > 0 {
		switch trimmed[0] {
		case 'r', 'R', 'u', 'U', 'b', 'B', 'f', 'F':
			trimmed = trimmed[1:]
		default:
			goto prefixDone
		}
	}
prefixDone:
	switch {
	case strings.HasPrefix(trimmed, `"""`) && strings.HasSuffix(trimmed, `"""`) && len(trimmed) >= 6:
		trimmed = trimmed[3 : len(trimmed)-3]
	case strings.HasPrefix(trimmed, `'''`) && strings.HasSuffix(trimmed, `'''`) && len(trimmed) >= 6:
		trimmed = trimmed[3 : len(trimmed)-3]
	case strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`) && len(trimmed) >= 2:
		trimmed = trimmed[1 : len(trimmed)-1]
	case strings.HasPrefix(trimmed, `'`) && strings.HasSuffix(trimmed, `'`) && len(trimmed) >= 2:
		trimmed = trimmed[1 : len(trimmed)-1]
	}

	return strings.TrimSpace(trimmed)
}

// pythonComplexitySet declares the Python tree-sitter node kinds and boolean
// operator tokens that count as McCabe decision points. Python exposes `and`
// and `or` as tokens inside a boolean_operator node, so that kind drives
// short-circuit counting.
var pythonComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if_statement",
		"elif_clause",
		"for_statement",
		"while_statement",
		"except_clause",
		"case_clause",
		"conditional_expression",
	},
	[]string{"class_definition", "function_definition", "lambda"},
	[]string{"boolean_operator"},
	[]string{"and", "or"},
	// case_clause covers every match arm including the catch-all `case _:`; a bare
	// wildcard arm is the implicit else, so it must not add a decision point.
	[]string{"case_clause"},
)

func cyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, pythonComplexitySet)
}

func isNestedDefinition(kind string) bool {
	switch kind {
	case "class_definition", "function_definition", "lambda":
		return true
	default:
		return false
	}
}
