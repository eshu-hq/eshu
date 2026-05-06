package parser

import (
	"regexp"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var javaScriptStaticComputedMemberNameRe = regexp.MustCompile(`^(?:[A-Za-z_$][A-Za-z0-9_$]*)(?:\.[A-Za-z_$][A-Za-z0-9_$]*)*$|^(?:0|[1-9][0-9]*)$`)

func javaScriptFunctionName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	switch node.Kind() {
	case "identifier", "property_identifier", "private_property_identifier", "jsx_identifier", "type_identifier":
		return strings.TrimSpace(nodeText(node, source))
	case "computed_property_name":
		return javaScriptComputedPropertyName(node, source)
	default:
		return strings.TrimSpace(nodeText(node, source))
	}
}

func javaScriptComputedPropertyName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if resolved, ok := javaScriptStaticComputedPropertyName(&child, source); ok {
			cursor.Close()
			return resolved
		}
	}
	cursor.Close()

	text := strings.TrimSpace(nodeText(node, source))
	if text == "" {
		return ""
	}
	if len(text) < 2 || text[0] != '[' || text[len(text)-1] != ']' {
		return text
	}

	inner := strings.TrimSpace(text[1 : len(text)-1])
	if inner == "" {
		return text
	}
	if unquoted, ok := trimJavaScriptQuotes(inner); ok {
		inner = unquoted
	}
	if javaScriptStaticComputedMemberNameRe.MatchString(inner) {
		return inner
	}
	return ""
}

func javaScriptStaticComputedPropertyName(node *tree_sitter.Node, source []byte) (string, bool) {
	if node == nil {
		return "", false
	}

	switch node.Kind() {
	case "string":
		if resolved, ok := trimJavaScriptQuotes(strings.TrimSpace(nodeText(node, source))); ok {
			return resolved, true
		}
	case "number":
		return strings.TrimSpace(nodeText(node, source)), true
	case "template_string":
		text := strings.TrimSpace(nodeText(node, source))
		if text == "" || strings.Contains(text, "${") {
			return "", false
		}
		if resolved, ok := trimJavaScriptQuotes(text); ok {
			return resolved, true
		}
	case "parenthesized_expression":
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			child := child
			if resolved, ok := javaScriptStaticComputedPropertyName(&child, source); ok {
				return resolved, true
			}
		}
	case "binary_expression":
		text := strings.TrimSpace(nodeText(node, source))
		if !strings.Contains(text, "+") {
			return "", false
		}
		cursor := node.Walk()
		defer cursor.Close()
		children := node.NamedChildren(cursor)
		if len(children) != 2 {
			return "", false
		}
		left, ok := javaScriptStaticComputedPropertyName(&children[0], source)
		if !ok {
			return "", false
		}
		right, ok := javaScriptStaticComputedPropertyName(&children[1], source)
		if !ok {
			return "", false
		}
		return left + right, true
	}

	return "", false
}

func trimJavaScriptQuotes(text string) (string, bool) {
	if len(text) < 2 {
		return text, false
	}

	first := text[0]
	last := text[len(text)-1]
	switch {
	case first == '"' && last == '"':
		return text[1 : len(text)-1], true
	case first == '\'' && last == '\'':
		return text[1 : len(text)-1], true
	case first == '`' && last == '`':
		return text[1 : len(text)-1], true
	default:
		return text, false
	}
}
