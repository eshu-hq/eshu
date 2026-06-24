// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package groovy

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// groovyComplexitySet declares the Groovy tree-sitter node kinds and boolean
// operator tokens that count as McCabe decision points. Groovy mirrors Java
// control flow: if/for/while statements, switch_case arms, catch handlers, and
// the ternary expression. The switch catch-all is a distinct switch_default
// node, so it is never counted.
var groovyComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if_statement",
		"for_statement",
		"while_statement",
		"switch_case",
		"catch_clause",
		"ternary_expression",
	},
	[]string{"method_declaration", "closure", "class_declaration"},
	[]string{"binary_expression"},
	[]string{"&&", "||"},
	nil,
)

// groovyCyclomaticComplexity returns the McCabe cyclomatic complexity for the
// method subtree rooted at node.
func groovyCyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, groovyComplexitySet)
}
