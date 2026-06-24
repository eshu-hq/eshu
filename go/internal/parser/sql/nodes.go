// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// sqlColumnDataType returns the declared type text for a column: the first type
// node after the column identifier, or the run of type keyword tokens before
// the first constraint/reference clause. Multi-token spellings ("CHARACTER
// VARYING(20)") are space-joined; lengths and precisions are preserved.
func sqlColumnDataType(definition *tree_sitter.Node, columnNode *tree_sitter.Node, source []byte) string {
	typeParts := make([]string, 0, 2)
	started := false
	for _, child := range allChildren(definition) {
		if child.Id() == columnNode.Id() {
			started = true
			continue
		}
		if !started {
			continue
		}
		name := child.GrammarName()
		if isColumnTypeBoundary(name) {
			break
		}
		if !child.IsNamed() && !strings.HasPrefix(name, "keyword_") {
			// Skip stray punctuation between identifier and type.
			continue
		}
		typeParts = append(typeParts, strings.TrimSpace(nodeText(child, source)))
		if child.IsNamed() {
			// A named type node carries its full declaration including length.
			break
		}
	}
	return strings.Join(nonEmpty(typeParts), " ")
}

// isColumnTypeBoundary reports whether a node kind ends the data-type span of a
// column definition (a constraint keyword or reference clause).
func isColumnTypeBoundary(kind string) bool {
	switch kind {
	case "keyword_primary", "keyword_not", "keyword_null", "keyword_references",
		"keyword_default", "keyword_unique", "keyword_check", "keyword_constraint",
		"keyword_generated", "keyword_collate", "object_reference":
		return true
	}
	return false
}

// inlineColumnReference returns the object_reference of an inline column-level
// REFERENCES (foreign key) clause, or nil when the column has none.
func inlineColumnReference(definition *tree_sitter.Node) *tree_sitter.Node {
	sawReferences := false
	for _, child := range allChildren(definition) {
		switch child.GrammarName() {
		case "keyword_references":
			sawReferences = true
		case "object_reference":
			if sawReferences {
				return child
			}
		}
	}
	return nil
}

// collectConstraintReferences returns the REFERENCES targets declared in a
// table-level `constraints` node (FOREIGN KEY ... REFERENCES ...). It walks all
// tokens, including anonymous keyword tokens, so the REFERENCES keyword that
// precedes each target object_reference is observed.
func collectConstraintReferences(constraints *tree_sitter.Node, source []byte) []sqlMention {
	mentions := make([]sqlMention, 0)
	var walk func(n *tree_sitter.Node)
	walk = func(n *tree_sitter.Node) {
		sawReferences := false
		for _, child := range allChildren(n) {
			switch child.GrammarName() {
			case "keyword_references":
				sawReferences = true
			case "object_reference":
				if sawReferences {
					mentions = append(mentions, sqlMention{
						name:   objectReferenceName(child, source),
						offset: int(child.StartByte()),
					})
					sawReferences = false
					continue
				}
				walk(child)
			default:
				walk(child)
			}
		}
	}
	walk(constraints)
	return mentions
}

// sqlRoutineLanguage returns the LANGUAGE clause identifier of a routine, or ""
// when absent.
func sqlRoutineLanguage(node *tree_sitter.Node, source []byte) string {
	language := firstChildByKind(node, "function_language")
	if language == nil {
		return ""
	}
	identifier := firstChildByKind(language, "identifier")
	if identifier == nil {
		return ""
	}
	return strings.TrimSpace(nodeText(identifier, source))
}

// sqlIndexName returns the index identifier of a create_index node.
func sqlIndexName(node *tree_sitter.Node, source []byte) string {
	for _, child := range namedChildren(node) {
		if child.GrammarName() == "identifier" {
			return normalizeSQLName(nodeText(child, source))
		}
	}
	return ""
}

// sqlIndexTable returns the indexed table reference of a create_index node.
func sqlIndexTable(node *tree_sitter.Node, source []byte) string {
	if ref := firstChildByKind(node, "object_reference"); ref != nil {
		return objectReferenceName(ref, source)
	}
	return ""
}

// sqlSchema returns the schema prefix of a dotted name, or "" when unqualified.
func sqlSchema(name string) string {
	if strings.Contains(name, ".") {
		return name[:strings.LastIndex(name, ".")]
	}
	return ""
}

// allChildren returns every child of node, including anonymous keyword and
// punctuation tokens, in source order.
func allChildren(node *tree_sitter.Node) []*tree_sitter.Node {
	count := node.ChildCount()
	children := make([]*tree_sitter.Node, 0, count)
	for index := uint(0); index < count; index++ {
		children = append(children, node.Child(index))
	}
	return children
}

func nonEmpty(values []string) []string {
	out := values[:0]
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
