package rust

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// rustComplexitySet declares the Rust tree-sitter node kinds and boolean
// operator tokens that count as McCabe decision points. Rust control flow is
// expression-based, so if/while/for/loop and each match arm are the branches.
var rustComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if_expression",
		"for_expression",
		"while_expression",
		"loop_expression",
		"match_arm",
	},
	[]string{"function_item", "closure_expression"},
	[]string{"binary_expression"},
	[]string{"&&", "||"},
	// match_arm covers every arm including the catch-all `_`; a bare unguarded
	// wildcard arm is the implicit else, so it must not add a decision point.
	[]string{"match_arm"},
)

func cyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, rustComplexitySet)
}
