package parser

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// javaScriptNewExpressionVariableTypes tracks local variables initialized from
// constructors so later member calls can carry bounded receiver type metadata.
func javaScriptNewExpressionVariableTypes(root *tree_sitter.Node, source []byte) map[string]string {
	typesByVariable := make(map[string]string)
	returnTypesByFunction := javaScriptFunctionReturnTypes(root, source)
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "variable_declarator":
			nameNode := node.ChildByFieldName("name")
			valueNode := node.ChildByFieldName("value")
			if nameNode == nil {
				return
			}
			variableName := strings.TrimSpace(nodeText(nameNode, source))
			if typeName := javaScriptDeclaredTypeName(node, source); variableName != "" && typeName != "" {
				typesByVariable[variableName] = typeName
			}
			if valueNode == nil || valueNode.Kind() != "new_expression" {
				if valueNode != nil && valueNode.Kind() == "call_expression" {
					functionNode := valueNode.ChildByFieldName("function")
					if returnType := returnTypesByFunction[javaScriptCallName(functionNode, source)]; variableName != "" && returnType != "" {
						typesByVariable[variableName] = returnType
					}
				}
				return
			}
			constructorName, _ := javaScriptNewExpressionConstructorName(valueNode, source)
			if variableName == "" || constructorName == "" {
				return
			}
			typesByVariable[variableName] = constructorName
		case "public_field_definition", "field_definition", "required_parameter", "optional_parameter", "formal_parameter":
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				return
			}
			variableName := strings.TrimSpace(nodeText(nameNode, source))
			typeName := javaScriptDeclaredTypeName(node, source)
			if variableName == "" || typeName == "" {
				return
			}
			typesByVariable[variableName] = typeName
			typesByVariable["this."+variableName] = typeName
		}
	})
	return typesByVariable
}

func javaScriptFunctionReturnTypes(root *tree_sitter.Node, source []byte) map[string]string {
	returnTypes := make(map[string]string)
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "generator_function_declaration", "method_definition":
			name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
			returnType := javaScriptDeclaredTypeName(node, source)
			if name != "" && returnType != "" {
				returnTypes[name] = returnType
			}
		case "variable_declarator":
			valueNode := node.ChildByFieldName("value")
			if !isJavaScriptFunctionValue(valueNode) {
				return
			}
			name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
			returnType := javaScriptDeclaredTypeName(valueNode, source)
			if name != "" && returnType != "" {
				returnTypes[name] = returnType
			}
		}
	})
	return returnTypes
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
	if receiver == "" {
		return ""
	}
	if inferredType := typesByVariable[receiver]; inferredType != "" {
		return inferredType
	}
	if strings.ContainsAny(receiver, ".()[") {
		return ""
	}
	return typesByVariable[receiver]
}

func javaScriptDeclaredTypeName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	typeNode := node.ChildByFieldName("type")
	if typeNode != nil {
		return javaScriptTypeReferenceLeafName(nodeText(typeNode, source))
	}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		child := children[i]
		if child.Kind() == "type_annotation" {
			return javaScriptTypeReferenceLeafName(nodeText(&child, source))
		}
	}
	return ""
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
