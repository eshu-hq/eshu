package python

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// pythonTypeAnnotations extracts parameter and return type annotations for a
// function_definition straight from the tree-sitter `parameters` and
// `return_type` nodes. Only annotated parameters (typed_parameter /
// typed_default_parameter) and a present return_type contribute entries, which
// matches the prior regex payload while reading the AST instead of the rendered
// signature text. The shared lineNumber is the function definition's start line.
func pythonTypeAnnotations(node *tree_sitter.Node, source []byte, functionName string) []map[string]any {
	parameters := node.ChildByFieldName("parameters")
	returnType := node.ChildByFieldName("return_type")
	if parameters == nil && returnType == nil {
		return nil
	}

	lineNumber := nodeLine(node)
	annotations := make([]map[string]any, 0)
	if parameters != nil {
		cursor := parameters.Walk()
		for _, child := range parameters.NamedChildren(cursor) {
			child := child
			name, annotationType, ok := pythonParameterAnnotation(&child, source)
			if !ok {
				continue
			}
			annotations = append(annotations, map[string]any{
				"name":            name,
				"line_number":     lineNumber,
				"type":            annotationType,
				"annotation_kind": "parameter",
				"context":         functionName,
				"lang":            "python",
			})
		}
		cursor.Close()
	}
	if returnType != nil {
		if normalized := pythonNormalizedAnnotation(nodeText(returnType, source)); normalized != "" {
			annotations = append(annotations, map[string]any{
				"name":            functionName,
				"line_number":     lineNumber,
				"type":            normalized,
				"annotation_kind": "return",
				"context":         functionName,
				"lang":            "python",
			})
		}
	}
	return annotations
}

// pythonParameterAnnotation returns the parameter name and normalized type for a
// typed parameter node. Untyped, positional-only, and keyword-only marker nodes
// carry no `type` child and report as not annotated. Splat parameters
// (*args / **kwargs) report the bare identifier name to match the prior regex.
func pythonParameterAnnotation(node *tree_sitter.Node, source []byte) (string, string, bool) {
	if node == nil {
		return "", "", false
	}
	switch node.Kind() {
	case "typed_parameter", "typed_default_parameter":
	default:
		return "", "", false
	}
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		return "", "", false
	}
	name := pythonParameterIdentifier(pythonTypedParameterName(node), source)
	if name == "" {
		return "", "", false
	}
	annotation := pythonNormalizedAnnotation(nodeText(typeNode, source))
	if annotation == "" {
		return "", "", false
	}
	return name, annotation, true
}

// pythonTypedParameterName resolves the name-bearing child of a typed parameter.
// typed_default_parameter exposes a `name` field; typed_parameter wraps its name
// in an identifier or a splat pattern child without a field label.
func pythonTypedParameterName(node *tree_sitter.Node) *tree_sitter.Node {
	if named := node.ChildByFieldName("name"); named != nil {
		return named
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "identifier", "list_splat_pattern", "dictionary_splat_pattern":
			return &child
		}
	}
	return nil
}

// pythonParameterIdentifier returns the bare identifier text for a parameter
// name node, unwrapping splat patterns so *args / **kwargs become args / kwargs.
func pythonParameterIdentifier(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier":
		return strings.TrimSpace(nodeText(node, source))
	case "list_splat_pattern", "dictionary_splat_pattern":
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			child := child
			if child.Kind() == "identifier" {
				return strings.TrimSpace(nodeText(&child, source))
			}
		}
	}
	return ""
}

func pythonAnnotatedAssignmentItem(node *tree_sitter.Node, source []byte) (map[string]any, bool) {
	if node == nil || node.Kind() != "assignment" {
		return nil, false
	}

	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		return nil, false
	}

	left := node.ChildByFieldName("left")
	if left == nil {
		return nil, false
	}

	name := strings.TrimSpace(pythonCallFullName(left, source))
	if name == "" {
		return nil, false
	}

	item := map[string]any{
		"name":            name,
		"line_number":     nodeLine(node),
		"type":            pythonNormalizedAnnotation(nodeText(typeNode, source)),
		"annotation_kind": "assignment",
		"lang":            "python",
	}
	if context := pythonAnnotatedAssignmentContext(node, source); context != "" {
		item["context"] = context
	}
	return item, true
}

func pythonAnnotatedAssignmentContext(node *tree_sitter.Node, source []byte) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_definition":
			nameNode := current.ChildByFieldName("name")
			return strings.TrimSpace(nodeText(nameNode, source))
		case "function_definition", "lambda", "module":
			return ""
		}
	}
	return ""
}
