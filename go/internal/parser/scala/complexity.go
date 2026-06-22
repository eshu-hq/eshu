package scala

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// scalaComplexitySet declares the Scala tree-sitter node kinds and boolean
// operator tokens that count as McCabe decision points. Scala spells short
// circuit operators as operator_identifier tokens inside an infix_expression,
// so the shared walker matches the operator text rather than a token kind.
var scalaComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if_expression",
		"while_expression",
		"for_expression",
		"case_clause",
	},
	[]string{"function_definition", "lambda_expression"},
	[]string{"infix_expression"},
	[]string{"&&", "||"},
)

func cyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, scalaComplexitySet)
}
