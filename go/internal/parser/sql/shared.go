package sql

import (
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

// sqlLineNumberForOffset returns the 1-based line number for a byte offset.
func sqlLineNumberForOffset(source []byte, offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	return strings.Count(string(source[:offset]), "\n") + 1
}

// collectMentionsFromNode walks a query/body subtree and returns the bounded
// table references it contains, tagged by operation. Relations inside FROM/JOIN
// clauses yield "select" reads; INSERT/UPDATE/DELETE/REFERENCES/ALTER yield the
// matching mutation operation. Offsets are absolute byte positions in the
// original source so callers can map line numbers.
func collectMentionsFromNode(node *tree_sitter.Node, source []byte, includeReads bool) []sqlMention {
	mentions := make([]sqlMention, 0)
	seen := make(map[string]struct{})
	var visit func(n *tree_sitter.Node, operation string)
	add := func(ref *tree_sitter.Node, operation string) {
		if ref == nil {
			return
		}
		name := objectReferenceName(ref, source)
		if name == "" {
			return
		}
		offset := int(ref.StartByte())
		key := operation + "|" + name + "|" + strconv.Itoa(offset)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		mentions = append(mentions, sqlMention{name: name, operation: operation, offset: offset})
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
		}
		for _, child := range namedChildren(n) {
			visit(child, operation)
		}
	}
	collectDeleteTargets(node, source, add)
	visit(node, "")
	return mentions
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
