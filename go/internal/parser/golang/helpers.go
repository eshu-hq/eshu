package golang

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func walkNamed(node *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	shared.WalkNamed(node, visit)
}

func nodeText(node *tree_sitter.Node, source []byte) string {
	return shared.NodeText(node, source)
}

func nodeLine(node *tree_sitter.Node) int {
	return shared.NodeLine(node)
}

func nodeEndLine(node *tree_sitter.Node) int {
	return shared.NodeEndLine(node)
}

func firstNamedDescendant(node *tree_sitter.Node, kinds ...string) *tree_sitter.Node {
	var result *tree_sitter.Node
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if result != nil {
			return
		}
		for _, kind := range kinds {
			if child.Kind() == kind {
				result = shared.CloneNode(child)
				return
			}
		}
	})
	return result
}

// goComplexitySet declares the Go tree-sitter node kinds and boolean operator
// tokens that count as McCabe decision points. It is data so Go complexity stays
// consistent with the shared walker used by every other language.
var goComplexitySet = shared.NewBranchNodeSet(
	[]string{
		"if_statement",
		"for_statement",
		"expression_case",
		"type_case",
		"communication_case",
	},
	[]string{"function_declaration", "method_declaration", "func_literal"},
	[]string{"binary_expression"},
	[]string{"&&", "||"},
)

func cyclomaticComplexity(node *tree_sitter.Node, source []byte) int {
	return shared.CyclomaticComplexity(node, source, goComplexitySet)
}

// walkScopeBindings visits scope and its named descendants but does not
// descend into nested function_declaration, method_declaration, or func_literal
// subtrees. The walker preserves Go lexical scoping for variable-type indices:
// a `var x = ...` inside an inner closure must not leak into the outer
// function's binding table, otherwise call-expression metadata in the outer
// scope would inherit shadowed identifiers from a body that never executes
// there. Visited nodes are passed by pointer; callers that retain a binding
// must copy the node value, because the underlying *tree_sitter.Node points
// at a stack-allocated local inside the recursive walk.
func walkScopeBindings(scope *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	if scope == nil {
		return
	}
	var walk func(*tree_sitter.Node)
	walk = func(current *tree_sitter.Node) {
		if current == nil {
			return
		}
		visit(current)
		cursor := current.Walk()
		defer cursor.Close()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			if isNestedDefinition(child.Kind()) {
				continue
			}
			walk(&child)
		}
	}
	walk(scope)
}

func isNestedDefinition(kind string) bool {
	switch kind {
	case "function_declaration", "method_declaration", "func_literal":
		return true
	default:
		return false
	}
}
