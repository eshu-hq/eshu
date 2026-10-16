// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fulltext_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/infra/fulltext"
)

func TestUnscopedStatementShape(t *testing.T) {
	t.Parallel()
	got := fulltext.UnscopedStatement([]string{"A", "B"}, "\n AND n.kind = $kind", []string{"coalesce(n.id, '') as id", "coalesce(n.name, '') as name"})
	want := "CALL db.index.fulltext.queryNodes('infra_search_key_ft', $infra_search_lucene) YIELD node AS n\n" +
		"WITH n\nWHERE (n:A OR n:B)\n AND n.kind = $kind\n" +
		"RETURN DISTINCT coalesce(n.id, '') as id, coalesce(n.name, '') as name\n" +
		"ORDER BY name\nLIMIT $limit"
	if got != want {
		t.Fatalf("UnscopedStatement() =\n%s\nwant\n%s", got, want)
	}
}

// The scoped legacy statement UNIONs over n, so two nodes with identical
// projected columns stay two rows; the scoped full-text form must not add
// DISTINCT, and must apply the grant predicate after the candidate filter.
func TestScopedStatementKeepsPerNodeRowsAndHoistedGrant(t *testing.T) {
	t.Parallel()
	got := fulltext.ScopedStatement([]string{"A"}, "\n AND x", "(GRANT)", []string{"coalesce(n.name, '') as name"})
	want := "CALL db.index.fulltext.queryNodes('infra_search_key_ft', $infra_search_lucene) YIELD node AS n\n" +
		"WITH n\nWHERE (n:A)\n AND x\n" +
		"WITH n\nWHERE (GRANT)\n" +
		"RETURN coalesce(n.name, '') as name\n" +
		"ORDER BY name\nLIMIT $limit"
	if got != want {
		t.Fatalf("ScopedStatement() =\n%s\nwant\n%s", got, want)
	}
	if strings.Contains(got, "DISTINCT") {
		t.Fatal("scoped statement must not de-duplicate projected rows")
	}
}
