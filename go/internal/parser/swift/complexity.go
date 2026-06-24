// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package swift

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// swiftComplexitySet declares the Swift tree-sitter node kinds that count as
// McCabe decision points. Swift exposes short-circuit boolean operators as
// dedicated node kinds (conjunction_expression for `&&`, disjunction_expression
// for `||`) rather than operator tokens inside a binary expression, so they are
// branch kinds here instead of BooleanOperators. Each switch_entry is an arm;
// the `default` arm reuses switch_entry but carries a default_keyword child, so
// the shared catch-all check keeps it from adding a decision.
var swiftComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if_statement",
		"guard_statement",
		"for_statement",
		"while_statement",
		"repeat_while_statement",
		"switch_entry",
		"catch_block",
		"ternary_expression",
		"conjunction_expression",
		"disjunction_expression",
	},
	[]string{"function_declaration", "init_declaration", "lambda_literal", "closure_expression"},
	nil,
	nil,
	// switch_entry covers both case and default arms; the default arm is the
	// implicit else and must not add a decision point.
	[]string{"switch_entry"},
)

// swiftCyclomaticComplexity returns the McCabe cyclomatic complexity for the
// function or initializer subtree rooted at node.
func swiftCyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, swiftComplexitySet)
}
