package golang

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

// goFunctionLiteralIsCompositeElement keeps registry-style function literals as
// reachability evidence without treating assigned local closures as roots. The
// lookup parameter is required so the ancestor walk is O(depth) instead of
// O(depth^2) via tree-sitter's root-walking Node.Parent(); see #161.
func goFunctionLiteralIsCompositeElement(node *tree_sitter.Node, lookup *goParentLookup) bool {
	for current := lookup.Parent(node); current != nil; current = lookup.Parent(current) {
		switch current.Kind() {
		case "composite_literal":
			return true
		case "function_declaration", "method_declaration", "func_literal", "short_var_declaration", "assignment_statement", "var_spec":
			return false
		}
	}
	return false
}
