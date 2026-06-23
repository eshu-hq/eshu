package kotlin

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// kotlinComplexitySet declares the Kotlin tree-sitter node kinds and boolean
// operator tokens that count as McCabe decision points. Kotlin control flow is
// expression-based: if/when/try are expressions, while loops are statements,
// and each when_entry arm is a branch. The `else` when_entry is the implicit
// else, recognized by the shared catch-all check, so it adds no decision point.
var kotlinComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if_expression",
		"for_statement",
		"while_statement",
		"do_while_statement",
		"when_entry",
		"catch_block",
	},
	[]string{
		"function_declaration",
		"secondary_constructor",
		"anonymous_function",
		"lambda_literal",
		"class_declaration",
		"object_declaration",
	},
	[]string{"binary_expression"},
	[]string{"&&", "||"},
	// when_entry covers every arm including the `else` catch-all; the bare
	// `else` arm is the implicit else and must not add a decision point.
	[]string{"when_entry"},
)

// kotlinCyclomaticComplexity returns the McCabe cyclomatic complexity for the
// function or secondary-constructor subtree rooted at node.
func kotlinCyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, kotlinComplexitySet)
}
