package golang

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

// goFunctionLiteralIsCompositeElement keeps registry-style function literals as
// reachability evidence without treating assigned local closures as roots.
func goFunctionLiteralIsCompositeElement(node *tree_sitter.Node) bool {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "composite_literal":
			return true
		case "function_declaration", "method_declaration", "func_literal", "short_var_declaration", "assignment_statement", "var_spec":
			return false
		}
	}
	return false
}
