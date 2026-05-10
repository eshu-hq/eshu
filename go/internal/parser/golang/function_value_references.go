package golang

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goFunctionValueReferenceCalls(
	root *tree_sitter.Node,
	source []byte,
	localNameBindings []goLocalNameBinding,
) []map[string]any {
	if root == nil {
		return nil
	}

	calls := make([]map[string]any, 0)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "identifier" || !goFunctionValueReferenceContext(node) {
			return
		}
		name := strings.TrimSpace(nodeText(node, source))
		if name == "" || name == "_" {
			return
		}
		if goNameIsLocallyBound(name, nodeLine(node), localNameBindings) {
			return
		}
		calls = append(calls, map[string]any{
			"name":        name,
			"full_name":   name,
			"line_number": nodeLine(node),
			"call_kind":   "go.function_value_reference",
			"lang":        "go",
		})
	})
	return calls
}

func goFunctionValueReferenceContext(node *tree_sitter.Node) bool {
	parent := node.Parent()
	if parent == nil {
		return false
	}
	switch parent.Kind() {
	case "call_expression":
		return false
	case "selector_expression", "qualified_type", "field_declaration", "parameter_declaration":
		return false
	case "short_var_declaration", "assignment_statement", "var_spec":
		return goNodeMatchesField(parent, node, "right") || goNodeMatchesField(parent, node, "value")
	case "literal_element":
		return true
	case "keyed_element":
		return goNodeMatchesField(parent, node, "value")
	default:
		return goFunctionValueReferenceContext(parent)
	}
}

func goNodeMatchesField(parent *tree_sitter.Node, child *tree_sitter.Node, fieldName string) bool {
	if parent == nil || child == nil {
		return false
	}
	fieldNode := parent.ChildByFieldName(fieldName)
	if fieldNode == nil {
		return false
	}
	if goSameNodeRange(fieldNode, child) {
		return true
	}
	for current := child.Parent(); current != nil; current = current.Parent() {
		if goSameNodeRange(fieldNode, current) {
			return true
		}
		if goSameNodeRange(current, parent) {
			break
		}
	}
	return false
}

// goNameIsLocallyBound reports whether name is shadowed at line by a local
// binding in the same lexical scope.
func goNameIsLocallyBound(name string, line int, bindings []goLocalNameBinding) bool {
	name = strings.TrimSpace(name)
	if name == "" || line <= 0 {
		return false
	}
	for _, binding := range bindings {
		if binding.variable != name ||
			binding.line > line ||
			line < binding.scopeStart ||
			line > binding.scopeEnd {
			continue
		}
		return true
	}
	return false
}

func goSameNodeRange(left *tree_sitter.Node, right *tree_sitter.Node) bool {
	if left == nil || right == nil {
		return false
	}
	return left.Kind() == right.Kind() &&
		left.StartByte() == right.StartByte() &&
		left.EndByte() == right.EndByte()
}
