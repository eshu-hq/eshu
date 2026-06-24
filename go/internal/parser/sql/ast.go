// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"strings"
	"unicode"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// sqlStatementKinds maps the tree-sitter grammar node kinds that introduce a
// top-level SQL statement the extractor understands. The DerekStride SQL
// grammar wraps each statement in a `statement` node whose first named child is
// one of these construct nodes.
var sqlStatementKinds = map[string]struct{}{
	"create_table":             {},
	"create_view":              {},
	"create_materialized_view": {},
	"create_function":          {},
	"create_index":             {},
	"create_trigger":           {},
	"alter_table":              {},
}

// namedChildren returns the named children of a node in source order. Anonymous
// tokens (keywords, punctuation) are skipped so callers reason about structural
// nodes only.
func namedChildren(node *tree_sitter.Node) []*tree_sitter.Node {
	count := node.NamedChildCount()
	children := make([]*tree_sitter.Node, 0, count)
	for index := uint(0); index < count; index++ {
		children = append(children, node.NamedChild(index))
	}
	return children
}

// firstChildByKind returns the first named descendant of node whose grammar
// name matches kind, preferring direct children before recursing. Returns nil
// when no match exists.
func firstChildByKind(node *tree_sitter.Node, kind string) *tree_sitter.Node {
	for _, child := range namedChildren(node) {
		if child.GrammarName() == kind {
			return child
		}
	}
	for _, child := range namedChildren(node) {
		if found := firstChildByKind(child, kind); found != nil {
			return found
		}
	}
	return nil
}

// nodeText returns the source slice covered by node.
func nodeText(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return string(source[node.StartByte():node.EndByte()])
}

// objectReferenceName normalizes an `object_reference` node into a dotted,
// dialect-quote-stripped name. When node is not an object_reference the first
// descendant object_reference is used.
func objectReferenceName(node *tree_sitter.Node, source []byte) string {
	ref := node
	if node != nil && node.GrammarName() != "object_reference" {
		ref = firstChildByKind(node, "object_reference")
	}
	if ref == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	for _, child := range namedChildren(ref) {
		if child.GrammarName() == "identifier" {
			parts = append(parts, normalizeSQLName(nodeText(child, source)))
		}
	}
	if len(parts) == 0 {
		return normalizeSQLName(nodeText(ref, source))
	}
	return strings.Join(parts, ".")
}

// childObjectReferences returns the direct object_reference children of node in
// source order. Trigger extraction relies on the header reference ordering
// (trigger, table, function).
func childObjectReferences(node *tree_sitter.Node) []*tree_sitter.Node {
	refs := make([]*tree_sitter.Node, 0, 3)
	for _, child := range namedChildren(node) {
		if child.GrammarName() == "object_reference" {
			refs = append(refs, child)
		}
	}
	return refs
}

// sqlDollarQuoteTagAt returns the dollar-quote tag that begins at the start of
// source ("$$" or "$tag$"), or "" when source does not open a dollar-quoted
// string. Used by the statement segmenter to skip routine bodies.
func sqlDollarQuoteTagAt(source string) string {
	if !strings.HasPrefix(source, "$") {
		return ""
	}
	closing := strings.IndexByte(source[1:], '$')
	if closing < 0 {
		return ""
	}
	tag := source[:closing+2]
	if tag == "$$" {
		return tag
	}
	inner := tag[1 : len(tag)-1]
	for index, r := range inner {
		switch {
		case index == 0 && (unicode.IsLetter(r) || r == '_'):
		case index > 0 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$'):
		default:
			return ""
		}
	}
	return tag
}
