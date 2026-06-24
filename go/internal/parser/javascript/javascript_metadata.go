// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptDocstring(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	lines := strings.Split(string(source), "\n")
	startRow := int(node.StartPosition().Row)
	if startRow <= 0 || startRow > len(lines) {
		return ""
	}

	commentLines := make([]string, 0)
	for index := startRow - 1; index >= 0; index-- {
		trimmed := strings.TrimSpace(lines[index])
		switch {
		case trimmed == "":
			if len(commentLines) == 0 {
				continue
			}
			return ""
		case strings.HasPrefix(trimmed, "/**") && strings.HasSuffix(trimmed, "*/"):
			return normalizeJavaScriptDocstring([]string{trimmed})
		case strings.HasPrefix(trimmed, "*/"):
			commentLines = append([]string{trimmed}, commentLines...)
		case len(commentLines) > 0:
			commentLines = append([]string{trimmed}, commentLines...)
			if strings.HasPrefix(trimmed, "/**") {
				return normalizeJavaScriptDocstring(commentLines)
			}
			if strings.HasPrefix(trimmed, "/*") {
				return ""
			}
		default:
			return ""
		}
	}

	return ""
}

func normalizeJavaScriptDocstring(lines []string) string {
	if len(lines) == 0 {
		return ""
	}

	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "/**")
		trimmed = strings.TrimPrefix(trimmed, "/*")
		trimmed = strings.TrimPrefix(trimmed, "*/")
		trimmed = strings.TrimSuffix(trimmed, "*/")
		trimmed = strings.TrimPrefix(trimmed, "*")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, "\n")
}

func javaScriptFunctionKind(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	switch node.Kind() {
	case "function_declaration", "function_expression", "arrow_function":
		declaration := strings.TrimSpace(nodeText(node, source))
		if strings.HasPrefix(declaration, "function*") || strings.HasPrefix(declaration, "async function*") {
			return "generator"
		}
		if strings.HasPrefix(declaration, "async ") {
			return "async"
		}
		return ""
	case "generator_function_declaration", "generator_function":
		return "generator"
	case "method_definition", "method_signature":
		return javaScriptMethodKind(node)
	}

	return ""
}

// javaScriptMethodKind classifies a method_definition/method_signature node from
// its leading modifier tokens. The tree-sitter grammar emits get/set/async and
// the generator star as anonymous child tokens before the method name field, so
// the kind is read directly from the AST instead of re-scanning node text. The
// precedence mirrors the previous regex contract: a leading get/set/async (after
// an optional static) classifies the method as getter/setter/async, and a bare
// generator star otherwise yields "generator".
func javaScriptMethodKind(node *tree_sitter.Node) string {
	if node == nil {
		return ""
	}
	nameNode := node.ChildByFieldName("name")
	generator := false
	for index := uint(0); index < node.ChildCount(); index++ {
		child := node.Child(index)
		if child == nil {
			continue
		}
		if nameNode != nil && child.Id() == nameNode.Id() {
			break
		}
		switch child.Kind() {
		case "static":
			continue
		case "get":
			return "getter"
		case "set":
			return "setter"
		case "async":
			return "async"
		case "*":
			generator = true
		}
	}
	if generator {
		return "generator"
	}
	return ""
}
