package csharp

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// csharpComplexitySet declares the C# tree-sitter node kinds and boolean
// operator tokens that count as McCabe decision points. Each switch_section is
// one reachable case group, and conditional_expression covers the ternary.
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
)

func cyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, csharpComplexitySet)
}
