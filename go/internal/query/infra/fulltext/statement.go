// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fulltext

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// candidates is the index call and label filter both statements start with.
// whereExtra is appended unchanged after the label group; it carries the
// legacy free-text or "::" equality predicate and the structured filters.
func candidates(labels []string, whereExtra string) string {
	terms := make([]string, len(labels))
	for i, label := range labels {
		terms[i] = "n:" + label
	}
	return "CALL db.index.fulltext.queryNodes('" + graph.InfraSearchKeyIndexName + "', $" + LuceneParam + ") YIELD node AS n\n" +
		"WITH n\nWHERE (" + strings.Join(terms, " OR ") + ")" + whereExtra + "\n"
}

// UnscopedStatement is the full-text form of the unscoped legacy search. The
// legacy statement UNIONs projected rows, which de-duplicates identical rows,
// so this form returns DISTINCT rows. DISTINCT runs before ORDER BY and LIMIT:
// an inner top-k would cut the page before the de-duplication and return too
// few rows.
func UnscopedStatement(labels []string, whereExtra string, returnExprs []string) string {
	return candidates(labels, whereExtra) +
		"RETURN DISTINCT " + strings.Join(returnExprs, ", ") + "\n" +
		"ORDER BY name\nLIMIT $limit"
}

// ScopedStatement is the full-text form of the Neo4j scoped legacy search. The
// legacy statement UNIONs nodes, not rows, so two nodes with identical
// projected columns stay two rows; this form does not add DISTINCT. The index
// yields each node once. scopePredicate is the hoisted list-EXISTS grant
// predicate, applied after the candidate filter as in the legacy form.
func ScopedStatement(labels []string, whereExtra, scopePredicate string, returnExprs []string) string {
	return candidates(labels, whereExtra) +
		"WITH n\nWHERE " + scopePredicate + "\n" +
		"RETURN " + strings.Join(returnExprs, ", ") + "\n" +
		"ORDER BY name\nLIMIT $limit"
}
