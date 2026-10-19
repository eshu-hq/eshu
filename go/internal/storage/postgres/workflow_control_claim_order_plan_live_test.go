// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8469 step 1 claim-index proof on real PostgreSQL: the shipped claim candidate SELECT (derived from the production claimNextWorkflowWorkItemQuery constant, never hand-copied) plans as a first-row serve from workflow_work_items_claim_order_idx with no Sort node over a seeded backlog, so claim cost stays flat in backlog size; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestWorkflowClaimCandidateUsesClaimOrderIndex proves #8469 step 1: the
// claim candidate plans as an index serve, not a scan plus sort. The
// EXPLAINed statement is derived from the shipped claim constant by
// truncating at the candidate CTE boundary, so the proof tracks production
// SQL instead of a hand-frozen copy. It fails on the base tree (no
// supporting index: the plan carries a Sort over every eligible row) and
// passes with migration 200 applied.
func TestWorkflowClaimCandidateUsesClaimOrderIndex(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seedClaimDepthBacklog(t, store, ctx, "claim-order-index", base, []claimDepthKeyBacklog{
		{FairnessKey: "claim-order-index:alpha", Count: 500},
		{FairnessKey: "claim-order-index:beta", Count: 500},
	})
	// Bulk seeding leaves the table without planner statistics, and without
	// them the planner prefers a scan plus sort even with the index present
	// (verified on a scratch table). Production tables carry autovacuum
	// statistics, so ANALYZE here proves the steady-state plan, not the
	// unanalyzed transient.
	if _, err := database.ExecContext(ctx, `ANALYZE workflow_work_items`); err != nil {
		t.Fatalf("ANALYZE workflow_work_items: %v", err)
	}

	const candidateMarker = "WITH candidate AS ("
	trimmed := strings.TrimLeft(claimNextWorkflowWorkItemQuery, "\n")
	head, ok := strings.CutPrefix(trimmed, candidateMarker)
	if !ok {
		t.Fatalf("claim query does not start with %q; derivation boundary moved", candidateMarker)
	}
	// The candidate CTE ends where claimed_item begins. Splitting there keeps
	// the proof derived from the shipped statement: any production edit to
	// the candidate predicate or ORDER BY moves this boundary and fails the
	// test instead of silently proving a stale copy.
	candidate, _, found := strings.Cut(head, "),\nclaimed_item AS (")
	if !found {
		t.Fatalf("claim query candidate CTE boundary moved; update the derivation")
	}
	explainQuery := "EXPLAIN (COSTS OFF) " + candidateMarker + candidate + ") SELECT work_item_id FROM candidate"

	rows, err := database.QueryContext(
		ctx,
		explainQuery,
		string(scope.CollectorGit),
		"claim-depth-instance",
		base.Add(time.Hour).UTC(),
	)
	if err != nil {
		t.Fatalf("EXPLAIN claim candidate: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan EXPLAIN row: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read EXPLAIN rows: %v", err)
	}
	if got := plan.String(); !strings.Contains(got, "Index Scan using workflow_work_items_claim_order_idx") {
		t.Fatalf("claim candidate plan misses the claim-order index:\n%s", got)
	}
	if got := plan.String(); strings.Contains(got, "Sort") {
		t.Fatalf("claim candidate plan still sorts the eligible backlog:\n%s", got)
	}

	// Order-equivalence spot check: with the index in place the granted row
	// is still the FIFO head, so the index changes cost, not behavior.
	item, _, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "claim-depth-instance",
		OwnerID:             "claim-order-index-owner",
		ClaimID:             "claim-order-index-0",
	}, base.Add(time.Hour).UTC(), 5*time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible error = %v, want nil", err)
	}
	if !found {
		t.Fatalf("ClaimNextEligible found = false, want a grant from the seeded backlog")
	}
	if !strings.HasSuffix(item.WorkItemID, ":claim-order-index:alpha:0000") {
		t.Fatalf("first grant = %q, want the oldest seeded row (FIFO head)", item.WorkItemID)
	}
}
