// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10791 run-age bound proofs on real PostgreSQL: stale open runs terminal-fail pending and claimed items with run_age_bound_exceeded, idempotent re-run, claim-versus-expire race with exactly-once and no deadlock, reconcile closes the run, and the guard re-admits the target; isolated disposable databases
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// The run-age-bound proofs run under the sbom_attestation kind: it sits
// outside the status-registry kind filter, so registry reads are unaffected
// by expiry transitions.
const expireStaleRunsLiveKind = scope.CollectorSBOMAttestation

func expireStaleRunsLiveInstance(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("expire-stale-%d", time.Now().UTC().UnixNano())
}

func seedExpireStaleRun(t *testing.T, ctx context.Context, database *sql.DB, runID, status string, createdAt time.Time) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_runs (run_id, trigger_kind, status, created_at, updated_at)
VALUES ($1, 'schedule', $2, $3, $3)`, runID, status, createdAt.UTC(),
	); err != nil {
		t.Fatalf("seed run %s: %v", runID, err)
	}
}

func seedExpireStaleItem(t *testing.T, ctx context.Context, database *sql.DB, workItemID, runID, instance, scopeID, status string, attemptCount int, createdAt time.Time) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_work_items (
    work_item_id, run_id, collector_kind, collector_instance_id, source_system,
    scope_id, acceptance_unit_id, source_run_id, generation_id, status,
    attempt_count, current_claim_id, current_owner_id, created_at, updated_at
) VALUES ($1, $2, $3, $4, $3, $5, $6, $7, $7, $8, $9, NULL, NULL, $10, $10)`,
		workItemID, runID, string(expireStaleRunsLiveKind), instance, scopeID,
		"unit-"+scopeID, "gen-"+scopeID, status, attemptCount, createdAt.UTC(),
	); err != nil {
		t.Fatalf("seed work item %s: %v", workItemID, err)
	}
}

func seedExpireStaleClaim(t *testing.T, ctx context.Context, database *sql.DB, claimID, workItemID, owner string, now time.Time) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_claims (
    claim_id, work_item_id, fencing_token, owner_id, status,
    claimed_at, heartbeat_at, lease_expires_at, created_at, updated_at
) VALUES ($1, $2, 1, $3, 'active', $4, $4, $5, $4, $4)`,
		claimID, workItemID, owner, now.UTC(), now.UTC().Add(time.Hour),
	); err != nil {
		t.Fatalf("seed claim %s: %v", claimID, err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE workflow_work_items SET current_claim_id = $1, current_owner_id = $2 WHERE work_item_id = $3`,
		claimID, owner, workItemID,
	); err != nil {
		t.Fatalf("attach claim %s: %v", claimID, err)
	}
}

func expireStaleItemStatus(t *testing.T, ctx context.Context, database *sql.DB, workItemID string) (status, class string, terminal bool) {
	t.Helper()
	if err := database.QueryRowContext(ctx, `
SELECT status, COALESCE(last_failure_class, ''), COALESCE(last_failure_terminal, FALSE)
FROM workflow_work_items WHERE work_item_id = $1`, workItemID,
	).Scan(&status, &class, &terminal); err != nil {
		t.Fatalf("read work item %s: %v", workItemID, err)
	}
	return status, class, terminal
}

