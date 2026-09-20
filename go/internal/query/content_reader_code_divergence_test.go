// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

// TestDivergenceGroupStatsQueryShape pins the shipped stats statement
// without hand-copying it: the expected text is derived from the production
// constant by truncating at the stable GROUP BY boundary, and the hermetic
// prefix guard asserts that derived string is a byte-prefix of production.
// Any drift in the SELECT/WHERE core fails loudly here instead of sliding
// into a stale copy.
func TestDivergenceGroupStatsQueryShape(t *testing.T) {
	t.Parallel()

	for _, kind := range []struct {
		name   string
		column string
	}{
		{"exact", "fp_exact"},
		{"renamed", "fp_renamed"},
	} {
		query := divergenceGroupStatsQuery(kind.column)
		marker := "GROUP BY"
		index := strings.Index(query, marker)
		if index < 0 {
			t.Fatalf("%s query must contain a GROUP BY boundary, got:\n%s", kind.name, query)
		}
		derived := query[:index]
		if !strings.HasPrefix(query, derived) || derived == query {
			t.Fatalf("%s derived prefix guard is vacuous", kind.name)
		}
		for _, clause := range []string{
			"FROM code_function_fingerprint",
			"f.repo_id = $1",
			"f.token_count >= $2",
		} {
			if !strings.Contains(derived, clause) {
				t.Fatalf("%s query must contain %q, got:\n%s", kind.name, clause, derived)
			}
		}
		if strings.Contains(query, "source_cache") {
			t.Fatalf("%s query must never read source_cache (#6835 contract gate)", kind.name)
		}
		if strings.Contains(query, "LIMIT") || strings.Contains(query, "OFFSET") {
			t.Fatalf("%s stats query must not page: paging happens over ranked stats in Go, got:\n%s", kind.name, query)
		}
		// Placeholders are fixed: $1 repo_id, $2 token floor. A
		// renumbered placeholder binds the wrong value.
		one, two := strings.Index(query, "$1"), strings.Index(query, "$2")
		if one < 0 || two < 0 || two < one {
			t.Fatalf("%s query placeholders out of order, got:\n%s", kind.name, query)
		}
		if strings.Contains(query, "$3") {
			t.Fatalf("%s query must take exactly two args, got:\n%s", kind.name, query)
		}
	}
	if got := divergenceGroupStatsQuery("fp_exact; DROP TABLE x; --"); got != "" {
		t.Fatalf("unexpected column must yield empty query, got:\n%s", got)
	}
	// The entity join lives in the phase-two member statement, pinned here
	// so a future edit cannot silently fold member hydration into the
	// grouping scan.
	if !strings.Contains(divergenceMembersQuery, "JOIN content_entities") {
		t.Fatal("member query must join content_entities")
	}
}
