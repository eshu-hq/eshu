// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// The live retire proofs run under the sbom_attestation kind: it matches the
// issue's collector family and sits outside the status-registry kind filter
// (oci_registry, package_registry), so the registry read must be
// byte-identical before and after a retirement.
const retireUnclaimedLiveKind = scope.CollectorSBOMAttestation

func retireUnclaimedLiveInstance(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("retire-unclaimed-%d", time.Now().UTC().UnixNano())
}

// seedRetireUnclaimedInstance inserts one collector_instances row. A
// deconfigured instance is enabled=false with deactivated_at set, the shape
// the reconciler writes for stale rows; an active one is enabled with no
// deactivated_at.
func seedRetireUnclaimedInstance(t *testing.T, ctx context.Context, database *sql.DB, instance string, active bool) {
	t.Helper()
	now := time.Now().UTC()
	var deactivated any
	enabled := false
	if active {
		enabled = true
	} else {
		deactivated = now
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO collector_instances (
    instance_id, collector_kind, mode, enabled, bootstrap, claims_enabled,
    configuration, last_observed_at, deactivated_at, created_at, updated_at
) VALUES ($1, $2, 'scheduled', $3, FALSE, FALSE, '{}'::jsonb, $4, $5, $4, $4)`,
		instance, string(retireUnclaimedLiveKind), enabled, now, deactivated,
	); err != nil {
		t.Fatalf("seed collector instance %s: %v", instance, err)
	}
}

func seedRetireUnclaimedRun(t *testing.T, ctx context.Context, database *sql.DB, runID, status string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_runs (run_id, trigger_kind, status, created_at, updated_at)
VALUES ($1, 'schedule', $2, $3, $3)`, runID, status, now,
	); err != nil {
		t.Fatalf("seed run %s: %v", runID, err)
	}
}

