// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var builtinTypeNames = map[string]struct{}{
	"Array":     {},
	"Boolean":   {},
	"Date":      {},
	"Error":     {},
	"Map":       {},
	"Number":    {},
	"Object":    {},
	"Promise":   {},
	"Readonly":  {},
	"Record":    {},
	"Set":       {},
	"String":    {},
	"boolean":   {},
	"never":     {},
	"null":      {},
	"number":    {},
	"object":    {},
	"string":    {},
	"symbol":    {},
	"undefined": {},
	"unknown":   {},
	"void":      {},
}

// AppendTypeReferenceCalls walks root for TypeScript type-annotation
// positions (type annotations, generic type arguments, extends/implements
// clauses, `as`/satisfies expressions) and appends each non-builtin type
// reference it finds to payload's "function_calls" bucket under the
// "typescript.type_reference" call kind. It is a no-op for lang values other
// than "typescript" and "tsx", since plain JavaScript has no type positions.
func AppendTypeReferenceCalls(payload map[string]any, root *tree_sitter.Node, source []byte, lang string) {
	switch lang {
	case "typescript", "tsx":
	default:
		return
	}
	seen := make(map[string]struct{})
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "type_annotation", "type_arguments", "extends_type_clause", "implements_clause", "as_expression", "type_assertion", "satisfies_expression":
			appendTypeReferencesFromNode(payload, node, source, lang, seen)
		}
	})
}

func appendTypeReferencesFromNode(
	payload map[string]any,
	node *tree_sitter.Node,
	source []byte,
	lang string,
	seen map[string]struct{},
) {
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		switch child.Kind() {
		case "type_identifier", "nested_type_identifier", "scoped_type_identifier":
		default:
			return
		}
		fullName := strings.TrimSpace(shared.NodeText(child, source))
		name := TypeReferenceLeafName(fullName)
		if name == "" || isBuiltinTypeName(name) {
			return
		}
		key := fullName + "|" + strconv.Itoa(shared.NodeLine(child))
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		shared.AppendBucket(payload, "function_calls", map[string]any{
			"name":        name,
			"full_name":   fullName,
			"call_kind":   "typescript.type_reference",
			"line_number": shared.NodeLine(child),
			"lang":        lang,
		})
	})
}

// TypeReferenceLeafName returns the final `.`/`:`-separated segment of a
// (possibly qualified or scoped) type reference name, or "" for a blank
// input.
func TypeReferenceLeafName(fullName string) string {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return ""
	}
	fields := strings.FieldsFunc(fullName, func(r rune) bool {
		return r == '.' || r == ':'
	})
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(fields[len(fields)-1])
}

func isBuiltinTypeName(name string) bool {
	_, ok := builtinTypeNames[strings.TrimSpace(name)]
	return ok
}
