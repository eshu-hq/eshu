package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func isJavaScriptFunctionValue(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "function_expression", "arrow_function", "generator_function", "generator_function_declaration":
		return true
	default:
		return false
	}
}

func javaScriptInsideFunction(node *tree_sitter.Node) bool {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "function_declaration", "function_expression", "arrow_function", "method_definition":
			return true
		}
	}
	return false
}

func javaScriptDecorators(node *tree_sitter.Node, source []byte) []string {
	decorators := make([]string, 0)
	for current := node; current != nil; current = current.Parent() {
		cursor := current.Walk()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			if child.Kind() != "decorator" {
				continue
			}
			decorator := strings.TrimSpace(nodeText(&child, source))
			if decorator == "" {
				continue
			}
			decorators = append(decorators, decorator)
		}
		cursor.Close()
		if current.Kind() == "decorated_definition" {
			return decorators
		}
		if current.Parent() == nil || current.Parent().Kind() != "decorated_definition" {
			break
		}
	}
	return decorators
}

func javaScriptTypeParameters(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return []string{}
	}
	typeParametersNode := node.ChildByFieldName("type_parameters")
	if typeParametersNode == nil {
		return []string{}
	}
	return javaScriptTypeParameterNames(nodeText(typeParametersNode, source))
}

func javaScriptCallName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "parenthesized_expression":
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		cursor.Close()
		for i := range children {
			if name := javaScriptCallName(&children[i], source); name != "" {
				return name
			}
		}
	case "identifier":
		return nodeText(node, source)
	case "member_expression":
		property := node.ChildByFieldName("property")
		return nodeText(property, source)
	default:
		return ""
	}
	return ""
}

func javaScriptCallFullName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(nodeText(node, source))
}

func javaScriptJSXComponentName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}

	switch nameNode.Kind() {
	case "identifier", "property_identifier", "jsx_identifier", "type_identifier":
		return strings.TrimSpace(nodeText(nameNode, source))
	case "member_expression", "nested_identifier":
		propertyNode := nameNode.ChildByFieldName("property")
		if propertyNode != nil {
			return strings.TrimSpace(nodeText(propertyNode, source))
		}
		text := strings.TrimSpace(nodeText(nameNode, source))
		if text == "" {
			return ""
		}
		parts := strings.Split(text, ".")
		return strings.TrimSpace(parts[len(parts)-1])
	default:
		return ""
	}
}
