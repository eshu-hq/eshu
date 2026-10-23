// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package items

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8829 disposable PostgreSQL 18 concurrency proofs that workflow_work_items retention batches (FOR UPDATE SKIP LOCKED) skip held rows without waiting and never block claim, heartbeat, completion or terminal failure through WorkflowControlStore, including while an uncommitted batch holds its locks; each proof creates and drops its own database from the bootstrap schema; about 1 to 3 s per proof on PostgreSQL 18.6 locally; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	concurrencyOldScopes      = 30
	concurrencyOldPerScope    = 100
	concurrencyLiveItems      = 300
	concurrencyWorkers        = 4
	concurrencyLiveInstanceID = "inst-live"
	// concurrencyOpBudget is the slowest single claim, heartbeat or
	// completion the proof accepts. Lifecycle statements never wait on a
	// retention lock, so a breach means retention contended with them.
	concurrencyOpBudget = 2 * time.Second
)

// seedOldHistory bulk-inserts concurrencyOldScopes scopes of terminal history
// older than the cutoff, each with a finished claim row, in one closed run.
func seedOldHistory(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	seedRun(t, ctx, database, "run-history", "failed")
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_work_items (
    work_item_id, run_id, collector_kind, collector_instance_id, source_system,
    scope_id, acceptance_unit_id, source_run_id, generation_id, status, attempt_count,
    last_failure_class, created_at, updated_at
)
SELECT 'old-' || s || '-' || lpad(c::text, 4, '0'), 'run-history', 'aws', 'inst-1', 'aws',
       'scope-old-' || s, 'scope-old-' || s, 'gen-old-' || s || '-' || c, 'gen-old-' || s || '-' || c,
       CASE WHEN c % 5 = 0 THEN 'failed_terminal' ELSE 'completed' END, 1,
       CASE WHEN c % 5 = 0 THEN 'aws_access_denied' END,
       $1::timestamptz + c * interval '15 minutes', $1::timestamptz + c * interval '15 minutes'
FROM generate_series(1, $2::int) AS s, generate_series(0, $3::int - 1) AS c`,
		liveEpoch, concurrencyOldScopes, concurrencyOldPerScope,
	); err != nil {
		t.Fatalf("seed old history: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_claims (
    claim_id, work_item_id, fencing_token, owner_id, status, claimed_at, heartbeat_at,
    lease_expires_at, created_at, updated_at
)
SELECT 'claim-' || work_item_id, work_item_id, 1, 'owner-1', status, updated_at, updated_at,
       updated_at + interval '5 minutes', updated_at, updated_at
FROM workflow_work_items WHERE run_id = 'run-history'`); err != nil {
		t.Fatalf("seed old claims: %v", err)
	}
}