// seedRetireUnclaimedItem inserts one pending, never-claimed work item.
func seedRetireUnclaimedItem(t *testing.T, ctx context.Context, database *sql.DB, workItemID, runID, instance, scopeID string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_work_items (
    work_item_id, run_id, collector_kind, collector_instance_id, source_system,
    scope_id, acceptance_unit_id, source_run_id, generation_id, status,
    attempt_count, current_claim_id, current_owner_id, created_at, updated_at
) VALUES ($1, $2, $3, $4, $3, $5, $6, $7, $7, 'pending', 0, NULL, NULL, $8, $8)`,
		workItemID, runID, string(retireUnclaimedLiveKind), instance, scopeID,
		"unit-"+scopeID, "gen-"+scopeID, now,
	); err != nil {
		t.Fatalf("seed work item %s: %v", workItemID, err)
	}
}

func retireUnclaimedItemStatus(t *testing.T, ctx context.Context, database *sql.DB, workItemID string) (status, class, operation string, terminal bool) {
	t.Helper()
	if err := database.QueryRowContext(ctx, `
SELECT status, COALESCE(last_failure_class, ''), COALESCE(last_failure_operation, ''), COALESCE(last_failure_terminal, FALSE)
FROM workflow_work_items WHERE work_item_id = $1`, workItemID,
	).Scan(&status, &class, &operation, &terminal); err != nil {
		t.Fatalf("read work item %s: %v", workItemID, err)
	}
	return status, class, operation, terminal
}

// TestRetireUnclaimedLiveHappyPath proves the terminal outcome on a live
// database: the two never-claimed rows of a deconfigured instance move to
// failed_terminal with the operator-retired class and operation, while a
// claimed row, a completed row, and an attempted row of the same instance
// are untouched (eshu-hq/eshu#10532).
func TestRetireUnclaimedLiveHappyPath(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	instance := retireUnclaimedLiveInstance(t)
	seedRetireUnclaimedInstance(t, ctx, database, instance, false)
	runID := "retire-happy-run"
	seedRetireUnclaimedRun(t, ctx, database, runID, "collection_active")
	seedRetireUnclaimedItem(t, ctx, database, "retire-happy-w1", runID, instance, "retire-happy-scope-1")
	seedRetireUnclaimedItem(t, ctx, database, "retire-happy-w2", runID, instance, "retire-happy-scope-2")

	now := time.Now().UTC()
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_work_items (
    work_item_id, run_id, collector_kind, collector_instance_id, source_system,
    scope_id, acceptance_unit_id, source_run_id, generation_id, status,
    attempt_count, current_claim_id, current_owner_id, created_at, updated_at
) VALUES
    ('retire-happy-claimed', $1, $2, $3, $2, 'retire-happy-scope-3', 'unit-3', 'gen-3', 'gen-3',
     'claimed', 1, 'claim-live', 'owner-live', $4, $4),
    ('retire-happy-completed', $1, $2, $3, $2, 'retire-happy-scope-4', 'unit-4', 'gen-4', 'gen-4',
     'completed', 1, NULL, NULL, $4, $4),
    ('retire-happy-attempted', $1, $2, $3, $2, 'retire-happy-scope-5', 'unit-5', 'gen-5', 'gen-5',
     'pending', 2, NULL, NULL, $4, $4)`,
		runID, string(retireUnclaimedLiveKind), instance, now,
	); err != nil {
		t.Fatalf("seed non-eligible rows: %v", err)
	}

	retired, err := store.RetireUnclaimedWorkItems(ctx, retireUnclaimedLiveKind, instance, "dark scope cleanup")
	if err != nil {
		t.Fatalf("RetireUnclaimedWorkItems() error = %v, want nil", err)
	}
	if len(retired) != 2 {
		t.Fatalf("retired %d items, want 2: %+v", len(retired), retired)
	}
	if retired[0].WorkItemID != "retire-happy-w1" || retired[1].WorkItemID != "retire-happy-w2" {
		t.Fatalf("retired = %+v, want [w1 w2] in work_item_id order", retired)
	}
	for _, item := range retired {
		status, class, operation, terminal := retireUnclaimedItemStatus(t, ctx, database, item.WorkItemID)
		if status != "failed_terminal" || class != workflow.FailureClassOperatorRetiredUnclaimed ||
			operation != workflow.FailureOperationOperatorRetire || !terminal {
			t.Errorf("%s: status=%q class=%q operation=%q terminal=%t, want failed_terminal/%q/%q/true",
				item.WorkItemID, status, class, operation, terminal,
				workflow.FailureClassOperatorRetiredUnclaimed, workflow.FailureOperationOperatorRetire)
		}
		var message string
		if err := database.QueryRowContext(ctx,
			`SELECT COALESCE(last_failure_message, '') FROM workflow_work_items WHERE work_item_id = $1`,
			item.WorkItemID).Scan(&message); err != nil {
			t.Fatalf("read failure message: %v", err)
		}
		if !strings.Contains(message, instance) || !strings.Contains(message, "dark scope cleanup") {
			t.Errorf("%s: failure message %q names neither the instance nor the note", item.WorkItemID, message)
		}
	}
	for _, untouched := range []string{"retire-happy-claimed", "retire-happy-completed", "retire-happy-attempted"} {
		status, _, _, _ := retireUnclaimedItemStatus(t, ctx, database, untouched)
		var want string
		switch untouched {
		case "retire-happy-claimed":
			want = "claimed"
		case "retire-happy-completed":
			want = "completed"
		default:
			want = "pending"
		}
		if status != want {
			t.Errorf("%s: status = %q, want %q (must be untouched)", untouched, status, want)
		}
	}
}

// TestRetireUnclaimedLiveIdempotent proves the second call retires nothing:
// the mutation is state-idempotent, so a retried operator request or a
// repeated reconciler pass is a safe no-op (eshu-hq/eshu#10532).
func TestRetireUnclaimedLiveIdempotent(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	instance := retireUnclaimedLiveInstance(t)
	seedRetireUnclaimedInstance(t, ctx, database, instance, false)
	runID := "retire-idempotent-run"
	seedRetireUnclaimedRun(t, ctx, database, runID, "collection_active")
	seedRetireUnclaimedItem(t, ctx, database, "retire-idem-w1", runID, instance, "retire-idem-scope-1")

	first, err := store.RetireUnclaimedWorkItems(ctx, retireUnclaimedLiveKind, instance, "first")
	if err != nil {
		t.Fatalf("first RetireUnclaimedWorkItems() error = %v, want nil", err)
	}
	if len(first) != 1 {
		t.Fatalf("first call retired %d items, want 1", len(first))
	}
	second, err := store.RetireUnclaimedWorkItems(ctx, retireUnclaimedLiveKind, instance, "second")
	if err != nil {
		t.Fatalf("second RetireUnclaimedWorkItems() error = %v, want nil", err)
	}
	if len(second) != 0 {
		t.Fatalf("second call retired %d items, want 0", len(second))
	}
}

