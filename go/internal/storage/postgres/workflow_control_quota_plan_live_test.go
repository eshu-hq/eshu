// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8834 real PostgreSQL quota-gated claim plan proof: indexed fair candidate walk and quota primary-key budget filter without a backlog sequential scan; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestClaimQuotaGatedPlanStaysIndexServed proves #8471: the quota-gated
// claim statement plans its candidate walk off the fair-claim index and its
// budget pre-filter off the quota usage primary key, with no sequential
// scan of the backlog. The EXPLAINed text IS the shipped
// claimNextWorkflowWorkItemQuotaQuery constant with all eight parameters
// bound, so any production edit to the statement changes what is planned
// here instead of silently proving a stale copy.
func TestClaimQuotaGatedPlanStaysIndexServed(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seeded := seedClaimDepthBacklog(t, store, ctx, "quota-plan", base, []claimDepthKeyBacklog{
		{FairnessKey: "quota-plan:alpha", Count: 500},
		{FairnessKey: "quota-plan:beta", Count: 500},
	})
	// Bulk seeding leaves the tables without planner statistics, and
	// without them the planner prefers scans even with the indexes present.
	// Production tables carry autovacuum statistics, so ANALYZE here proves
	// the steady-state plan, not the unanalyzed transient.
	for _, table := range []string{"workflow_work_items", "component_quota_windows"} {
		if _, err := database.ExecContext(ctx, `ANALYZE `+table); err != nil {
			t.Fatalf("ANALYZE %s: %v", table, err)
		}
	}

	rows, err := database.QueryContext(
		ctx,
		"EXPLAIN (COSTS OFF) "+claimNextWorkflowWorkItemQuotaQuery,
		string(scope.CollectorGit),
		"claim-depth-instance",
		base.Add(time.Hour).UTC(),
		"quota-plan-owner",
		"quota-plan-claim",
		base.Add(time.Hour).Add(5*time.Minute).UTC(),
		"quota-plan-salt",
		1000,
	)
	if err != nil {
		t.Fatalf("EXPLAIN quota-gated claim: %v", err)
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
		t.Fatalf("quota-gated candidate plan misses the fair-claim index:\n%s", got)
	}
	if got := plan.String(); !strings.Contains(got, "component_quota_windows_pkey") {
		t.Fatalf("quota-gated budget pre-filter misses the quota usage primary key:\n%s", got)
	}
	if got := plan.String(); strings.Contains(got, "Seq Scan on workflow_work_items") {
		t.Fatalf("quota-gated candidate plan sequentially scans the backlog:\n%s", got)
	}

	// Liveness spot check: a quota-configured store still grants from the
	// seeded backlog instead of returning empty while budget remains.
	store.QuotaLookup = func(kind scope.CollectorKind) (workflow.ClaimQuota, bool) {
		if kind == scope.CollectorGit {
			return workflow.ClaimQuota{MaxGenerationsPerHour: 1000}, true
		}
		return workflow.ClaimQuota{}, false
	}
	item, _, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "claim-depth-instance",
		OwnerID:             "quota-plan-owner",
		ClaimID:             "quota-plan-spot",
		RotationSalt:        "quota-plan-spot",
	}, base.Add(time.Hour).UTC(), 5*time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible found = false, want a grant from the seeded backlog")
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
