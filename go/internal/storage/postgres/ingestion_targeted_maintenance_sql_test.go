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
			marker:   relationshipReopenStageMarker,
			conjunct: relationshipReopenPartitionConjunct,
		},
		{
			name:     "code_import_repo_edge reopen bounded by partition",
			shipped:  listSucceededCodeImportRepoEdgeWorkItemsQuery,
			derived:  listSucceededCodeImportRepoEdgeWorkItemsForPartitionsQuery,
			marker:   relationshipReopenStageMarker,
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
