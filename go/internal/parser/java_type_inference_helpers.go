package parser

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaDeclarationHasVariable(node *tree_sitter.Node, receiver string, source []byte) bool {
	if node == nil || receiver == "" {
		return false
	}
	for _, name := range javaDeclarationVariableNames(node, source) {
		if name == receiver {
			return true
		}
	}
	return false
}

func javaDeclarationVariableNames(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	var names []string
	walkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "variable_declarator" {
			return
		}
		name := strings.TrimSpace(nodeText(child.ChildByFieldName("name"), source))
		if name == "" {
			return
		}
		names = append(names, name)
	})
	if len(names) == 0 {
		return nil
	}
	return names
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

func javaLambdaTypedParameters(node *tree_sitter.Node, source []byte) []javaTypedName {
	typeName := javaLambdaClassLiteralType(node, source)
	if typeName == "" {
		return nil
	}
	names := javaLambdaParameterNames(node, source)
	if len(names) == 0 {
		return nil
	}
	return []javaTypedName{{
		name:     names[0],
		typeName: typeName,
		line:     nodeLine(node) - 1,
	}}
}

func javaLambdaClassLiteralType(node *tree_sitter.Node, source []byte) string {
	if node == nil || node.Kind() != "lambda_expression" {
		return ""
	}
	argumentsNode := node.Parent()
	if argumentsNode == nil || argumentsNode.Kind() != "argument_list" {
		return ""
	}
	var previousClassLiteral string
	walkDirectNamed(argumentsNode, func(child *tree_sitter.Node) {
		if child.StartByte() >= node.StartByte() {
			return
		}
		if typeName := javaClassLiteralTypeName(child, source); typeName != "" {
			previousClassLiteral = typeName
		}
	})
	return previousClassLiteral
}

func javaClassLiteralTypeName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	raw := strings.TrimSpace(nodeText(node, source))
	typeName, ok := strings.CutSuffix(raw, ".class")
	if !ok {
		return ""
	}
	return javaTypeLeafName(typeName)
}

func javaLambdaParameterNames(node *tree_sitter.Node, source []byte) []string {
	raw := strings.TrimSpace(nodeText(node, source))
	parameters, _, ok := strings.Cut(raw, "->")
	if !ok {
		return nil
	}
	parameters = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(parameters), ")"), "("))
	if parameters == "" {
		return nil
	}
	parts := strings.Split(parameters, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimSpace(fields[len(fields)-1])
		name = strings.TrimPrefix(name, "@")
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
