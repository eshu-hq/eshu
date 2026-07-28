// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestRunDeferredRelationshipMaintenanceExcludesFailedGenerationsFromCorrelationReplay
// is the regression proof for the perpetual-churn hole the replay floor left
// open (PR #5850 review, P2).
//
// failProjectorWorkQuery (projector_queue_sql.go) sets the generation's status
// to 'failed' AND nulls the scope's active_generation_id in the same statement,
// so a scope whose active generation failed reaches the correlation listing with
// active_generation_id IS NULL and its LATEST generation terminally failed. The
// floor's no-active fallback picked that generation purely for being latest, so
// its succeeded reducer rows sat exactly AT the floor and were reopened on every
// drain — returning to 'succeeded' and reopening again, forever, for a
// generation whose output no query can read: every fact-backed correlation read
// surface joins scope.active_generation_id = fact.generation_id
// (facts_active_container_image_identity.go, facts_active_cicd_run_correlation.go,
// facts_active_supply_chain_impact.go), and that pointer is NULL.
//
// The exclusion is on the WORK ITEM's own generation, not on the floor's
// fallback. Moving the fallback to the newest NON-failed generation instead
// would be strictly worse: it lowers the floor, so the failed generation's rows
// (still at or above it) keep churning AND the older generation's rows start
// churning too. Neither is query-visible while active_generation_id is NULL, so
// the correct replay count for such a scope is zero.
//
// The never-activated scope in this test is the guard rail: 'pending' with a
// NULL active pointer is the activation race the whole replay exists for, and it
// MUST keep reopening. Losing it would be a far worse regression than the churn.
func TestRunDeferredRelationshipMaintenanceExcludesFailedGenerationsFromCorrelationReplay(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionReopenPartitionMemoSchema(t, db)

	base := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []memoProofFixture{
		{scopeID: "git:scope-a", genID: "gen-a", repoID: "repo-a", repoName: "alpha-service"},
		{scopeID: "git:scope-b", genID: "gen-b", repoID: "repo-b", repoName: "beta-service"},
	}
	seedMemoProofScopesAndFacts(t, ctx, db, fixtures, map[string]string{"repo-a": "beta-service"}, base)

	domain := string(reducer.DomainSupplyChainImpact)

	// The churn shape: the scope's ACTIVE generation failed, so its status is
	// 'failed' and active_generation_id is NULL. gen-failed-2 is both the latest
	// generation and the failed one.
	seedScopeGeneration(t, ctx, db, "git:scope-failed", "gen-failed-1", base, false)
	seedScopeGeneration(t, ctx, db, "git:scope-failed", "gen-failed-2", base.Add(time.Hour), true)
	failScopeGeneration(t, ctx, db, "git:scope-failed", "gen-failed-2")
	seedSucceededReopenWorkItem(t, ctx, db, "work-failed-1", "git:scope-failed", "gen-failed-1", domain, base)
	seedSucceededReopenWorkItem(t, ctx, db, "work-failed-2", "git:scope-failed", "gen-failed-2", domain, base)

	// A LATER generation failed while an OLDER one stayed active:
	// failProjectorWorkQuery only nulls the pointer when the failed generation IS
	// the active one, so this scope keeps a usable active generation. The active
	// generation's rows must still replay; the failed newer generation's must not.
	seedScopeGeneration(t, ctx, db, "git:scope-mixed", "gen-mixed-active", base, true)
	seedScopeGeneration(t, ctx, db, "git:scope-mixed", "gen-mixed-newer", base.Add(time.Hour), false)
	failScopeGeneration(t, ctx, db, "git:scope-mixed", "gen-mixed-newer")
	seedSucceededReopenWorkItem(t, ctx, db, "work-mixed-active", "git:scope-mixed", "gen-mixed-active", domain, base)
	seedSucceededReopenWorkItem(t, ctx, db, "work-mixed-newer", "git:scope-mixed", "gen-mixed-newer", domain, base)

	// The activation race: never activated, so 'pending' with a NULL active
	// pointer. This is the case the whole replay exists for.
	seedScopeGeneration(t, ctx, db, "git:scope-unactivated", "gen-unactivated", base, false)
	seedSucceededReopenWorkItem(
		t, ctx, db, "work-unactivated", "git:scope-unactivated", "gen-unactivated", domain, base,
	)

	store := NewIngestionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return base }

	if err := store.RunDeferredRelationshipMaintenance(ctx, nil, nil); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenance() error = %v", err)
	}

	for _, tc := range []struct {
		workItemID string
		want       string
		why        string
	}{
		{
			"work-failed-2", "succeeded",
			"a terminally failed generation's rows cannot be query-visible (active_generation_id is NULL), " +
				"so replaying them is pure per-drain churn",
		},
		{
			"work-failed-1", "succeeded",
			"the older generation of a failed scope is not query-visible either and must not start churning",
		},
		{"work-mixed-active", "pending", "the scope's usable active generation must still replay"},
		{"work-mixed-newer", "succeeded", "a failed generation must not replay even when the scope has an active one"},
		{
			"work-unactivated", "pending",
			"NULL active_generation_id with a non-failed latest generation is the activation race, not a failure",
		},
	} {
		if got := workItemStatus(t, ctx, db, tc.workItemID); got != tc.want {
			t.Fatalf("%s status = %q, want %q (%s)", tc.workItemID, got, tc.want, tc.why)
		}
	}

	// The churn claim is about EVERY drain, so prove the second pass is quiet
	// too: re-succeed what the first pass reopened and run maintenance again.
	if _, err := db.ExecContext(
		ctx, "UPDATE fact_work_items SET status = 'succeeded' WHERE status = 'pending'",
	); err != nil {
		t.Fatalf("re-succeed reopened work items: %v", err)
	}
	if err := store.RunDeferredRelationshipMaintenance(ctx, nil, nil); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenance() second pass error = %v", err)
	}
	for _, workItemID := range []string{"work-failed-1", "work-failed-2", "work-mixed-newer"} {
		if got := workItemStatus(t, ctx, db, workItemID); got != "succeeded" {
			t.Fatalf(
				"%s status after a second maintenance pass = %q, want %q "+
					"(failed generations must not reopen on any drain, not merely on the first)",
				workItemID, got, "succeeded",
			)
		}
	}
}

// failScopeGeneration reproduces failProjectorWorkQuery's terminal effect: the
// generation's status becomes 'failed', and the scope's active_generation_id is
// nulled only when it named that generation.
func failScopeGeneration(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		"UPDATE scope_generations SET status = 'failed' WHERE generation_id = $1", generationID); err != nil {
		t.Fatalf("fail generation %q: %v", generationID, err)
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = $1 AND active_generation_id = $2",
		scopeID, generationID); err != nil {
		t.Fatalf("null active pointer for failed generation %q: %v", generationID, err)
	}
}
