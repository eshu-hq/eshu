package javascript

import (
	"fmt"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptFunctionValueReferenceCalls(
	node *tree_sitter.Node,
	source []byte,
	lang string,
	commonJSModuleAliases map[string]struct{},
) []map[string]any {
	if node == nil || node.Kind() != "call_expression" {
		return nil
	}
	argumentsNode := node.ChildByFieldName("arguments")
	if argumentsNode == nil {
		return nil
	}
	items := make([]map[string]any, 0, 2)
	seen := make(map[string]struct{})
	walkNamed(argumentsNode, func(child *tree_sitter.Node) {
		if !javaScriptFunctionValueReferenceNode(child) ||
			javaScriptFunctionValueReferenceIsCallCallee(child) ||
			javaScriptFunctionValueReferenceIsHandlerValue(child, source) {
			return
		}
		if child.Parent() != nil && child.Parent().Kind() == "member_expression" {
			return
		}
		item := javaScriptFunctionValueReferenceCall(child, source, lang, commonJSModuleAliases)
		if item == nil {
			return
		}
		fullName, _ := item["full_name"].(string)
		key := fmt.Sprintf("%s|%d", fullName, nodeLine(child))
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		items = append(items, item)
	})
	return items
}

func javaScriptFunctionValueReferenceCall(
	node *tree_sitter.Node,
	source []byte,
	lang string,
	commonJSModuleAliases map[string]struct{},
) map[string]any {
	if !javaScriptFunctionValueReferenceNode(node) {
		return nil
	}
	fullName := rewriteJavaScriptCommonJSModuleExportAliasFullName(nodeText(node, source), commonJSModuleAliases)
	name := javaScriptCallName(node, source)
	if name == "" || fullName == "" {
		return nil
	}
	return map[string]any{
		"name":        name,
		"full_name":   fullName,
		"call_kind":   "javascript.function_value_reference",
		"line_number": nodeLine(node),
		"lang":        lang,
	}
}

func javaScriptReturnValueNode(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil || node.Kind() != "return_statement" {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "identifier", "member_expression":
			return &child
		default:
			return nil
		}
	}
	return nil
}

func javaScriptFunctionValueReferenceIsHandlerValue(node *tree_sitter.Node, source []byte) bool {
	if node == nil {
		return false
	}
	parent := node.Parent()
	if parent == nil || parent.Kind() != "pair" {
		return false
	}
	if !javaScriptNodeSameRange(parent.ChildByFieldName("value"), node) {
		return false
	}
	return strings.TrimSpace(nodeText(parent.ChildByFieldName("key"), source)) == "handler"
}

func javaScriptFunctionValueReferenceNode(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "identifier", "member_expression":
		return true
	default:
		return false
	}
}

func javaScriptFunctionValueReferenceIsCallCallee(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	parent := node.Parent()
	if parent == nil || parent.Kind() != "call_expression" {
		return false
	}
	functionNode := parent.ChildByFieldName("function")
	return javaScriptNodeSameRange(functionNode, node)
}
