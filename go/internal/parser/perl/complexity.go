// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perl

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// perlComplexitySet declares the Perl tree-sitter node kinds and boolean
// operator tokens that count as McCabe decision points. The grammar folds
// keyword pairs into one kind: conditional_statement covers if/unless,
// loop_statement covers while/until, and for_statement covers for/foreach.
// Perl's pervasive postfix statement modifiers (`return 1 if $x`,
// `print $_ foreach @xs`) are separate expression kinds, each a real branch.
//
// Perl's given/when switch is experimental and the tree-sitter grammar parses
// it into ERROR nodes, so when arms are deliberately not scored; a given/when
// body adds no decision points until the grammar supports it.
var perlComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"conditional_statement",
		"loop_statement",
		"for_statement",
		"conditional_expression",
		"postfix_conditional_expression",
		"postfix_for_expression",
	},
	[]string{"subroutine_declaration_statement", "anonymous_subroutine_expression"},
	[]string{"binary_expression"},
	[]string{"&&", "||", "and", "or"},
	nil,
)

// perlCyclomaticComplexity returns the McCabe cyclomatic complexity for the
// subroutine or phaser-block subtree rooted at node.
func perlCyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, perlComplexitySet)
}
