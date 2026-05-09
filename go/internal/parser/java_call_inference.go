package parser

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// javaCallInferredObjectType attaches bounded receiver type evidence for Java
// method calls when the receiver is a local variable, parameter, field, or
// inline constructor expression visible in the parsed source.
func javaCallInferredObjectType(callNode *tree_sitter.Node, source []byte) string {
	if callNode == nil || callNode.Kind() != "method_invocation" {
		return ""
	}
	objectNode := callNode.ChildByFieldName("object")
	if objectNode == nil {
		return ""
	}
	if objectNode.Kind() == "object_creation_expression" {
		return javaObjectCreationTypeName(objectNode, source)
	}
	receiver := strings.TrimSpace(nodeText(objectNode, source))
	if receiver == "" || strings.ContainsAny(receiver, ".()[") {
		return ""
	}
	callLine := nodeLine(callNode)
	if typeName := javaVariableTypeBefore(javaCallInferenceScope(callNode), receiver, source, callLine); typeName != "" {
		return typeName
	}
	return javaFieldTypeBefore(javaEnclosingClassNode(callNode), receiver, source, callLine)
}

func javaCallInferenceScope(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "method_declaration", "constructor_declaration", "lambda_expression":
			return current
		}
	}
	return nil
}

func javaEnclosingClassNode(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "interface_declaration", "enum_declaration":
			return current
		}
	}
	return nil
}

func javaVariableTypeBefore(scope *tree_sitter.Node, receiver string, source []byte, beforeLine int) string {
	if scope == nil || receiver == "" {
		return ""
	}
	var matched string
	walkNamed(scope, func(node *tree_sitter.Node) {
		if matched != "" {
			return
		}
		switch node.Kind() {
		case "formal_parameter":
			if javaParameterName(node, source) == receiver {
				matched = javaDeclaredTypeName(node, source)
			}
		case "local_variable_declaration":
			if nodeLine(node) >= beforeLine {
				return
			}
			if javaDeclarationHasVariable(node, receiver, source) {
				matched = javaDeclaredTypeName(node, source)
			}
		}
	})
	return matched
}

func javaFieldTypeBefore(classNode *tree_sitter.Node, receiver string, source []byte, beforeLine int) string {
	if classNode == nil || receiver == "" {
		return ""
	}
	var matched string
	walkNamed(classNode, func(node *tree_sitter.Node) {
		if matched != "" || node.Kind() != "field_declaration" || nodeLine(node) >= beforeLine {
			return
		}
		if javaDeclarationHasVariable(node, receiver, source) {
			matched = javaDeclaredTypeName(node, source)
		}
	})
	return matched
}

func javaDeclarationHasVariable(node *tree_sitter.Node, receiver string, source []byte) bool {
	if node == nil || receiver == "" {
		return false
	}
	found := false
	walkNamed(node, func(child *tree_sitter.Node) {
		if found || child.Kind() != "variable_declarator" {
			return
		}
		name := strings.TrimSpace(nodeText(child.ChildByFieldName("name"), source))
		found = name == receiver
	})
	return found
}

func javaParameterName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return strings.TrimSpace(nodeText(nameNode, source))
	}
	var name string
	walkNamed(node, func(child *tree_sitter.Node) {
		if name != "" || child.Kind() != "identifier" {
			return
		}
		name = strings.TrimSpace(nodeText(child, source))
	})
	return name
}

func javaDeclaredTypeName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if typeNode := node.ChildByFieldName("type"); typeNode != nil {
		return javaTypeLeafName(nodeText(typeNode, source))
	}
	var typeName string
	walkNamed(node, func(child *tree_sitter.Node) {
		if typeName != "" {
			return
		}
		switch child.Kind() {
		case "type_identifier", "scoped_type_identifier", "generic_type", "integral_type", "floating_point_type", "boolean_type":
			typeName = javaTypeLeafName(nodeText(child, source))
		}
	})
	return typeName
}

func javaObjectCreationTypeName(node *tree_sitter.Node, source []byte) string {
	if node == nil || node.Kind() != "object_creation_expression" {
		return ""
	}
	if typeNode := node.ChildByFieldName("type"); typeNode != nil {
		return javaTypeLeafName(nodeText(typeNode, source))
	}
	if typeNode := javaFirstTypeIdentifier(node); typeNode != nil {
		return javaTypeLeafName(nodeText(typeNode, source))
	}
	return ""
}

func javaTypeLeafName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "? extends ")
	value = strings.TrimPrefix(value, "? super ")
	if value == "" {
		return ""
	}
	if cut := strings.IndexAny(value, "<["); cut >= 0 {
		value = strings.TrimSpace(value[:cut])
	}
	if idx := strings.LastIndex(value, "."); idx >= 0 {
		value = strings.TrimSpace(value[idx+1:])
	}
	return strings.TrimSpace(value)
}
