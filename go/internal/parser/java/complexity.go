// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// javaComplexitySet declares the Java tree-sitter node kinds and boolean
// operator tokens that count as McCabe decision points. Each `case` switch_label
// and each catch_clause is a branch, matching how loops and conditionals are
// counted across languages. The `default` switch_label is excluded as the
// implicit else.
var javaComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if_statement",
		"for_statement",
		"enhanced_for_statement",
		"while_statement",
		"do_statement",
		"switch_label",
		"catch_clause",
		"ternary_expression",
	},
	[]string{"method_declaration", "constructor_declaration", "lambda_expression"},
	[]string{"binary_expression"},
	[]string{"&&", "||"},
	// switch_label covers both `case` and `default`; the default arm is the
	// implicit else, so it must not add a decision point.
	[]string{"switch_label"},
)

func cyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, javaComplexitySet)
}
