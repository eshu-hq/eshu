package ruby

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// rubyComplexitySet declares the Ruby tree-sitter node kinds and boolean
// operator tokens that count as McCabe decision points. Ruby control flow uses
// distinct statement kinds (if/unless/while/until/for) plus elsif and case
// when arms, rescue handlers, and the ternary conditional. The case `else` arm
// is a separate `else` node, not a `when`, so it is never counted. Iterator
// blocks (`each`/`map { ... }`) are method calls, not branches, so block nodes
// are deliberately absent.
var rubyComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if",
		"elsif",
		"unless",
		"while",
		"until",
		"for",
		"when",
		"rescue",
		"conditional",
	},
	[]string{"method", "singleton_method", "lambda"},
	[]string{"binary"},
	[]string{"&&", "||", "and", "or"},
	nil,
)

// rubyCyclomaticComplexity returns the McCabe cyclomatic complexity for the
// method or singleton-method subtree rooted at node.
func rubyCyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, rubyComplexitySet)
}
