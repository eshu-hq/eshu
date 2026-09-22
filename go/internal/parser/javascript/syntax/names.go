// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var staticComputedMemberNameRe = regexp.MustCompile(`^(?:[A-Za-z_$][A-Za-z0-9_$]*)(?:\.[A-Za-z_$][A-Za-z0-9_$]*)*$|^(?:0|[1-9][0-9]*)$`)

// FunctionName returns the declared name text for a node that names a
// function, method, class member, or JSX identifier, resolving a statically
// known computed property (a string, number, template literal without
// interpolation, or a `+`-concatenation of those) to its literal value.
func FunctionName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	switch node.Kind() {
	case "identifier", "property_identifier", "private_property_identifier", "jsx_identifier", "type_identifier":
		return strings.TrimSpace(shared.NodeText(node, source))
	case "string", "number", "template_string":
		if resolved, ok := staticComputedPropertyName(node, source); ok {
			return resolved
		}
		return strings.TrimSpace(shared.NodeText(node, source))
	case "computed_property_name":
		return computedPropertyName(node, source)
	default:
		return strings.TrimSpace(shared.NodeText(node, source))
	}
}

func computedPropertyName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	cursor := node.Walk()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if resolved, ok := staticComputedPropertyName(&child, source); ok {
			cursor.Close()
			return resolved
		}
	}
	cursor.Close()

	text := strings.TrimSpace(shared.NodeText(node, source))
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
	if unquoted, ok := TrimQuotes(inner); ok {
		inner = unquoted
	}
	if staticComputedMemberNameRe.MatchString(inner) {
		return inner
	}
	return ""
}

func staticComputedPropertyName(node *tree_sitter.Node, source []byte) (string, bool) {
	if node == nil {
		return "", false
	}

	switch node.Kind() {
	case "string":
		if resolved, ok := TrimQuotes(strings.TrimSpace(shared.NodeText(node, source))); ok {
			return resolved, true
		}
	case "number":
		return strings.TrimSpace(shared.NodeText(node, source)), true
	case "template_string":
		text := strings.TrimSpace(shared.NodeText(node, source))
		if text == "" || strings.Contains(text, "${") {
			return "", false
		}
		if resolved, ok := TrimQuotes(text); ok {
			return resolved, true
		}
	case "parenthesized_expression":
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			child := child
			if resolved, ok := staticComputedPropertyName(&child, source); ok {
				return resolved, true
			}
		}
	case "binary_expression":
		text := strings.TrimSpace(shared.NodeText(node, source))
		if !strings.Contains(text, "+") {
			return "", false
		}
		cursor := node.Walk()
		defer cursor.Close()
		children := node.NamedChildren(cursor)
		if len(children) != 2 {
			return "", false
		}
		left, ok := staticComputedPropertyName(&children[0], source)
		if !ok {
			return "", false
		}
		right, ok := staticComputedPropertyName(&children[1], source)
		if !ok {
			return "", false
		}
		return left + right, true
	}

	return "", false
}

// TrimQuotes strips a matching pair of double, single, or backtick quotes
// from text. ok is false when text is too short or its first and last bytes
// are not a matching quote pair, and text is returned unchanged.
func TrimQuotes(text string) (string, bool) {
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

// StringLiteralValue returns the unquoted content of a string node by reading
// its string_fragment child, falling back to trimming the quote bytes.
func StringLiteralValue(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "string_fragment" {
			return shared.NodeText(&child, source)
		}
	}
	text := strings.TrimSpace(shared.NodeText(node, source))
	if unquoted, ok := TrimQuotes(text); ok {
		return unquoted
	}
	return text
}
