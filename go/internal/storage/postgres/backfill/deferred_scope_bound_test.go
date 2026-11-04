// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"strings"
	"testing"
)

// TestDeferredScopedQueryBoundsLatestLookupToPartition is the issue #8875
// round-2 regression gate. The deferred per-partition fact load joins the
// latest-generations selection only to confirm the partition's own generation
// is still latest, so computing every scope's latest generation on every call
// (millions of calls per pg_stat_statements window) is a fixed per-call
// full-corpus scan. The lookup must stay bounded to the partition scope ($3).
// The DISTINCT ON + COALESCE precedence contract stays pinned by
// TestLatestGenerationCTE* in the postgres package.
func TestDeferredScopedQueryBoundsLatestLookupToPartition(t *testing.T) {
	t.Parallel()

	const cteOpen = "WITH latest_generations AS ("
	query := ListDeferredScopedRelationshipFactRecordsQuery
	start := strings.Index(query, cteOpen)
	if start < 0 {
		t.Fatal("deferred scoped relationship query lost its latest_generations CTE")
	}
	rest := query[start:]
	end := strings.Index(rest, "),\nsource_facts")
	if end < 0 {
		t.Fatal("cannot locate end of latest_generations CTE in deferred scoped relationship query")
	}
	latestCTE := rest[:end]
	if !strings.Contains(latestCTE, "DISTINCT ON (generation.scope_id)") {
		t.Fatal("partition-bounded latest lookup must keep the DISTINCT ON latest-generation selection")
	}
	if !strings.Contains(latestCTE, "generation.scope_id = $3") {
		t.Fatal("latest_generations lookup in the deferred scoped query must stay bounded " +
			"to the partition scope ($3): it recomputes every scope's latest generation on every per-partition call")
	}
}

// TestDeferredScopedLatestLookupDerivesFromSharedCTE pins the lockstep between
// the partition-bounded latest lookup and the shared LatestGenerationCTE: the
// bounded text must be byte-identical to the shared CTE except for the added
// scope bound, so a future edit to the shared selection cannot silently drift
// the deferred shape.
func TestDeferredScopedLatestLookupDerivesFromSharedCTE(t *testing.T) {
	t.Parallel()

	const boundLine = "\n    WHERE generation.scope_id = $3"
	if got := strings.Count(DeferredScopedLatestGenerationCTE, boundLine); got != 1 {
		t.Fatalf("bounded latest lookup must carry exactly one partition scope bound, got %d", got)
	}
	stripped := strings.Replace(DeferredScopedLatestGenerationCTE, boundLine, "", 1)
	if stripped != LatestGenerationCTE {
		t.Fatal("bounded latest lookup drifted from the shared LatestGenerationCTE beyond the partition scope bound")
	}
}
