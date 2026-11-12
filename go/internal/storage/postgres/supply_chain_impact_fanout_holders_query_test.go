// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestFindSupplyChainImpactHoldersQueryUsesMaterializedPairUnion pins the H3
// holder-read shape (#10564): one MATERIALIZED candidate CTE that UNIONs the
// three lookup arms projecting only (scope_id, generation_id) pairs, then a
// single active-generation join. Materialization is what puts the
// advisory-group-members arm on its GIN bitmap instead of a scope nested
// loop, and the UNION deduplicates pairs before the heap-heavy join. Each
// arm keeps the literal fact kind and the tombstone predicate so the arms
// still prove their partial-index predicates under a generic plan.
func TestFindSupplyChainImpactHoldersQueryUsesMaterializedPairUnion(t *testing.T) {
	t.Parallel()

	q := FindSupplyChainImpactHoldersQuery
	if !strings.Contains(q, "AS MATERIALIZED") {
		t.Fatalf("holders query does not materialize its candidate set:\n%s", q)
	}
	if got := strings.Count(q, "UNION"); got != 2 {
		t.Fatalf("holders query has %d UNIONs, want 2 separating the three lookup arms", got)
	}
	if strings.Contains(q, "UNION ALL") {
		t.Fatalf("holders query must deduplicate (scope_id, generation_id) pairs with UNION, not UNION ALL")
	}
	if got := strings.Count(q, "JOIN ingestion_scopes"); got != 1 {
		t.Fatalf("holders query joins ingestion_scopes %d times, want exactly 1 on the pair set", got)
	}
	if !strings.Contains(q, "scope.active_generation_id") {
		t.Fatalf("holders query lost the active-generation join:\n%s", q)
	}
	if !strings.Contains(q, facts.ReducerSupplyChainImpactFindingFactKind) {
		t.Fatalf("holders query lost the literal fact kind")
	}
	if got := strings.Count(q, "is_tombstone = FALSE"); got != 3 {
		t.Fatalf("holders query carries the tombstone predicate %d times, want 3 (one per arm)", got)
	}
	for _, arm := range []string{
		"payload->>'cve_id' = ANY($1::text[])",
		"payload->>'advisory_id' = ANY($1::text[])",
		"provenance,advisory_group_members}' ?| $1::text[]",
	} {
		if !strings.Contains(q, arm) {
			t.Fatalf("holders query lost the %q arm", arm)
		}
	}
	if strings.Contains(q, "OR finding.payload") {
		t.Fatalf("holders query still ORs the three arms in one WHERE clause instead of unioning pair sets")
	}
	if !strings.Contains(q, "SELECT DISTINCT") {
		t.Fatalf("holders query lost its DISTINCT scope reporting")
	}
}