// TestRetireUnclaimedLiveGateRefusals proves the deconfigured-only validity
// against live rows: an enabled instance fails closed (a live collector may
// still claim), and an unknown instance fails closed (a typo, never a
// silent no-op). Neither refusal writes anything (eshu-hq/eshu#10532).
func TestRetireUnclaimedLiveGateRefusals(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	active := retireUnclaimedLiveInstance(t) + "-active"
	seedRetireUnclaimedInstance(t, ctx, database, active, true)
	runID := "retire-gate-run"
	seedRetireUnclaimedRun(t, ctx, database, runID, "collection_active")
	seedRetireUnclaimedItem(t, ctx, database, "retire-gate-w1", runID, active, "retire-gate-scope-1")

	if _, err := store.RetireUnclaimedWorkItems(ctx, retireUnclaimedLiveKind, active, "note"); err == nil {
		t.Fatal("active instance retire error = nil, want ErrWorkflowRetireInstanceActive")
	} else if !strings.Contains(err.Error(), ErrWorkflowRetireInstanceActive.Error()) {
		t.Fatalf("active instance retire error = %v, want ErrWorkflowRetireInstanceActive", err)
	}
	if _, err := store.RetireUnclaimedWorkItems(ctx, retireUnclaimedLiveKind, "never-existed-here", "note"); err == nil {
		t.Fatal("unknown instance retire error = nil, want ErrWorkflowRetireUnknownInstance")
	} else if !strings.Contains(err.Error(), ErrWorkflowRetireUnknownInstance.Error()) {
		t.Fatalf("unknown instance retire error = %v, want ErrWorkflowRetireUnknownInstance", err)
	}
	status, _, _, _ := retireUnclaimedItemStatus(t, ctx, database, "retire-gate-w1")
	if status != "pending" {
		t.Fatalf("refused row status = %q, want pending (refusals must not write)", status)
	}
}

