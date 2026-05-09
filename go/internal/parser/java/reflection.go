package java

import (
	"strconv"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// appendJavaReflectionReferences records only statically named reflection
// references. Dynamic class or method names remain unmodeled so the reducer
// does not invent reachability that source text did not prove.
func appendJavaReflectionReferences(payload map[string]any, node *tree_sitter.Node, source []byte) {
	if node == nil || node.Kind() != "method_invocation" {
		return
	}
	if className := javaReflectionClassReference(node, source); className != "" {
		appendBucket(payload, "function_calls", map[string]any{
			"name":            javaTypeLeafName(className),
			"line_number":     nodeLine(node),
			"lang":            "java",
			"call_kind":       "java.reflection_class_reference",
			"reflected_class": className,
		})
	}
	if methodRef := javaReflectionMethodReference(node, source); len(methodRef) > 0 {
		appendBucket(payload, "function_calls", methodRef)
	}
}

func javaReflectionClassReference(node *tree_sitter.Node, source []byte) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}
	name := strings.TrimSpace(nodeText(nameNode, source))
	if name != "forName" && name != "loadClass" {
		return ""
	}
	if name == "forName" && javaCallFullName(node, source) != "Class.forName" {
		return ""
	}
	className := javaReflectionFirstStringArgument(node, source)
	if className == "" || !strings.Contains(className, ".") {
		return ""
	}
	return className
}

func javaReflectionMethodReference(node *tree_sitter.Node, source []byte) map[string]any {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := strings.TrimSpace(nodeText(nameNode, source))
	if name != "getMethod" && name != "getDeclaredMethod" {
		return nil
	}
	objectType := javaReflectionReceiverClassLiteral(node, source)
	if objectType == "" {
		return nil
	}
	arguments := javaArgumentNodes(node)
	if len(arguments) == 0 {
		return nil
	}
	methodName := javaStringLiteralValue(arguments[0], source)
	if methodName == "" {
		return nil
	}
	parameterTypes := make([]string, 0, len(arguments)-1)
	for _, argument := range arguments[1:] {
		parameterTypes = append(parameterTypes, javaClassLiteralTypeName(argument, source))
	}
	item := map[string]any{
		"name":              methodName,
		"line_number":       nodeLine(node),
		"lang":              "java",
		"call_kind":         "java.reflection_method_reference",
		"inferred_obj_type": objectType,
		"argument_count":    len(arguments) - 1,
	}
	if hasNonEmptyString(parameterTypes) {
		item["argument_types"] = parameterTypes
	}
	return item
}

func javaReflectionReceiverClassLiteral(node *tree_sitter.Node, source []byte) string {
	objectNode := node.ChildByFieldName("object")
	if objectNode == nil {
		return ""
	}
	return javaClassLiteralTypeName(objectNode, source)
}

func javaReflectionFirstStringArgument(node *tree_sitter.Node, source []byte) string {
	arguments := javaArgumentNodes(node)
	if len(arguments) == 0 {
		return ""
	}
	return javaStringLiteralValue(arguments[0], source)
}

func javaArgumentNodes(node *tree_sitter.Node) []*tree_sitter.Node {
	argumentsNode := node.ChildByFieldName("arguments")
	if argumentsNode == nil {
		return nil
	}
	var arguments []*tree_sitter.Node
	walkDirectNamed(argumentsNode, func(child *tree_sitter.Node) {
		arguments = append(arguments, child)
	})
	return arguments
}

func javaStringLiteralValue(node *tree_sitter.Node, source []byte) string {
	if node == nil || node.Kind() != "string_literal" {
		return ""
	}
	raw := strings.TrimSpace(nodeText(node, source))
	if raw == "" {
		return ""
	}
	value, err := strconv.Unquote(raw)
	if err != nil {
		return strings.Trim(raw, `"`)
	}
	return strings.TrimSpace(value)
}