// seedLiveWork inserts claimable pending items in an open run, timestamped
// now so retention never considers them.
func seedLiveWork(t *testing.T, ctx context.Context, database *sql.DB, count int) {
	t.Helper()
	seedRun(t, ctx, database, "run-live", "collection_active")
	now := time.Now().UTC().Add(-time.Second)
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_work_items (
    work_item_id, run_id, collector_kind, collector_instance_id, source_system,
    scope_id, acceptance_unit_id, source_run_id, generation_id, status,
    visible_at, created_at, updated_at
)
SELECT 'live-' || lpad(n::text, 4, '0'), 'run-live', 'aws', $1, 'aws',
       'scope-live-' || n, 'scope-live-' || n, 'gen-live-' || n, 'gen-live-' || n, 'pending',
       $2, $2, $2
FROM generate_series(1, $3::int) AS n`, concurrencyLiveInstanceID, now, count); err != nil {
		t.Fatalf("seed live work: %v", err)
	}
}

// lifecycleWorker claims, heartbeats and completes (or terminally fails) live
// work until none is left, recording errors and the slowest operation. A
// non-nil beforeClaim runs, untimed, before every claim; the overlap proof
// uses it to pace the worker against the retention loop.
func lifecycleWorker(
	ctx context.Context,
	store *postgres.WorkflowControlStore,
	worker int,
	slowest *atomic.Int64,
	processed *atomic.Int64,
	beforeClaim func() error,
) error {
	timed := func(op func(context.Context) error) error {
		opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		started := time.Now()
		err := op(opCtx)
		recordMax(slowest, time.Since(started).Nanoseconds())
		return err
	}
	for attempt := 0; ; attempt++ {
		if beforeClaim != nil {
			if err := beforeClaim(); err != nil {
				return fmt.Errorf("worker %d: %w", worker, err)
			}
		}
		claimID := fmt.Sprintf("claim-w%d-%d", worker, attempt)
		owner := fmt.Sprintf("owner-w%d", worker)
		var (
			item  workflow.WorkItem
			claim workflow.Claim
			found bool
		)
		if err := timed(func(opCtx context.Context) error {
			var err error
			item, claim, found, err = store.ClaimNextEligible(opCtx, workflow.ClaimSelector{
				CollectorKind:       scope.CollectorAWS,
				CollectorInstanceID: concurrencyLiveInstanceID,
				OwnerID:             owner,
				ClaimID:             claimID,
			}, time.Now().UTC(), 30*time.Second)
			return err
		}); err != nil {
			return fmt.Errorf("worker %d claim: %w", worker, err)
		}
		if !found {
			return nil
		}
		mutation := workflow.ClaimMutation{
			WorkItemID:    item.WorkItemID,
			ClaimID:       claim.ClaimID,
			FencingToken:  claim.FencingToken,
			OwnerID:       owner,
			LeaseDuration: 30 * time.Second,
		}
		mutation.ObservedAt = time.Now().UTC()
		if err := timed(func(opCtx context.Context) error { return store.HeartbeatClaim(opCtx, mutation) }); err != nil {
			return fmt.Errorf("worker %d heartbeat %s: %w", worker, item.WorkItemID, err)
		}
		mutation.ObservedAt = time.Now().UTC()
		if attempt%7 == 0 {
			mutation.FailureClass = "proof_terminal"
			mutation.FailureMessage = "terminal failure seeded by the retention concurrency proof"
			mutation.FailureTerminal = true
			if err := timed(func(opCtx context.Context) error { return store.FailClaimTerminal(opCtx, mutation) }); err != nil {
				return fmt.Errorf("worker %d fail terminal %s: %w", worker, item.WorkItemID, err)
			}
		} else if err := timed(func(opCtx context.Context) error { return store.CompleteClaim(opCtx, mutation) }); err != nil {
			return fmt.Errorf("worker %d complete %s: %w", worker, item.WorkItemID, err)
		}
		processed.Add(1)
	}
}

// TestPruneWorkItemsLiveDoesNotContendWithClaimLifecycle runs retention
// batches in a loop while concurrent workers claim, heartbeat and complete
// live work. Every lifecycle operation must succeed within
// concurrencyOpBudget, retention must not fail, and both sides must finish
// their work. An overlapGate makes the two sides interleave by construction:
// a CI run once had the workers drain all live work before the first
// retention pass returned, so the proof never overlapped.
func TestPruneWorkItemsLiveDoesNotContendWithClaimLifecycle(t *testing.T) {
	database, ctx := isolatedLiveDB(t)
	seedOldHistory(t, ctx, database)
	seedLiveWork(t, ctx, database, concurrencyLiveItems)
	store := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: database})
	sweeper := NewSweeper(postgres.SQLDB{DB: database})

	var (
		slowest, processed  atomic.Int64
		retentionDeleted    atomic.Int64
		slowestRetention    atomic.Int64
		retentionErr        error
		retentionLoopFinish = make(chan struct{})
	)
	gate := newOverlapGate(&processed)
	go func() {
		defer close(retentionLoopFinish)
		defer gate.retentionStopped()
		// One batch per call, so every committed batch is counted as it
		// commits, not when a multi-batch pass returns.
		policy := testPolicy(50, 1)
		for {
			if err := gate.beforeRetentionBatch(); err != nil {
				retentionErr = err
				return
			}
			if gate.workersFinished() {
				return
			}
			result, err := sweeper.PruneWorkItems(ctx, liveAsOf, policy)
			if err != nil {
				retentionErr = err
				return
			}
			retentionDeleted.Add(int64(result.Deleted()))
			gate.retentionCommitted(result)
			for _, batch := range result.BatchDurations {
				recordMax(&slowestRetention, int64(batch))
			}
		}
	}()

	var wg sync.WaitGroup
	errs := make(chan error, concurrencyWorkers)
	for worker := range concurrencyWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- lifecycleWorker(ctx, store, worker, &slowest, &processed, gate.beforeClaim)
		}()
	}
	wg.Wait()
	batchesDuringWork, deletedDuringWork := gate.finishWorkers()
	<-retentionLoopFinish
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("lifecycle worker failed while retention ran: %v", err)
		}
	}
	if retentionErr != nil {
		t.Fatalf("retention failed while lifecycle work ran: %v", retentionErr)
	}
	if want := int64(overlapMinBatches()); batchesDuringWork < want || deletedDuringWork == 0 {
		t.Fatalf("retention committed %d batches deleting %d rows while workers ran, want at least %d batches deleting rows; the proof did not overlap",
			batchesDuringWork, deletedDuringWork, want)
	}
	if got := processed.Load(); got != concurrencyLiveItems {
		t.Fatalf("lifecycle processed %d items, want %d", got, concurrencyLiveItems)
	}
	if got := time.Duration(slowest.Load()); got > concurrencyOpBudget {
		t.Fatalf("slowest lifecycle operation = %s, want <= %s", got, concurrencyOpBudget)
	}

	finalDeleted := drainToWrap(t, ctx, sweeper, 1000)
	var oldRemaining, liveOpen int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workflow_work_items WHERE run_id = 'run-history'`).Scan(&oldRemaining); err != nil {
		t.Fatalf("count old history: %v", err)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workflow_work_items WHERE run_id = 'run-live' AND status NOT IN ('completed', 'failed_terminal')`,
	).Scan(&liveOpen); err != nil {
		t.Fatalf("count open live work: %v", err)
	}
	// Each old scope keeps its newest row, which is also its newest completed
	// row, and its newest terminal failure.
	if want := concurrencyOldScopes * 2; oldRemaining != want {
		t.Fatalf("old history remaining = %d, want %d", oldRemaining, want)
	}
	if liveOpen != 0 {
		t.Fatalf("live work left open = %d, want 0", liveOpen)
	}
	if total := retentionDeleted.Load() + int64(finalDeleted); total != concurrencyOldScopes*(concurrencyOldPerScope-2) {
		t.Fatalf("retention deleted %d rows, want %d", total, concurrencyOldScopes*(concurrencyOldPerScope-2))
	}
	if orphans := orphanClaims(t, ctx, database); orphans != 0 {
		t.Fatalf("orphan claims = %d, want 0", orphans)
	}
	t.Logf("retention batches during work=%d deleting %d rows, slowest retention batch=%s slowest lifecycle op=%s",
		batchesDuringWork, deletedDuringWork, time.Duration(slowestRetention.Load()), time.Duration(slowest.Load()))
}

// TestPruneWorkItemsLiveSkipsLockedRowsWithoutWaiting proves a retention batch
// skips a row another transaction holds instead of waiting on it.
func TestPruneWorkItemsLiveSkipsLockedRowsWithoutWaiting(t *testing.T) {
	database, ctx := isolatedLiveDB(t)
	seedOldHistory(t, ctx, database)
	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	const lockedID = "old-1-0000"
	if _, err := holder.ExecContext(ctx,
		`SELECT 1 FROM workflow_work_items WHERE work_item_id = $1 FOR UPDATE`, lockedID); err != nil {
		t.Fatalf("hold row lock: %v", err)
	}

	// The holder keeps its lock for the whole pass, so a batch that waited on
	// the row instead of skipping it would end on the policy lock timeout
	// with a 55P03 error. A nil error is the proof; no wall-clock bound is
	// needed.
	sweeper := NewSweeper(postgres.SQLDB{DB: database})
	result, err := sweeper.PruneWorkItems(ctx, liveAsOf, testPolicy(1000, 1))
	if err != nil {
		t.Fatalf("PruneWorkItems() with a held row lock error = %v, want nil (it must skip, not wait)", err)
	}
	if result.Deleted() == 0 {
		t.Fatal("pass deleted nothing; want every eligible row except the locked one")
	}
	var present int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workflow_work_items WHERE work_item_id = $1`, lockedID).Scan(&present); err != nil {
		t.Fatalf("read locked row: %v", err)
	}
	if present != 1 {
		t.Fatal("locked row was deleted under another transaction's lock")
	}

	if err := holder.Rollback(); err != nil {
		t.Fatalf("release holder: %v", err)
	}
	// Finish the walk the bounded pass started, then walk the table once more
	// from the head: the skipped window is revisited on that next walk.
	drainToWrap(t, ctx, sweeper, 1000)
	drainToWrap(t, ctx, sweeper, 1000)
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workflow_work_items WHERE work_item_id = $1`, lockedID).Scan(&present); err != nil {
		t.Fatalf("read released row: %v", err)
	}
	if present != 0 {
		t.Fatal("released row survived the next walk; skipped rows must be retried")
	}
}

// TestPruneWorkItemsLiveOpenBatchDoesNotBlockLifecycle holds one retention
// batch transaction open, with its deletes and row locks uncommitted, and
// proves claim, heartbeat and completion still finish within budget.
func TestPruneWorkItemsLiveOpenBatchDoesNotBlockLifecycle(t *testing.T) {
	database, ctx := isolatedLiveDB(t)
	seedOldHistory(t, ctx, database)
	seedLiveWork(t, ctx, database, 5)
	store := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: database})

	retentionTx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin retention tx: %v", err)
	}
	defer func() { _ = retentionTx.Rollback() }()
	policy := workflow.DefaultWorkItemRetentionPolicy()
	if _, err := retentionTx.ExecContext(ctx, setBatchTimeoutsQuery,
		lockTimeoutSetting(policy.LockTimeout), statementTimeoutSetting); err != nil {
		t.Fatalf("set retention timeouts: %v", err)
	}
	var (
		next                  sql.NullString
		scanned, done, failed int64
	)
	if err := retentionTx.QueryRowContext(ctx, pruneBatchQuery, "", 1000, policy.Cutoff(liveAsOf)).
		Scan(&next, &scanned, &done, &failed); err != nil {
		t.Fatalf("run retention batch: %v", err)
	}
	if done+failed == 0 {
		t.Fatal("open retention batch deleted nothing; the proof holds no locks")
	}

	var slowest, processed atomic.Int64
	if err := lifecycleWorker(ctx, store, 0, &slowest, &processed, nil); err != nil {
		t.Fatalf("lifecycle under an open retention batch: %v", err)
	}
	if processed.Load() != 5 {
		t.Fatalf("lifecycle processed %d items, want 5", processed.Load())
	}
	if got := time.Duration(slowest.Load()); got > concurrencyOpBudget {
		t.Fatalf("slowest lifecycle operation under an open retention batch = %s, want <= %s", got, concurrencyOpBudget)
	}
	if err := retentionTx.Commit(); err != nil {
		t.Fatalf("commit retention batch: %v", err)
	}
}
