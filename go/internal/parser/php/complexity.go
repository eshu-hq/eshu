// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// phpComplexitySet declares the PHP tree-sitter node kinds and boolean operator
// tokens that count as McCabe decision points. Each elseif chains a new
// condition (else_if_clause), each switch case and each PHP 8 match arm is a
// branch, and catch handlers count. The switch `default_statement` and the
// `match_default_expression` are distinct node kinds, so the catch-all is never
// counted. The plain `else_clause` carries no condition and is likewise absent.
var phpComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if_statement",
		"else_if_clause",
		"for_statement",
		"foreach_statement",
		"while_statement",
		"do_statement",
		"case_statement",
		"match_conditional_expression",
		"catch_clause",
		"conditional_expression",
	},
	[]string{
		"function_definition",
		"method_declaration",
		"anonymous_function_creation_expression",
		"arrow_function",
		"class_declaration",
		"interface_declaration",
		"trait_declaration",
	},
	[]string{"binary_expression"},
	[]string{"&&", "||", "and", "or"},
	nil,
)

// phpCyclomaticComplexity returns the McCabe cyclomatic complexity for the
// function or method subtree rooted at node.
func phpCyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, phpComplexitySet)
}
