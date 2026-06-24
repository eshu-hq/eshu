// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package csharp

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// csharpComplexitySet declares the C# tree-sitter node kinds and boolean
// operator tokens that count as McCabe decision points. Each `case` switch_section
// is one reachable case group, and conditional_expression covers the ternary. A
// switch_section whose label is `default` is excluded as the implicit else.
var csharpComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if_statement",
		"for_statement",
		"foreach_statement",
		"while_statement",
		"do_statement",
		"switch_section",
		"catch_clause",
		"conditional_expression",
	},
	[]string{"method_declaration", "constructor_declaration", "local_function_statement", "lambda_expression"},
	[]string{"binary_expression"},
	[]string{"&&", "||"},
	// switch_section covers both `case` and `default` groups; the default group
	// is the implicit else, so it must not add a decision point.
	[]string{"switch_section"},
)

func cyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, csharpComplexitySet)
}
