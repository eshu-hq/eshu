package c

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// cComplexitySet declares the C tree-sitter node kinds and boolean operator
// tokens that count as McCabe decision points. The set drives the shared
// cyclomatic complexity walker so C ranks alongside every other language.
var cComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if_statement",
		"for_statement",
		"while_statement",
		"do_statement",
		"case_statement",
		"conditional_expression",
	},
	[]string{"function_definition"},
	[]string{"binary_expression"},
	[]string{"&&", "||"},
)

func cyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, cComplexitySet)
}