// TestExpireStaleRunsLiveHappyPath proves the bound on a live database: a
// run older than maxAge has its pending and claimed items moved to
// failed_terminal with the run_age_bound_exceeded class, while a fresh
// run's pending item is untouched (eshu-hq/eshu#10791).
func TestExpireStaleRunsLiveHappyPath(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	staleAt := now.Add(-25 * time.Hour)
	instance := expireStaleRunsLiveInstance(t)

	seedExpireStaleRun(t, ctx, database, "expire-happy-stale-run", "collection_active", staleAt)
	seedExpireStaleItem(t, ctx, database, "expire-happy-pending", "expire-happy-stale-run", instance, "expire-happy-scope-1", "pending", 1, staleAt)
	seedExpireStaleItem(t, ctx, database, "expire-happy-claimed", "expire-happy-stale-run", instance, "expire-happy-scope-2", "claimed", 2, staleAt)
	seedExpireStaleClaim(t, ctx, database, "expire-happy-claim", "expire-happy-claimed", "owner-live", now)
	seedExpireStaleRun(t, ctx, database, "expire-happy-fresh-run", "collection_active", now)
	seedExpireStaleItem(t, ctx, database, "expire-happy-fresh", "expire-happy-fresh-run", instance, "expire-happy-scope-3", "pending", 0, now)

	expired, err := store.ExpireStaleOpenRuns(ctx, now, 24*time.Hour)
	if err != nil {
		t.Fatalf("ExpireStaleOpenRuns() error = %v, want nil", err)
	}
	if got, want := len(expired), 2; got != want {
		t.Fatalf("expired items = %d, want %d", got, want)
	}
	for _, id := range []string{"expire-happy-pending", "expire-happy-claimed"} {
		status, class, terminal := expireStaleItemStatus(t, ctx, database, id)
		if status != "failed_terminal" || !terminal {
			t.Fatalf("item %s = %s terminal=%t, want failed_terminal true", id, status, terminal)
		}
		if class != string(workflow.FailureClassRunAgeBoundExceeded) {
			t.Fatalf("item %s class = %q, want run_age_bound_exceeded", id, class)
		}
	}
	if status, _, _ := expireStaleItemStatus(t, ctx, database, "expire-happy-fresh"); status != "pending" {
		t.Fatalf("fresh item status = %s, want pending (untouched)", status)
	}
}

// TestExpireStaleRunsLiveIdempotent proves the re-run is a safe no-op.
func TestExpireStaleRunsLiveIdempotent(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	staleAt := now.Add(-25 * time.Hour)
	instance := expireStaleRunsLiveInstance(t)

	seedExpireStaleRun(t, ctx, database, "expire-idem-run", "collection_active", staleAt)
	seedExpireStaleItem(t, ctx, database, "expire-idem-item", "expire-idem-run", instance, "expire-idem-scope", "pending", 0, staleAt)

	if _, err := store.ExpireStaleOpenRuns(ctx, now, 24*time.Hour); err != nil {
		t.Fatalf("first ExpireStaleOpenRuns() error = %v, want nil", err)
	}
	expired, err := store.ExpireStaleOpenRuns(ctx, now, 24*time.Hour)
	if err != nil {
		t.Fatalf("second ExpireStaleOpenRuns() error = %v, want nil", err)
	}
	if len(expired) != 0 {
		t.Fatalf("second expire returned %d items, want 0", len(expired))
	}
}

// TestExpireStaleRunsLiveClaimRace proves expiry racing a live completion
// gives exactly one winner with no deadlock: either the completion lands
// and expiry skips the row, or expiry terminals it and the completion is
// fenced with ErrClaimRejected.
func TestExpireStaleRunsLiveClaimRace(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	staleAt := now.Add(-25 * time.Hour)
	instance := expireStaleRunsLiveInstance(t)

	seedExpireStaleRun(t, ctx, database, "expire-race-run", "collection_active", staleAt)
	seedExpireStaleItem(t, ctx, database, "expire-race-item", "expire-race-run", instance, "expire-race-scope", "claimed", 1, staleAt)
	seedExpireStaleClaim(t, ctx, database, "expire-race-claim", "expire-race-item", "owner-race", now)

	mutation := workflow.ClaimMutation{
		WorkItemID:   "expire-race-item",
		ClaimID:      "expire-race-claim",
		FencingToken: 1,
		OwnerID:      "owner-race",
		ObservedAt:   now,
	}
	var wg sync.WaitGroup
	wg.Add(2)
	var expireErr, completeErr error
	go func() {
		defer wg.Done()
		_, expireErr = store.ExpireStaleOpenRuns(ctx, now, 24*time.Hour)
	}()
	go func() {
		defer wg.Done()
		completeErr = store.CompleteClaim(ctx, mutation)
	}()
	wg.Wait()

	status, _, _ := expireStaleItemStatus(t, ctx, database, "expire-race-item")
	switch status {
	case "completed":
		if completeErr != nil {
			t.Fatalf("completed but CompleteClaim error = %v", completeErr)
		}
	case "failed_terminal":
		if !errors.Is(completeErr, workflow.ErrClaimRejected) {
			t.Fatalf("expired but CompleteClaim error = %v, want ErrClaimRejected", completeErr)
		}
		if expireErr != nil {
			t.Fatalf("ExpireStaleOpenRuns error = %v, want nil", expireErr)
		}
	default:
		t.Fatalf("item status = %s, want completed or failed_terminal (exactly one winner)", status)
	}
}

