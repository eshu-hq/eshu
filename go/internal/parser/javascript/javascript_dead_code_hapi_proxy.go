package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptIsHapiProxyCallback(node *tree_sitter.Node, name string, source []byte) bool {
	if node == nil || node.Kind() != "pair" || !javaScriptIsHapiProxyCallbackName(name) {
		return false
	}
	if !isJavaScriptFunctionValue(node.ChildByFieldName("value")) {
		return false
	}
	objectNode := node.Parent()
	if objectNode == nil || objectNode.Kind() != "object" {
		return false
	}
	argumentsNode := objectNode.Parent()
	if argumentsNode == nil || argumentsNode.Kind() != "arguments" {
		return false
	}
	callNode := argumentsNode.Parent()
	if callNode == nil || callNode.Kind() != "call_expression" {
		return false
	}
	functionNode := callNode.ChildByFieldName("function")
	_, property, ok := javaScriptMemberBaseAndProperty(functionNode, source)
	return ok && strings.EqualFold(property, "proxy")
}

func javaScriptIsHapiProxyCallbackName(name string) bool {
	switch strings.TrimSpace(name) {
	case "mapUri", "onResponse":
		return true
	default:
		return false
	}
}
