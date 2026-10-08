// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestTargetedMaintenanceQueriesDeriveFromShippedQueries pins that every query
// the partition-scoped maintenance pass (#7584) runs is the whole pass's
// shipped query plus exactly one predicate, never a hand-copied second query.
// The shipped text before the marker must be a byte prefix of the derived
// query, the shipped text after the marker must be its byte suffix, and the
// only bytes between them must be the inserted conjunct.
func TestTargetedMaintenanceQueriesDeriveFromShippedQueries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		shipped  string
		derived  string
		marker   string
		conjunct string
	}{
		{
			name:     "active repository generations bounded by repo",
			shipped:  activeRepositoryGenerationsQuery,
			derived:  activeRepositoryGenerationsForReposQuery,
			marker:   activeRepositoryGenerationsRepoMarker,
			conjunct: "  AND repo_id = ANY($1)\n",
		},
		{
			name:     "deployment_mapping reopen bounded by partition",
			shipped:  listSucceededDeploymentMappingWorkItemsQuery,
			derived:  listSucceededDeploymentMappingWorkItemsForPartitionsQuery,
			marker:   correlationReopenStageMarker,
			conjunct: relationshipReopenPartitionConjunct,
		},
		{
			name:     "code_import_repo_edge reopen bounded by partition",
			shipped:  listSucceededCodeImportRepoEdgeWorkItemsQuery,
			derived:  listSucceededCodeImportRepoEdgeWorkItemsForPartitionsQuery,
			marker:   correlationReopenStageMarker,
			conjunct: relationshipReopenPartitionConjunct,
		},
		{
			name:     "correlation reopen bounded by partition",
			shipped:  listSucceededReducerWorkItemsByDomainQuery,
			derived:  listSucceededReducerWorkItemsByDomainForPartitionsQuery,
			marker:   correlationReopenStageMarker,
			conjunct: "  AND (work.scope_id, work.generation_id) IN (SELECT * FROM unnest($2::text[], $3::text[]))\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.Count(tc.shipped, tc.marker); got != 1 {
				t.Fatalf("shipped query holds marker %q %d times, want 1", tc.marker, got)
			}
			cut := strings.Index(tc.shipped, tc.marker) + len(tc.marker)
			prefix, suffix := tc.shipped[:cut], tc.shipped[cut:]
			if !strings.HasPrefix(tc.derived, prefix) {
				t.Fatalf("derived query does not start with the shipped prefix")
			}
			if !strings.HasSuffix(tc.derived, suffix) {
				t.Fatalf("derived query does not end with the shipped suffix")
			}
			if middle := tc.derived[len(prefix) : len(tc.derived)-len(suffix)]; middle != tc.conjunct {
				t.Fatalf("derived query inserts %q, want exactly %q", middle, tc.conjunct)
			}
		})
	}

	// The partition read wraps the whole shipped query so the DISTINCT ON
	// repository pick is the shipped pick; it must embed it verbatim.
	if !strings.Contains(activeRepositoryGenerationsForPartitionsQuery, activeRepositoryGenerationsQuery) {
		t.Fatal("partition-bounded repository read does not embed activeRepositoryGenerationsQuery verbatim")
	}
}

// TestDeriveQueryAtMarkerRefusesMissingOrRepeatedMarker pins the fail-closed
// derivation: a shipped query that lost its marker, or holds it twice, must
// panic at package init rather than derive some other query shape.
func TestDeriveQueryAtMarkerRefusesMissingOrRepeatedMarker(t *testing.T) {
	t.Parallel()

	for _, shipped := range []string{"SELECT 1\n", "WHERE a\nWHERE a\n"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("deriveQueryAtMarker(%q) did not panic", shipped)
				}
			}()
			_ = deriveQueryAtMarker(shipped, "WHERE a\n", "  AND b\n")
		}()
	}
	if got, want := deriveQueryAtMarker("x\nWHERE a\ny\n", "WHERE a\n", "  AND b\n"), "x\nWHERE a\n  AND b\ny\n"; got != want {
		t.Fatalf("deriveQueryAtMarker() = %q, want %q", got, want)
	}
}

// TestTargetedMaintenanceMarkersOpenAnAndOnlyWhereClause pins predicate
// semantics, not bytes: each derived query appends "AND <bound>"
// right after its marker, which narrows the shipped rows only while the
// shipped WHERE clause the marker opens is a pure conjunction. A top-level OR
// anywhere in that clause would make the inserted AND bind to one disjunct and
// silently widen the derived query. Parenthesized ORs are fine.
func TestTargetedMaintenanceMarkersOpenAnAndOnlyWhereClause(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ shipped, marker string }{
		"active repository generations": {activeRepositoryGenerationsQuery, activeRepositoryGenerationsRepoMarker},
		"deployment_mapping reopen":     {listSucceededDeploymentMappingWorkItemsQuery, correlationReopenStageMarker},
		"code_import_repo_edge reopen":  {listSucceededCodeImportRepoEdgeWorkItemsQuery, correlationReopenStageMarker},
		"correlation reopen":            {listSucceededReducerWorkItemsByDomainQuery, correlationReopenStageMarker},
	} {
		start := strings.Index(tc.shipped, tc.marker)
		if start < 0 {
			t.Fatalf("%s: marker missing", name)
		}
		if clause := whereClauseFrom(tc.shipped[start:]); hasTopLevelOR(clause) {
			t.Fatalf("%s: WHERE clause opened by the marker has a top-level OR; the inserted AND would bind to one disjunct:\n%s",
				name, clause)
		}
	}
}

// TestHasTopLevelORSeededViolation is the seeded RED/GREEN pair for the guard
// above: a planted top-level OR is caught, a parenthesized one is not, and
// the clause stops at ORDER BY.
func TestHasTopLevelORSeededViolation(t *testing.T) {
	t.Parallel()

	if !hasTopLevelOR(whereClauseFrom("WHERE stage = 'reducer'\n  AND domain = $1\n   OR status = 'x'\nORDER BY 1")) {
		t.Fatal("planted top-level OR not detected")
	}
	if hasTopLevelOR(whereClauseFrom("WHERE stage = 'reducer'\n  AND (a OR b)\nORDER BY x OR y")) {
		t.Fatal("parenthesized OR or ORDER BY text flagged")
	}
}

// whereClauseFrom returns the text from the marker up to the first top-level
// ORDER BY, GROUP BY, LIMIT, or end of statement.
func whereClauseFrom(text string) string {
	depth := 0
	upper := strings.ToUpper(text)
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth != 0 {
			continue
		}
		for _, stop := range []string{"ORDER BY", "GROUP BY", "LIMIT "} {
			if strings.HasPrefix(upper[i:], stop) {
				return text[:i]
			}
		}
	}
	return text
}

// hasTopLevelOR reports whether clause contains an OR keyword outside every
// parenthesis and string literal.
func hasTopLevelOR(clause string) bool {
	depth := 0
	inString := false
	upper := strings.ToUpper(clause)
	for i := 0; i < len(clause); i++ {
		switch c := clause[i]; {
		case c == '\'':
			inString = !inString
		case inString:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && strings.HasPrefix(upper[i:], "OR") &&
			(i == 0 || !isIdentByte(clause[i-1])) &&
			(i+2 >= len(clause) || !isIdentByte(clause[i+2])):
			return true
		}
	}
	return false
}

// isIdentByte reports whether b can appear in a SQL identifier.
func isIdentByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
