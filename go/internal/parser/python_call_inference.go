package parser

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// pythonCallInferredObjectType tracks local variables initialized from simple
// constructor calls before a member call so reducer resolution stays bounded.
func pythonCallInferredObjectType(
	callNode *tree_sitter.Node,
	functionNode *tree_sitter.Node,
	source []byte,
) string {
	if callNode == nil || functionNode == nil || functionNode.Kind() != "attribute" {
		return ""
	}
	receiver := strings.TrimSpace(nodeText(functionNode.ChildByFieldName("object"), source))
	if receiver == "" || strings.ContainsAny(receiver, ".()[") {
		return ""
	}
	if receiver == "self" {
		return pythonEnclosingClassName(callNode, source)
	}
	scope := pythonCallInferenceScope(callNode)
	if scope == nil {
		return ""
	}
	typesByVariable := pythonConstructorAssignmentsBefore(scope, source, nodeLine(callNode))
	return typesByVariable[receiver]
}

func pythonCallInferenceScope(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "function_definition", "module":
			return current
		}
	}
	return nil
}

func pythonConstructorAssignmentsBefore(
	scope *tree_sitter.Node,
	source []byte,
	beforeLine int,
) map[string]string {
	typesByVariable := make(map[string]string)
	walkNamed(scope, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment" || nodeLine(node) >= beforeLine {
			return
		}
		left := node.ChildByFieldName("left")
		right := node.ChildByFieldName("right")
		if left == nil || right == nil || left.Kind() != "identifier" || right.Kind() != "call" {
			return
		}
		variableName := strings.TrimSpace(nodeText(left, source))
		constructorName := strings.TrimSpace(pythonCallFullName(right.ChildByFieldName("function"), source))
		if variableName == "" || constructorName == "" || !pythonLooksLikeConstructor(constructorName) {
			return
		}
		typesByVariable[variableName] = constructorName
	})
	return typesByVariable
}

func pythonLooksLikeConstructor(name string) bool {
	leaf := pythonTrailingName(name)
	if leaf == "" {
		return false
	}
	first := rune(leaf[0])
	return first >= 'A' && first <= 'Z'
}

func pythonTrailingName(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '.' || r == ':' || r == '/' || r == '\\'
	})
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func pythonClassReferenceCallItem(
	functionNode *tree_sitter.Node,
	callNode *tree_sitter.Node,
	source []byte,
) map[string]any {
	if functionNode == nil || callNode == nil || functionNode.Kind() != "attribute" {
		return nil
	}
	receiver := strings.TrimSpace(nodeText(functionNode.ChildByFieldName("object"), source))
	if receiver == "" || !pythonLooksLikeConstructor(receiver) {
		return nil
	}
	return map[string]any{
		"name":        pythonTrailingName(receiver),
		"full_name":   receiver,
		"line_number": nodeLine(callNode),
		"lang":        "python",
		"call_kind":   "python.class_reference",
	}
}
