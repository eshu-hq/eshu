// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"sort"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Options configures one SQL parser execution.
type Options = shared.Options

// sqlMention is one bounded table reference recovered from the AST, tagged with
// the DML/DDL operation that produced it and its byte offset for line mapping.
type sqlMention struct {
	name      string
	operation string
	offset    int
}

type sqlLineIndex struct {
	sourceLength int
	newlines     []int
}

func newSQLLineIndex(source []byte) sqlLineIndex {
	newlines := make([]int, 0)
	for index, b := range source {
		if b == '\n' {
			newlines = append(newlines, index)
		}
	}
	return sqlLineIndex{sourceLength: len(source), newlines: newlines}
}

func (idx sqlLineIndex) lineForOffset(offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > idx.sourceLength {
		offset = idx.sourceLength
	}
	return sort.SearchInts(idx.newlines, offset) + 1
}

// normalizeSQLName strips dialect quoting (double quotes, MySQL backticks,
// MSSQL brackets) from each dotted segment and rejoins the schema-qualified
// name. Empty segments are dropped so trailing separators do not survive.
func normalizeSQLName(raw string) string {
	parts := strings.Split(raw, ".")
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		cleaned := strings.TrimSpace(part)
		cleaned = strings.TrimPrefix(cleaned, `"`)
		cleaned = strings.TrimSuffix(cleaned, `"`)
		cleaned = strings.TrimPrefix(cleaned, "`")
		cleaned = strings.TrimSuffix(cleaned, "`")
		cleaned = strings.TrimPrefix(cleaned, "[")
		cleaned = strings.TrimSuffix(cleaned, "]")
		if cleaned != "" {
			normalized = append(normalized, cleaned)
		}
	}
	return strings.Join(normalized, ".")
}

// collectMentionsFromNode walks a query/body subtree and returns the bounded
// table references it contains, tagged by operation. Relations inside FROM/JOIN
// clauses yield "select" reads; INSERT/UPDATE/DELETE/REFERENCES/ALTER/DROP
// yield the matching mutation operation. Offsets are absolute byte positions
// in the original source so callers can map line numbers.
func collectMentionsFromNode(node *tree_sitter.Node, source []byte, includeReads bool) []sqlMention {
	mentions := make([]sqlMention, 0)
	seen := make(map[string]struct{})
	var visit func(n *tree_sitter.Node, operation string)
	addName := func(name, operation string, offset int) {
		if name == "" {
			return
		}
		key := operation + "|" + name + "|" + strconv.Itoa(offset)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		mentions = append(mentions, sqlMention{name: name, operation: operation, offset: offset})
	}
	add := func(ref *tree_sitter.Node, operation string) {
		if ref == nil {
			return
		}
		addName(objectReferenceName(ref, source), operation, int(ref.StartByte()))
	}
	visit = func(n *tree_sitter.Node, operation string) {
		switch n.GrammarName() {
		case "relation":
			if includeReads {
				add(firstChildByKind(n, "object_reference"), "select")
			}
		case "insert":
			add(firstDirectChildByKind(n, "object_reference"), "insert")
		case "update":
			if rel := firstChildByKind(n, "relation"); rel != nil {
				add(firstChildByKind(rel, "object_reference"), "update")
			}
		case "delete":
			// DELETE FROM target lives in the sibling `from` clause; handled below.
		case "from":
			add(firstChildByKind(n, "object_reference"), "select")
		case "alter_table":
			// The altered table is the first object_reference child. A migration
			// that only does ALTER TABLE must still record the table it touches.
			add(firstDirectChildByKind(n, "object_reference"), "alter")
		case "drop_table":
			// DROP TABLE is migration evidence for an existing table, not a new
			// SqlTable entity. The grammar recovers valid comma-separated target
			// lists in more than one shape; collectDropTableTargets keeps recovery
			// structurally bounded so unrelated trailing SQL cannot become DROP
			// evidence.
			collectDropTableTargets(n, source, add, addName)
		}
		for _, child := range namedChildren(n) {
			visit(child, operation)
		}
	}
	collectDeleteTargets(node, source, add)
	collectReferencesTargets(node, add)
	visit(node, "")
	return dropShadowedReads(mentions)
}

