// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8469 step 3 claim-plan proof on real PostgreSQL: the priority-then-rotation candidate SELECT (derived from the production claimNextWorkflowWorkItemQuery constant, never hand-copied) plans as loose distinct-pair probes from workflow_work_items_fair_claim_idx with no sequential scan of the backlog, so claim cost scales with distinct keys rather than backlog size; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestWorkflowClaimCandidateUsesFairClaimIndex proves #8469 step 3: the
// fair-claim candidate plans as index probes, never as a backlog scan. The
// EXPLAINed statement is derived from the shipped claim constant by
// truncating at the candidate CTE boundary, so the proof tracks production
// SQL instead of a hand-frozen copy.
func TestWorkflowClaimCandidateUsesFairClaimIndex(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seeded := seedClaimDepthBacklog(t, store, ctx, "claim-fair-index", base, []claimDepthKeyBacklog{
		{FairnessKey: "claim-fair-index:alpha", Count: 500},
		{FairnessKey: "claim-fair-index:beta", Count: 500},
	})
	// Bulk seeding leaves the table without planner statistics, and without
	// them the planner prefers scans even with the index present (verified
	// on a scratch table in step 1). Production tables carry autovacuum
	// statistics, so ANALYZE here proves the steady-state plan, not the
	// unanalyzed transient.
	if _, err := database.ExecContext(ctx, `ANALYZE workflow_work_items`); err != nil {
		t.Fatalf("ANALYZE workflow_work_items: %v", err)
	}

	const candidateMarker = "WITH RECURSIVE keys AS ("
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

	// database/sql binds positionally, so the sparse $7 has no slot: the
	// test fixes the draw salt to a literal in the derived text and binds
	// only $1-$3. The salt value is not the contract under proof (the
	// rotation tests prove the draw honors it); the plan shape is.
	explainQuery = strings.ReplaceAll(explainQuery, "$7", "'claim-fair-index-plan'")
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
	if got := plan.String(); !strings.Contains(got, "Index Scan using workflow_work_items_fair_claim_idx") {
		t.Fatalf("fair-claim candidate plan misses the fair-claim index:\n%s", got)
	}
	if got := plan.String(); strings.Contains(got, "Seq Scan on workflow_work_items") {
		t.Fatalf("fair-claim candidate plan sequentially scans the backlog:\n%s", got)
	}

	// Liveness spot check: the rewritten claim still grants from the seeded
	// backlog instead of returning empty while eligible work exists.
	item, _, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "claim-depth-instance",
		OwnerID:             "claim-fair-index-owner",
		ClaimID:             "claim-fair-index-0",
		RotationSalt:        "claim-fair-index-spot",
	}, base.Add(time.Hour).UTC(), 5*time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible error = %v, want nil", err)
	}
	if !found {
		t.Fatalf("ClaimNextEligible found = false, want a grant from the seeded backlog")
	}
	known := false
	for _, id := range seeded {
		if id == item.WorkItemID {
			known = true
			break
		}
	}
	if !known {
		t.Fatalf("granted %q, want a seeded backlog row", item.WorkItemID)
	}
}
