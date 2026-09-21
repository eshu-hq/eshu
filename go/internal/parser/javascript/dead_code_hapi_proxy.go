// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptIsHapiProxyCallback(node *tree_sitter.Node, name string, source []byte, parents *javaScriptParentLookup) bool {
	if node == nil || node.Kind() != "pair" || !javaScriptIsHapiProxyCallbackName(name) {
		return false
	}
	if !isJavaScriptFunctionValue(node.ChildByFieldName("value")) {
		return false
	}
	objectNode := parents.parent(node)
	if objectNode == nil || objectNode.Kind() != "object" {
		return false
	}
	argumentsNode := parents.parent(objectNode)
	if argumentsNode == nil || argumentsNode.Kind() != "arguments" {
		return false
	}
	callNode := parents.parent(argumentsNode)
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