// collectDropTableTargets records every target in a DROP TABLE statement.
// DerekStride/tree-sitter-sql accepts one target in the grammar production;
// valid comma-separated targets before the final target therefore usually
// appear as direct object_reference children of a direct ERROR node. Some
// otherwise-valid forms instead put the remaining comma list after the
// drop_table node as a sibling ERROR. The bounded source-tail recovery handles
// only a complete comma-separated identifier list and never walks arbitrary
// ERROR descendants, so malformed trailing SQL cannot become DROP evidence.
func collectDropTableTargets(
	node *tree_sitter.Node,
	source []byte,
	add func(ref *tree_sitter.Node, operation string),
	addName func(name, operation string, offset int),
) {
	for _, child := range namedChildren(node) {
		switch child.GrammarName() {
		case "object_reference":
			add(child, "drop")
		case "ERROR":
			// A direct ERROR child is also how the grammar represents some
			// valid comma-separated lists. Trust its object_reference children
			// only when the complete source remainder is a bounded target list;
			// otherwise malformed recovery can mint false MIGRATES evidence.
			if !isCompleteDropTargetListFrom(int(child.StartByte()), source) {
				continue
			}
			for _, recovered := range namedChildren(child) {
				if recovered.GrammarName() == "object_reference" {
					add(recovered, "drop")
				}
			}
		}
	}
	recoverDropTableTargetsFromTail(int(node.EndByte()), source, addName)
}

// dropShadowedReads removes "select" mentions that are actually mutation
// targets. The generic relation/from read walk in collectMentionsFromNode tags
// the object_reference of an UPDATE or DELETE target as a "select" read, at the
// SAME byte offset the update/delete clause already recorded it as a write. That
// spurious read tag must not survive: an UPDATE/DELETE target is a write, not a
// read, and a stamped "select" mention now materializes a READS_FROM edge
// (#5345) — a write mislabeled as a read is wrong graph truth. A table that is
// genuinely both read and written in the same routine (e.g. INSERT INTO t SELECT
// FROM t) keeps a distinct read offset, so its real read survives.
func dropShadowedReads(mentions []sqlMention) []sqlMention {
	mutatedAt := make(map[string]struct{})
	for _, m := range mentions {
		if m.operation != "select" {
			mutatedAt[m.name+"|"+strconv.Itoa(m.offset)] = struct{}{}
		}
	}
	if len(mutatedAt) == 0 {
		return mentions
	}
	filtered := make([]sqlMention, 0, len(mentions))
	for _, m := range mentions {
		if m.operation == "select" {
			if _, shadowed := mutatedAt[m.name+"|"+strconv.Itoa(m.offset)]; shadowed {
				continue
			}
		}
		filtered = append(filtered, m)
	}
	return filtered
}

// collectReferencesTargets records the target table of every REFERENCES (foreign
// key) clause in the subtree. The grammar places the referenced table in an
// object_reference that follows a keyword_references token, including inside
// ALTER TABLE ... ADD CONSTRAINT ... FOREIGN KEY ... REFERENCES clauses, so a
// migration whose only table touch is a foreign key still records a mention.
func collectReferencesTargets(node *tree_sitter.Node, add func(ref *tree_sitter.Node, operation string)) {
	var visit func(n *tree_sitter.Node)
	visit = func(n *tree_sitter.Node) {
		sawReferences := false
		for _, child := range allChildren(n) {
			switch child.GrammarName() {
			case "keyword_references":
				sawReferences = true
				continue
			case "object_reference":
				if sawReferences {
					add(child, "references")
					sawReferences = false
					continue
				}
			}
			visit(child)
		}
	}
	visit(node)
}

// collectDeleteTargets attaches DELETE statements to their FROM target so a
// delete is recorded as a "delete" mention rather than a generic read.
func collectDeleteTargets(node *tree_sitter.Node, source []byte, add func(ref *tree_sitter.Node, operation string)) {
	var visit func(n *tree_sitter.Node)
	visit = func(n *tree_sitter.Node) {
		if n.GrammarName() == "delete" {
			parent := n.Parent()
			if parent != nil {
				if fromClause := firstChildByKind(parent, "from"); fromClause != nil {
					add(firstChildByKind(fromClause, "object_reference"), "delete")
				}
			}
		}
		for _, child := range namedChildren(n) {
			visit(child)
		}
	}
	visit(node)
}

// firstDirectChildByKind returns the first direct named child of node whose
// grammar name matches kind, without recursing into descendants.
func firstDirectChildByKind(node *tree_sitter.Node, kind string) *tree_sitter.Node {
	for _, child := range namedChildren(node) {
		if child.GrammarName() == kind {
			return child
		}
	}
	return nil
}
