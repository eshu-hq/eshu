package parser

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// javaScriptNewExpressionVariableTypes tracks local variables initialized from
// constructors so later member calls can carry bounded receiver type metadata.
func javaScriptNewExpressionVariableTypes(root *tree_sitter.Node, source []byte) map[string]string {
	typesByVariable := make(map[string]string)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "variable_declarator" {
			return
		}
		nameNode := node.ChildByFieldName("name")
		valueNode := node.ChildByFieldName("value")
		if nameNode == nil || valueNode == nil || valueNode.Kind() != "new_expression" {
			return
		}
		variableName := strings.TrimSpace(nodeText(nameNode, source))
		constructorName, _ := javaScriptNewExpressionConstructorName(valueNode, source)
		if variableName == "" || constructorName == "" {
			return
		}
		typesByVariable[variableName] = constructorName
	})
	return typesByVariable
}

func javaScriptCallInferredObjectType(
	functionNode *tree_sitter.Node,
	source []byte,
	typesByVariable map[string]string,
) string {
	if functionNode == nil || len(typesByVariable) == 0 || functionNode.Kind() != "member_expression" {
		return ""
	}
	objectNode := functionNode.ChildByFieldName("object")
	if objectNode == nil {
		return ""
	}
	receiver := strings.TrimSpace(nodeText(objectNode, source))
	if receiver == "" || strings.ContainsAny(receiver, ".()[") {
		return ""
	}
	return typesByVariable[receiver]
}

func javaScriptNewExpressionConstructorName(node *tree_sitter.Node, source []byte) (string, string) {
	if node == nil || node.Kind() != "new_expression" {
		return "", ""
	}
	constructorNode := node.ChildByFieldName("constructor")
	constructor := strings.TrimSpace(nodeText(constructorNode, source))
	if constructor == "" {
		constructor = javaScriptNewExpressionConstructorFromText(nodeText(node, source))
	}
	constructor = strings.TrimSpace(constructor)
	if constructor == "" {
		return "", ""
	}
	return javaScriptTrailingConstructorName(constructor), constructor
}

func javaScriptNewExpressionConstructorFromText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "new ")
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	cutAt := len(text)
	for _, marker := range []string{"(", "<", " "} {
		if idx := strings.Index(text, marker); idx >= 0 && idx < cutAt {
			cutAt = idx
		}
	}
	return strings.TrimSpace(text[:cutAt])
}

func javaScriptTrailingConstructorName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ".")
	return strings.TrimSpace(parts[len(parts)-1])
}