// TestExpireStaleRunsLiveGuardRelease proves the wedge clears end to end:
// after expiry, run reconciliation closes the stale run failed, and the
// same target admits fresh work again (eshu-hq/eshu#10791 decision 5a).
func TestExpireStaleRunsLiveGuardRelease(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	staleAt := now.Add(-25 * time.Hour)
	instance := expireStaleRunsLiveInstance(t)
	scopeID := "expire-guard-scope"

	seedExpireStaleRun(t, ctx, database, "expire-guard-run", "collection_active", staleAt)
	seedExpireStaleItem(t, ctx, database, "expire-guard-item", "expire-guard-run", instance, scopeID, "pending", 1, staleAt)

	// Guard holds while the run is open: the same target admits nothing.
	admission, err := store.CreateRunWithWorkItemsIfNoOpenTargets(ctx,
		workflow.Run{RunID: "expire-guard-probe", TriggerKind: "schedule", Status: "collection_pending", RequestedScopeSet: "[]", CreatedAt: now, UpdatedAt: now},
		[]workflow.WorkItem{{
			WorkItemID: "expire-guard-probe-item", RunID: "expire-guard-probe",
			CollectorKind: expireStaleRunsLiveKind, CollectorInstanceID: instance,
			SourceSystem: string(expireStaleRunsLiveKind), ScopeID: scopeID,
			AcceptanceUnitID: "unit-" + scopeID, SourceRunID: "gen-probe", GenerationID: "gen-probe",
			Status: "pending", CreatedAt: now, UpdatedAt: now,
		}})
	if err != nil {
		t.Fatalf("probe admission error = %v, want nil", err)
	}
	if admission.InsertedWorkItems != 0 {
		t.Fatalf("probe inserted %d items, want 0 (guard must block while the run is open)", admission.InsertedWorkItems)
	}

	if _, err := store.ExpireStaleOpenRuns(ctx, now, 24*time.Hour); err != nil {
		t.Fatalf("ExpireStaleOpenRuns() error = %v, want nil", err)
	}
	if _, err := store.ReconcileWorkflowRuns(ctx, now); err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
	var runStatus string
	if err := database.QueryRowContext(ctx, `SELECT status FROM workflow_runs WHERE run_id = 'expire-guard-run'`).Scan(&runStatus); err != nil {
		t.Fatalf("read run status: %v", err)
	}
	if runStatus != "failed" {
		t.Fatalf("stale run status = %s, want failed after expire + reconcile", runStatus)
	}

	admission, err = store.CreateRunWithWorkItemsIfNoOpenTargets(ctx,
		workflow.Run{RunID: "expire-guard-readmit", TriggerKind: "schedule", Status: "collection_pending", RequestedScopeSet: "[]", CreatedAt: now, UpdatedAt: now},
		[]workflow.WorkItem{{
			WorkItemID: "expire-guard-readmit-item", RunID: "expire-guard-readmit",
			CollectorKind: expireStaleRunsLiveKind, CollectorInstanceID: instance,
			SourceSystem: string(expireStaleRunsLiveKind), ScopeID: scopeID,
			AcceptanceUnitID: "unit-" + scopeID, SourceRunID: "gen-readmit", GenerationID: "gen-readmit",
			Status: "pending", CreatedAt: now, UpdatedAt: now,
		}})
	if err != nil {
		t.Fatalf("re-admission error = %v, want nil", err)
	}
	if admission.InsertedWorkItems != 1 {
		t.Fatalf("re-admission inserted %d items, want 1 (guard must release after close)", admission.InsertedWorkItems)
	}
}
