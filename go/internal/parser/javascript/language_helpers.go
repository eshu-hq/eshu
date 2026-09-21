// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"log/slog"
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

func javaScriptInsideFunction(node *tree_sitter.Node, parents *javaScriptParentLookup) bool {
	for current := parents.parent(node); current != nil; current = parents.parent(current) {
		switch current.Kind() {
		case "function_declaration", "function_expression", "arrow_function", "method_definition":
			return true
		}
	}
	return false
}

func javaScriptDecorators(node *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) []string {
	decorators := make([]string, 0)
	for current := node; current != nil; current = parents.parent(current) {
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
		if parents.parent(current) == nil || parents.parent(current).Kind() != "decorated_definition" {
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

// jsParseByteCap bounds the size of a single JavaScript/TypeScript/TSX file
// handed to tree-sitter. Normal hand-written source is tens of KB; the
// pathological tail is large generated bundles (minified webpack output,
// bundled vendor code) that tree-sitter parses superlinearly -- a 2.7MB
// webpack bundle measured 15.9s (~224x a normal parse) and a 3.4MB generated
// bundle measured 5.1s (#4766). 1 MiB is generous headroom above any
// hand-written file while remaining well below the pathological range.
const jsParseByteCap = 1 << 20

// ParserFactory returns a pooled tree-sitter parser for the requested runtime
// grammar name. The caller must return the parser via the paired ParserReturner
// after use instead of calling parser.Close directly.
type ParserFactory func(language string) (*tree_sitter.Parser, error)

// ParserReturner returns a borrowed parser to the runtime pool. It must be
// called with the same language name that was passed to ParserFactory.
type ParserReturner func(language string, p *tree_sitter.Parser)

// jsBoundedFileEvent records one file whose size exceeded jsParseByteCap and
// whose tree-sitter parse was skipped entirely.
type jsBoundedFileEvent struct {
	path          string
	originalBytes int
}

// row renders one bounded-file event as a payload row for
// payload["js_parse_bounded"].
func (e jsBoundedFileEvent) row() map[string]any {
	return map[string]any{
		"path":           e.path,
		"original_bytes": e.originalBytes,
		"action":         "file_skipped",
	}
}

// recordJSBoundedFile appends a js_parse_bounded payload row for one bounded
// file and emits a matching structured log line so a dropped parse is
// observable rather than silent.
func recordJSBoundedFile(payload map[string]any, path string, originalBytes int) {
	event := jsBoundedFileEvent{path: path, originalBytes: originalBytes}
	payload["js_parse_bounded"] = append(
		payload["js_parse_bounded"].([]map[string]any),
		event.row(),
	)
	slog.Warn(
		"javascript-family parse file bounded",
		"component", "parser.javascript",
		"path", event.path,
		"original_bytes", event.originalBytes,
		"action", "file_skipped",
	)
}
