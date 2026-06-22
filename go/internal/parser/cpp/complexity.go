package cpp

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// cppComplexitySet declares the C++ tree-sitter node kinds and boolean operator
// tokens that count as McCabe decision points. Range-based for loops parse as
// for_range_loop, and catch handlers as catch_clause, so both join the loop and
// conditional kinds C++ shares with C.
var cppComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if_statement",
		"for_statement",
		"for_range_loop",
		"while_statement",
		"do_statement",
		"case_statement",
		"catch_clause",
		"conditional_expression",
	},
	[]string{"function_definition", "lambda_expression"},
	[]string{"binary_expression"},
	[]string{"&&", "||"},
	// case_statement covers both `case` and `default`; the default arm is the
	// implicit else, so it must not add a decision point.
	[]string{"case_statement"},
)

func cyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, cppComplexitySet)
}
