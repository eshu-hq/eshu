// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dart

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// dartComplexitySet declares the Dart tree-sitter node kinds that count as
// McCabe decision points. Dart exposes short-circuit boolean operators as
// dedicated node kinds (logical_and_expression for `&&`, logical_or_expression
// for `||`) rather than operator tokens, so they are branch kinds here. Each
// switch_statement_case is an arm; the catch-all is a distinct
// switch_statement_default node and is therefore never counted.
var dartComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if_statement",
		"for_statement",
		"while_statement",
		"do_statement",
		"switch_statement_case",
		"switch_expression_case",
		"catch_clause",
		"conditional_expression",
		"if_element",
		"logical_and_expression",
		"logical_or_expression",
	},
	[]string{"function_expression", "function_signature", "lambda_expression"},
	nil,
	nil,
	nil,
)

// dartCyclomaticComplexity returns the McCabe cyclomatic complexity for a Dart
// callable. The Dart grammar models a function as a signature node followed by a
// sibling body node, so the body subtree holds the control flow; bodyNode is
// that sibling (it may equal signatureNode for expression-bodied members that
// the grammar nests differently). When bodyNode is nil only the signature is
// scored, yielding the straight-line base of 1.
func dartCyclomaticComplexity(signatureNode *tree_sitter.Node, bodyNode *tree_sitter.Node, source []byte) int {
	if bodyNode == nil || bodyNode == signatureNode {
		return shared.CyclomaticComplexity(signatureNode, source, dartComplexitySet)
	}
	// Score the body subtree where the branches live. The signature itself is
	// straight-line, so the body's count already includes the base of 1.
	return shared.CyclomaticComplexity(bodyNode, source, dartComplexitySet)
}
