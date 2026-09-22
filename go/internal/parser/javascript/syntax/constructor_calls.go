// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// javaScriptNewExpressionVariableTypes tracks local variables initialized from
// constructors so later member calls can carry bounded receiver type metadata.
// CollectNewExpressionVariableType records dst's inferred type for
// one variable_declarator, public_field_definition, field_definition, or
// parameter node, given the already-fully-computed returnTypesByFunction
// lookup (see FunctionReturnTypes). It is a no-op for any other
// node kind, so callers may invoke it on every visited node in a shared
// traversal without pre-filtering. returnTypesByFunction must already be
// complete before this is called for any node: a variable_declarator's
// call_expression value may reference a function declared later in the file.
func CollectNewExpressionVariableType(
	node *tree_sitter.Node,
	source []byte,
	returnTypesByFunction map[string]string,
	dst map[string]string,
) {
	switch node.Kind() {
	case "variable_declarator":
		nameNode := node.ChildByFieldName("name")
		valueNode := node.ChildByFieldName("value")
		if nameNode == nil {
			return
		}
		variableName := strings.TrimSpace(shared.NodeText(nameNode, source))
		if typeName := DeclaredTypeName(node, source); variableName != "" && typeName != "" {
			dst[variableName] = typeName
		}
		if valueNode == nil || valueNode.Kind() != "new_expression" {
			if valueNode != nil && valueNode.Kind() == "call_expression" {
				functionNode := valueNode.ChildByFieldName("function")
				if returnType := returnTypesByFunction[CallName(functionNode, source)]; variableName != "" && returnType != "" {
					dst[variableName] = returnType
				}
			}
			return
		}
		constructorName, _ := NewExpressionConstructorName(valueNode, source)
		if variableName == "" || constructorName == "" {
			return
		}
		dst[variableName] = constructorName
	case "public_field_definition", "field_definition", "required_parameter", "optional_parameter", "formal_parameter":
		variableName := TypedBindingName(node, source)
		typeName := DeclaredTypeName(node, source)
		if variableName == "" || typeName == "" {
			return
		}
		dst[variableName] = typeName
		dst["this."+variableName] = typeName
	}
}

// TypedBindingName returns the bound identifier of a possibly type-annotated
// binding, preferring the grammar's name field and falling back to the text
// before the first colon when that field is unset. The fallback strips a rest
// prefix, an optional marker and any default-value clause, then takes the last
// whitespace-separated token, so a TypeScript parameter property such as
// "public readonly foo: T" yields "foo" rather than its modifiers. It returns an
// empty string when the text carries no annotation.
func TypedBindingName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return strings.TrimSpace(shared.NodeText(nameNode, source))
	}
	raw := strings.TrimSpace(shared.NodeText(node, source))
	if raw == "" || !strings.Contains(raw, ":") {
		return ""
	}
	name, _, _ := strings.Cut(raw, ":")
	if beforeDefault, _, ok := strings.Cut(name, "="); ok {
		name = beforeDefault
	}
	name = strings.TrimSpace(strings.TrimPrefix(name, "..."))
	name = strings.TrimSuffix(name, "?")
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

// FunctionReturnTypes maps each named function, generator, method, and
// function-valued variable under root to its declared return type, skipping any
// declaration missing either a name or an annotation. A repeated name keeps the
// last declaration walked.
func FunctionReturnTypes(root *tree_sitter.Node, source []byte) map[string]string {
	returnTypes := make(map[string]string)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "generator_function_declaration", "method_definition":
			name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
			returnType := DeclaredTypeName(node, source)
			if name != "" && returnType != "" {
				returnTypes[name] = returnType
			}
		case "variable_declarator":
			valueNode := node.ChildByFieldName("value")
			if !IsFunctionValue(valueNode) {
				return
			}
			name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
			returnType := DeclaredTypeName(valueNode, source)
			if name != "" && returnType != "" {
				returnTypes[name] = returnType
			}
		}
	})
	return returnTypes
}

// CallInferredObjectType returns the recorded type of a member call's receiver,
// carrying a constructor binding's type through to its later method calls. The
// receiver text is matched whole, so a compound receiver resolves only when it
// was recorded under exactly that text and is never matched by its base. It
// returns an empty string when functionNode is not a member expression, when
// typesByVariable is empty, and when the receiver has no recorded type.
func CallInferredObjectType(
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
	receiver := strings.TrimSpace(shared.NodeText(objectNode, source))
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

// DeclaredTypeName returns the leaf name of a declaration's type annotation,
// reading the grammar's type field and falling back to a direct type_annotation
// child. It returns an empty string when the declaration carries no annotation.
func DeclaredTypeName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	typeNode := node.ChildByFieldName("type")
	if typeNode != nil {
		return TypeReferenceLeafName(shared.NodeText(typeNode, source))
	}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		child := children[i]
		if child.Kind() == "type_annotation" {
			return TypeReferenceLeafName(shared.NodeText(&child, source))
		}
	}
	return ""
}

// NewExpressionConstructorName returns the trailing constructor name and the full
// constructor expression of a new expression, so "new pkg.Thing()" yields "Thing"
// and "pkg.Thing". It falls back to parsing the node text when the grammar leaves
// the constructor field unset, and returns two empty strings for any other node.
func NewExpressionConstructorName(node *tree_sitter.Node, source []byte) (string, string) {
	if node == nil || node.Kind() != "new_expression" {
		return "", ""
	}
	constructorNode := node.ChildByFieldName("constructor")
	constructor := strings.TrimSpace(shared.NodeText(constructorNode, source))
	if constructor == "" {
		constructor = newExpressionConstructorFromText(shared.NodeText(node, source))
	}
	constructor = strings.TrimSpace(constructor)
	if constructor == "" {
		return "", ""
	}
	return trailingConstructorName(constructor), constructor
}

func newExpressionConstructorFromText(text string) string {
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

func trailingConstructorName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ".")
	return strings.TrimSpace(parts[len(parts)-1])
}