// TestRetireUnclaimedLiveClaimRace proves claim-versus-retire never both
// win and never deadlock: barrier-synchronized claimants and one retirer
// race over the same rows, and every row ends claimed exactly once or
// retired exactly once, with no 40P01 on either side (eshu-hq/eshu#10532).
func TestRetireUnclaimedLiveClaimRace(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	instance := retireUnclaimedLiveInstance(t)
	seedRetireUnclaimedInstance(t, ctx, database, instance, false)
	runID := "retire-race-run"
	seedRetireUnclaimedRun(t, ctx, database, runID, "collection_active")
	const racedRows = 12
	for i := 0; i < racedRows; i++ {
		seedRetireUnclaimedItem(t, ctx, database,
			fmt.Sprintf("retire-race-w%02d", i), runID, instance, fmt.Sprintf("retire-race-scope-%02d", i))
	}
	// Pre-claim four rows before the race starts. A retire that lands after
	// a claim commits must drop those rows on its locked-row recheck
	// (attempt_count is 1, a claim owns them), so the claim-first order is
	// covered deterministically instead of by timing luck: the retire
	// statement is an order of magnitude cheaper than the fair-claim
	// lottery, and a pure barrier start lets the retire sweep everything.
	const preClaimed = 4
	for i := 0; i < preClaimed; i++ {
		_, _, ok, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
			CollectorKind:       retireUnclaimedLiveKind,
			CollectorInstanceID: instance,
			OwnerID:             "race-pre-owner",
			ClaimID:             fmt.Sprintf("race-pre-claim-%d", i),
			RotationSalt:        "retire-race-salt",
		}, time.Now().UTC(), workflow.DefaultClaimLeaseTTL())
		if err != nil {
			t.Fatalf("pre-claim %d: %v", i, err)
		}
		if !ok {
			t.Fatalf("pre-claim %d: ok = false, want a row", i)
		}
	}

	start := make(chan struct{})
	drained := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	var retired retiredCounter
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		// Loop until nothing is left so claims in flight win rows
		// between retire statements; every row still gets exactly one
		// winner. Two consecutive empty passes confirm quiescence: a
		// claim mid-flight during the first pass has committed by the
		// second, and claim statements are synchronous, so nothing
		// undecided remains once the claimants observe the drain.
		emptyPasses := 0
		for {
			items, err := store.RetireUnclaimedWorkItems(ctx, retireUnclaimedLiveKind, instance, "race")
			if err != nil {
				errs <- fmt.Errorf("retire: %w", err)
				return
			}
			retired.add(len(items))
			if len(items) == 0 {
				emptyPasses++
				if emptyPasses >= 2 {
					close(drained)
					return
				}
			} else {
				emptyPasses = 0
			}
			time.Sleep(time.Millisecond)
		}
	}()
	const claimants = 4
	for c := 0; c < claimants; c++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			// Retry empty picks: a rival may hold the head under
			// SKIP LOCKED while rows remain. Sustained pressure is
			// what makes the interleaving real.
			for {
				select {
				case <-drained:
					return
				default:
				}
				_, _, ok, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
					CollectorKind:       retireUnclaimedLiveKind,
					CollectorInstanceID: instance,
					OwnerID:             fmt.Sprintf("race-owner-%d", worker),
					ClaimID:             fmt.Sprintf("race-claim-%d-%d", worker, time.Now().UTC().UnixNano()),
					RotationSalt:        "retire-race-salt",
				}, time.Now().UTC(), workflow.DefaultClaimLeaseTTL())
				if err != nil {
					if isRetireRaceDeadlock(err) {
						errs <- fmt.Errorf("claim: deadlock: %w", err)
					} else {
						errs <- fmt.Errorf("claim: %w", err)
					}
					return
				}
				if !ok {
					time.Sleep(time.Millisecond)
					continue
				}
			}
		}(c)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("race error: %v", err)
	}

	var claimed, retiredCount, pending int
	if err := database.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE status = 'claimed'),
       COUNT(*) FILTER (WHERE status = 'failed_terminal'),
       COUNT(*) FILTER (WHERE status = 'pending')
FROM workflow_work_items WHERE run_id = $1`, runID,
	).Scan(&claimed, &retiredCount, &pending); err != nil {
		t.Fatalf("count outcomes: %v", err)
	}
	if retired.load() != retiredCount {
		t.Fatalf("retire reported %d, table holds %d failed_terminal", retired.load(), retiredCount)
	}
	if claimed+retiredCount != racedRows || pending != 0 {
		t.Fatalf("claimed=%d retired=%d pending=%d, want sum %d with 0 pending (every row exactly one winner)",
			claimed, retiredCount, pending, racedRows)
	}
	t.Logf("race split: claimed=%d retired=%d", claimed, retiredCount)
	// The pre-claimed rows must still be claimed: the retire's locked-row
	// recheck drops rows a claim already owns. Their claims stay active,
	// so no retired row may carry an active claim either.
	var preClaimedSurvived int
	if err := database.QueryRowContext(ctx, `
SELECT COUNT(*) FROM workflow_work_items AS item
JOIN workflow_claims AS claim ON claim.work_item_id = item.work_item_id
WHERE item.run_id = $1 AND claim.owner_id = 'race-pre-owner' AND claim.status = 'active'
  AND item.status = 'claimed'`, runID,
	).Scan(&preClaimedSurvived); err != nil {
		t.Fatalf("count surviving pre-claims: %v", err)
	}
	if preClaimedSurvived != preClaimed {
		t.Fatalf("surviving pre-claims = %d, want %d (claim-first rows must survive the retire)", preClaimedSurvived, preClaimed)
	}
	var both int
	if err := database.QueryRowContext(ctx, `
SELECT COUNT(*) FROM workflow_work_items AS item
JOIN workflow_claims AS claim ON claim.work_item_id = item.work_item_id
WHERE item.run_id = $1 AND item.status = 'failed_terminal' AND claim.status = 'active'`, runID,
	).Scan(&both); err != nil {
		t.Fatalf("count double winners: %v", err)
	}
	if both != 0 {
		t.Fatalf("%d rows both claimed-active and retired, want 0", both)
	}
}

// TestRetireUnclaimedLiveHeldRow proves the SKIP LOCKED half of the race
// deterministically: with one row held under an uncommitted row lock, the
// retire skips it and takes the rest; the held row stays pending and a
// later claim wins it (eshu-hq/eshu#10532).
func TestRetireUnclaimedLiveHeldRow(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	instance := retireUnclaimedLiveInstance(t)
	seedRetireUnclaimedInstance(t, ctx, database, instance, false)
	runID := "retire-held-run"
	seedRetireUnclaimedRun(t, ctx, database, runID, "collection_active")
	seedRetireUnclaimedItem(t, ctx, database, "retire-held-w1", runID, instance, "retire-held-scope-1")
	seedRetireUnclaimedItem(t, ctx, database, "retire-held-w2", runID, instance, "retire-held-scope-2")

	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder tx: %v", err)
	}
	var held string
	if err := holder.QueryRowContext(ctx,
		`SELECT work_item_id FROM workflow_work_items WHERE work_item_id = $1 FOR UPDATE`,
		"retire-held-w1").Scan(&held); err != nil {
		_ = holder.Rollback()
		t.Fatalf("lock held row: %v", err)
	}

	retired, err := store.RetireUnclaimedWorkItems(ctx, retireUnclaimedLiveKind, instance, "held-row")
	if err != nil {
		_ = holder.Rollback()
		t.Fatalf("RetireUnclaimedWorkItems() error = %v, want nil", err)
	}
	if len(retired) != 1 || retired[0].WorkItemID != "retire-held-w2" {
		_ = holder.Rollback()
		t.Fatalf("retired = %+v, want only [retire-held-w2]", retired)
	}
	if err := holder.Rollback(); err != nil {
		t.Fatalf("rollback holder: %v", err)
	}

	status, _, _, _ := retireUnclaimedItemStatus(t, ctx, database, "retire-held-w1")
	if status != "pending" {
		t.Fatalf("held row status = %q, want pending", status)
	}
	_, _, ok, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       retireUnclaimedLiveKind,
		CollectorInstanceID: instance,
		OwnerID:             "held-row-owner",
		ClaimID:             "held-row-claim",
		RotationSalt:        "retire-held-salt",
	}, time.Now().UTC(), workflow.DefaultClaimLeaseTTL())
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("ClaimNextEligible() ok = false, want the held pending row")
	}
}

func isRetireRaceDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40P01"
	}
	return strings.Contains(err.Error(), "40P01")
}

type retiredCounter struct {
	mu sync.Mutex
	n  int
}

func (a *retiredCounter) add(n int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n += n
}

func (a *retiredCounter) load() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.n
}

// TestRetireUnclaimedLiveReconcileClosesRun proves the run-reconciliation
// consequence: once the retire moves a run's last open items to
// failed_terminal, the next ReconcileWorkflowRuns pass closes the run as
// failed (nothing pending or claimed, terminal failure present), which is
// what releases the open-target guard for those targets
// (eshu-hq/eshu#10532).
func TestRetireUnclaimedLiveReconcileClosesRun(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	instance := retireUnclaimedLiveInstance(t)
	seedRetireUnclaimedInstance(t, ctx, database, instance, false)
	runID := "retire-reconcile-run"
	seedRetireUnclaimedRun(t, ctx, database, runID, "collection_pending")
	seedRetireUnclaimedItem(t, ctx, database, "retire-recon-w1", runID, instance, "retire-recon-scope-1")
	seedRetireUnclaimedItem(t, ctx, database, "retire-recon-w2", runID, instance, "retire-recon-scope-2")

	if _, err := store.RetireUnclaimedWorkItems(ctx, retireUnclaimedLiveKind, instance, "reconcile"); err != nil {
		t.Fatalf("RetireUnclaimedWorkItems() error = %v, want nil", err)
	}
	reconciled, err := store.ReconcileWorkflowRuns(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
	if reconciled == 0 {
		t.Fatal("ReconcileWorkflowRuns() reconciled 0 runs, want >= 1")
	}
	var status string
	var finished sql.NullTime
	if err := database.QueryRowContext(ctx,
		`SELECT status, finished_at FROM workflow_runs WHERE run_id = $1`, runID,
	).Scan(&status, &finished); err != nil {
		t.Fatalf("read run status: %v", err)
	}
	if status != string(workflow.RunStatusFailed) {
		t.Fatalf("run status = %q, want failed", status)
	}
	if !finished.Valid {
		t.Fatal("run finished_at is NULL, want set on the terminal transition")
	}
}

// retireUnclaimedAdmissionItem builds a valid guarded-admission work item
// for the shared targets.
func retireUnclaimedAdmissionItem(workItemID, runID, instance, scopeID string) workflow.WorkItem {
	now := time.Now().UTC()
	return workflow.WorkItem{
		WorkItemID:          workItemID,
		RunID:               runID,
		CollectorKind:       retireUnclaimedLiveKind,
		CollectorInstanceID: instance,
		SourceSystem:        string(retireUnclaimedLiveKind),
		ScopeID:             scopeID,
		AcceptanceUnitID:    "unit-" + scopeID,
		SourceRunID:         "gen-" + scopeID,
		GenerationID:        "gen-" + scopeID,
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

// TestRetireUnclaimedLiveGuardRelease proves the guard-release consequence
// end to end: while the run holds pending rows, guarded admission for the
// same targets admits nothing (the open-target suppression the inert rows
// provide today); after the retire and the reconcile pass that closes the
// run, the same targets admit (eshu-hq/eshu#10532). The disposition stays
// valid only because the instance is deconfigured, so no planner re-admits
// in production; this exercises the consequence, not a production plan.
func TestRetireUnclaimedLiveGuardRelease(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	instance := retireUnclaimedLiveInstance(t)
	seedRetireUnclaimedInstance(t, ctx, database, instance, false)
	runID := "retire-guard-run"
	seedRetireUnclaimedRun(t, ctx, database, runID, "collection_pending")
	seedRetireUnclaimedItem(t, ctx, database, "retire-guard-w1", runID, instance, "retire-guard-scope-1")
	seedRetireUnclaimedItem(t, ctx, database, "retire-guard-w2", runID, instance, "retire-guard-scope-2")

	suppressed, err := store.CreateRunWithWorkItemsIfNoOpenTargets(ctx,
		workflow.Run{
			RunID:             "retire-guard-run-2",
			TriggerKind:       workflow.TriggerKindSchedule,
			Status:            workflow.RunStatusCollectionPending,
			RequestedScopeSet: "[]",
			CreatedAt:         time.Now().UTC(),
			UpdatedAt:         time.Now().UTC(),
		},
		[]workflow.WorkItem{
			retireUnclaimedAdmissionItem("retire-guard-w1b", "retire-guard-run-2", instance, "retire-guard-scope-1"),
			retireUnclaimedAdmissionItem("retire-guard-w2b", "retire-guard-run-2", instance, "retire-guard-scope-2"),
		})
	if err != nil {
		t.Fatalf("suppressed admission error = %v, want nil", err)
	}
	if suppressed.InsertedWorkItems != 0 {
		t.Fatalf("suppressed admission inserted %d items, want 0 (open targets must suppress)", suppressed.InsertedWorkItems)
	}

	if _, err := store.RetireUnclaimedWorkItems(ctx, retireUnclaimedLiveKind, instance, "guard"); err != nil {
		t.Fatalf("RetireUnclaimedWorkItems() error = %v, want nil", err)
	}
	if _, err := store.ReconcileWorkflowRuns(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}

	released, err := store.CreateRunWithWorkItemsIfNoOpenTargets(ctx,
		workflow.Run{
			RunID:             "retire-guard-run-3",
			TriggerKind:       workflow.TriggerKindSchedule,
			Status:            workflow.RunStatusCollectionPending,
			RequestedScopeSet: "[]",
			CreatedAt:         time.Now().UTC(),
			UpdatedAt:         time.Now().UTC(),
		},
		[]workflow.WorkItem{
			retireUnclaimedAdmissionItem("retire-guard-w1c", "retire-guard-run-3", instance, "retire-guard-scope-1"),
			retireUnclaimedAdmissionItem("retire-guard-w2c", "retire-guard-run-3", instance, "retire-guard-scope-2"),
		})
	if err != nil {
		t.Fatalf("released admission error = %v, want nil", err)
	}
	if released.EligibleTargets != 2 || released.InsertedWorkItems != 2 {
		t.Fatalf("released admission = %+v, want 2 eligible and 2 inserted (guard must release)", released)
	}
}

// TestRetireUnclaimedLiveReadsUnaffected proves the status-registry and
// failure-reporting reads do not make a serving scope read as failed: the
// registry snapshots are byte-identical before and after (sbom_attestation
// sits outside the registry kind filter), the instance-bound terminal
// failure status read skips the deactivated instance and reports nothing
// for the active one, and the per-instance failure list is empty for both
// (a deactivated instance is not a serving scope, and a read is not a
// decision). The retired rows stay terminal truth in the table for
// operator forensics (eshu-hq/eshu#10532).
func TestRetireUnclaimedLiveReadsUnaffected(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	asOf := time.Now().UTC()

	retired := retireUnclaimedLiveInstance(t) + "-gone"
	active := retireUnclaimedLiveInstance(t) + "-live"
	seedRetireUnclaimedInstance(t, ctx, database, retired, false)
	seedRetireUnclaimedInstance(t, ctx, database, active, true)
	seedRetireUnclaimedRun(t, ctx, database, "retire-reads-run", "collection_active")
	seedRetireUnclaimedItem(t, ctx, database, "retire-reads-w1", "retire-reads-run", retired, "retire-reads-scope-1")
	seedRetireUnclaimedItem(t, ctx, database, "retire-reads-w2", "retire-reads-run", retired, "retire-reads-scope-2")

	seedRetireUnclaimedRun(t, ctx, database, "retire-reads-live-run", "collection_active")
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_work_items (
    work_item_id, run_id, collector_kind, collector_instance_id, source_system,
    scope_id, acceptance_unit_id, source_run_id, generation_id, status,
    attempt_count, current_claim_id, current_owner_id, created_at, updated_at
) VALUES ('retire-reads-live-completed', 'retire-reads-live-run', $1, $2, $1,
    'retire-reads-scope-9', 'unit-9', 'gen-9', 'gen-9', 'completed', 1, NULL, NULL, $3, $3)`,
		string(retireUnclaimedLiveKind), active, asOf,
	); err != nil {
		t.Fatalf("seed active serving row: %v", err)
	}

	before, err := readRegistryCollectorSnapshots(ctx, SQLDB{DB: database}, asOf)
	if err != nil {
		t.Fatalf("read registry snapshots before: %v", err)
	}
	if _, err := store.RetireUnclaimedWorkItems(ctx, retireUnclaimedLiveKind, retired, "reads"); err != nil {
		t.Fatalf("RetireUnclaimedWorkItems() error = %v, want nil", err)
	}
	after, err := readRegistryCollectorSnapshots(ctx, SQLDB{DB: database}, asOf)
	if err != nil {
		t.Fatalf("read registry snapshots after: %v", err)
	}
	if fmt.Sprintf("%+v", before) != fmt.Sprintf("%+v", after) {
		t.Fatalf("registry snapshots changed by the retire:\nbefore=%+v\nafter=%+v", before, after)
	}

	instances := []collector.InstanceSummary{
		{
			InstanceID:     active,
			CollectorKind:  string(retireUnclaimedLiveKind),
			Enabled:        true,
			LastObservedAt: asOf,
		},
		{
			InstanceID:     retired,
			CollectorKind:  string(retireUnclaimedLiveKind),
			Enabled:        false,
			DeactivatedAt:  asOf,
			LastObservedAt: asOf,
		},
	}
	failures, err := readWorkflowCollectorTerminalFailures(ctx, SQLDB{DB: database}, instances, asOf)
	if err != nil {
		t.Fatalf("read terminal failures: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("terminal failures = %+v, want empty (deactivated instance skipped, active instance healthy)", failures)
	}

	ackStore := NewTerminalFailureAckStore(SQLDB{DB: database}, SQLDB{DB: database})
	for _, id := range []string{active, retired} {
		page, err := ackStore.ListTerminalFailures(ctx, TerminalFailureListFilter{
			CollectorKind: string(retireUnclaimedLiveKind),
			InstanceID:    id,
		})
		if err != nil {
			t.Fatalf("ListTerminalFailures(%s) error = %v, want nil", id, err)
		}
		if len(page.Items) != 0 {
			t.Fatalf("ListTerminalFailures(%s) = %+v, want empty", id, page.Items)
		}
	}
}
