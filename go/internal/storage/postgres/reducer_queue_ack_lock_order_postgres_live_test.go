// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/scope/completion"
)

// A held first key must stop every writer before it owns any later key. The
// reverse heap order makes this an ordering assertion, not a probabilistic race.
func TestReducerContentionGateAckFanoutLockOrderLive(t *testing.T) {
	for _, variant := range []struct {
		name   string
		domain reducer.Domain
		fanout bool
		stale  bool
		dirty  bool
	}{
		{"generic", reducer.DomainSupplyChainImpact, false, false, false},
		{"identity", reducer.DomainContainerImageIdentity, false, false, false},
		{"cicd", reducer.DomainCICDRunCorrelation, false, false, false},
		{"fanout", reducer.DomainSupplyChainImpact, true, false, false},
		{"generic_stale_owner", reducer.DomainSupplyChainImpact, false, true, false},
		{"identity_stale_epoch", reducer.DomainContainerImageIdentity, false, true, false},
		{"cicd_stale_owner", reducer.DomainCICDRunCorrelation, false, true, false},
		{"fanout_stale_status", reducer.DomainSupplyChainImpact, true, true, false},
		{"fanout_already_dirty", reducer.DomainSupplyChainImpact, true, false, true},
	} {
		t.Run(variant.name, func(t *testing.T) {
			db := openReducerAckFanoutProofDB(t)
			db.SetMaxOpenConns(6)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			now := time.Now().UTC().Truncate(time.Microsecond)
			const scope, generation = "repository:6488-order", "generation:6488-order"
			seedContainerImageIdentityAckScope(t, ctx, db, scope)
			seedContainerImageIdentityAckGeneration(t, ctx, db, scope, generation)
			if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id=$2 WHERE scope_id=$1`, scope, generation); err != nil {
				t.Fatal(err)
			}
			intents := []reducer.Intent{}
			for _, id := range []string{"order-z", "order-a"} {
				insertCrossScopeCompletionBaseConsumer(t, ctx, db, id, scope, generation, variant.domain, now)
				intents = append(intents, reducer.Intent{IntentID: id, Domain: variant.domain, ClaimEpoch: 1, ClaimedAt: &now})
			}
			if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='running', lease_owner='order-owner', claim_until=$1, last_attempt_at=$2, container_image_identity_claim_epoch=1`, now.Add(time.Hour), now); err != nil {
				t.Fatal(err)
			}
			if variant.dirty {
				if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET cross_scope_replay_required=TRUE`); err != nil {
					t.Fatal(err)
				}
			}
			worker := ackFanoutProbeConnection(t, ctx, db, "order-worker")
			if _, err := worker.ExecContext(ctx, `SET enable_indexscan=off; SET enable_bitmapscan=off`); err != nil {
				t.Fatal(err)
			}
			var pid int
			if err := worker.Conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			blocker, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback()
			if _, err := blocker.ExecContext(ctx, `SELECT work_item_id FROM fact_work_items WHERE work_item_id='order-a' FOR UPDATE`); err != nil {
				t.Fatal(err)
			}
			var lease reducer.CrossScopeCompletionLease
			if variant.fanout {
				event := insertCrossScopeCompletionEvent(t, ctx, db, reducer.DomainCICDRunCorrelation, "claimed", "order-fanout", now.Add(time.Hour), 1, now)
				lease = reducer.CrossScopeCompletionLease{EventID: event, ProducerDomain: reducer.DomainCICDRunCorrelation, LeaseOwner: "order-fanout", ClaimEpoch: 1}
			}
			done := make(chan error, 1)
			go func() {
				if variant.fanout {
					store := completionstore.NewCrossScopeCompletionStore(worker)
					store.Now = func() time.Time { return now }
					_, err := store.Fanout(ctx, lease, 1)
					done <- err
					return
				}
				queue := ReducerQueue{database: worker, LeaseOwner: "order-owner", LeaseDuration: time.Minute, Now: func() time.Time { return now }}
				done <- queue.AckBatch(ctx, intents, nil)
			}()
			var blockerPID int
			if err := blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
				t.Fatal(err)
			}
			waitForReducerRowLockWaiter(t, ctx, db, pid, blockerPID)
			probe, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, lockErr := probe.ExecContext(ctx, `SELECT work_item_id FROM fact_work_items WHERE work_item_id='order-z' FOR UPDATE NOWAIT`)
			_ = probe.Rollback()
			if lockErr != nil {
				t.Errorf("writer locked later key before blocked first key: %v", lockErr)
			}
			if variant.stale {
				change := "lease_owner='new-owner'"
				if variant.domain == reducer.DomainContainerImageIdentity {
					change = "container_image_identity_claim_epoch=2"
				}
				if variant.fanout {
					change = "status='pending',lease_owner=NULL,claim_until=NULL"
				}
				if _, err := blocker.ExecContext(ctx, "UPDATE fact_work_items SET "+change+" WHERE work_item_id='order-a'"); err != nil {
					t.Fatal(err)
				}
			}
			if err := blocker.Commit(); err != nil {
				t.Fatal(err)
			}
			err = <-done
			if variant.stale && !variant.fanout {
				if !errors.Is(err, ErrReducerClaimRejected) {
					t.Fatalf("stale reducer claim error = %v, want %v", err, ErrReducerClaimRejected)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			for _, intent := range intents {
				status, replay := "succeeded", false
				if variant.fanout {
					status, replay = "running", true
				}
				if variant.stale && intent.IntentID == "order-a" {
					status, replay = "running", false
					if variant.fanout {
						status = "pending"
					}
				}
				assertCrossScopeConsumerState(t, ctx, db, intent.IntentID, status, replay)
			}
		})
	}
}

func waitForReducerRowLockWaiter(t *testing.T, ctx context.Context, db *sql.DB, pid, blockerPID int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		if err := db.QueryRowContext(ctx, `SELECT $2::int = ANY(pg_blocking_pids($1))`, pid, blockerPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backend %d never reached held first row", pid)
}

// Match the contention job's ESHU_POSTGRES_DSN contract while reusing the
// existing isolated-schema bootstrap helper, which takes the test DSN name.
func openReducerAckFanoutProofDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN for the ACK/fanout contention proof")
	}
	t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	return openContainerImageIdentityAckCapabilityProofDB(t)
}

// A real 40P01 on the batch ACK must be retried, not end the reducer run
// (#7267). The ACK locks its rows FOR NO KEY UPDATE in work_item_id order; the
// blocker takes the same two rows in reverse, so Postgres's deadlock detector
// aborts the ACK statement. The blocker's raised deadlock_timeout makes the ACK
// backend the one that runs the detector, and therefore the victim, provided
// the blocker's second lock request arrives within the ACK's own
// deadlock_timeout (normally a few tens of milliseconds; a stalled runner can
// miss that window and the run then fails at the blocker's timeout).
func TestReducerContentionGateAckBatchDeadlockRetryLive(t *testing.T) {
	for _, variant := range []struct {
		name    string
		reclaim bool
	}{
		{"retry_acks_each_row_once", false},
		{"retry_after_reclaim_rejects_stale_claim", true},
	} {
		t.Run(variant.name, func(t *testing.T) {
			db := openReducerAckFanoutProofDB(t)
			db.SetMaxOpenConns(8)
			db.SetMaxIdleConns(8)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			const owner, scope, generation = "reducer-7267-deadlock", "repository:7267-deadlock", "generation:7267-deadlock"
			const first, second = "reducer_7267_deadlock_a", "reducer_7267_deadlock_b"
			seedReducerAckReclaimScope(t, ctx, db, scope)
			seedReducerAckReclaimGeneration(t, ctx, db, scope, generation)
			for _, id := range []string{first, second} {
				seedReducerAckReclaimWorkItem(t, ctx, db, id, scope, generation)
			}

			queue := ReducerQueue{database: SQLDB{DB: db}, LeaseOwner: owner, LeaseDuration: time.Minute}
			sink := &deadlockAckRecorder{ReducerQueue: queue}
			executor := &deadlockAckBarrierExecutor{arrived: make(chan reducer.Intent, 2), release: make(chan struct{})}
			service := reducer.Service{
				PollInterval: 10 * time.Millisecond, WorkSource: queue, Executor: executor,
				WorkSink: sink, Workers: 2, BatchClaimSize: 2,
			}
			runCtx, stopRun := context.WithCancel(ctx)
			defer stopRun()
			runDone := make(chan error, 1)
			go func() { runDone <- service.Run(runCtx) }()

			// Hold both executions until the blocker owns the later key, so the
			// ACK meets it as one two-row batch.
			claimed := map[string]reducer.Intent{}
			for len(claimed) < 2 {
				select {
				case intent := <-executor.arrived:
					claimed[intent.IntentID] = intent
				case err := <-runDone:
					t.Fatalf("reducer run ended before both claims executed: %v", err)
				case <-ctx.Done():
					t.Fatalf("claims never reached the executor: %v", ctx.Err())
				}
			}
			blocker, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = blocker.Rollback() }()
			if _, err := blocker.ExecContext(ctx, `SET LOCAL deadlock_timeout = '30s'`); err != nil {
				t.Fatalf("pin blocker deadlock_timeout (needs superuser): %v", err)
			}
			if _, err := blocker.ExecContext(ctx, `SELECT 1 FROM fact_work_items WHERE work_item_id = $1 FOR UPDATE`, second); err != nil {
				t.Fatal(err)
			}
			var blockerPID int
			if err := blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
				t.Fatal(err)
			}
			close(executor.release)
			ackPID := waitForReducerBackendBlockedBy(t, ctx, db, blockerPID)

			// Unordered locker: now wait on the first key, which the ACK holds.
			// This returns only once the detector has aborted the ACK.
			if _, err := blocker.ExecContext(ctx, `SELECT 1 FROM fact_work_items WHERE work_item_id = $1 FOR UPDATE`, first); err != nil {
				t.Fatalf("blocker lost the deadlock (ACK backend %d should have): %v", ackPID, err)
			}
			var reclaimedAt time.Time
			if variant.reclaim {
				// Same-owner lease reclaim while the ACK backs off: only the
				// claim timestamp fences the stale ACK.
				if err := blocker.QueryRowContext(ctx, `
UPDATE fact_work_items
SET last_attempt_at = clock_timestamp(), attempt_count = attempt_count + 1,
    claim_until = clock_timestamp() + INTERVAL '1 hour'
WHERE work_item_id = $1
RETURNING last_attempt_at`, first).Scan(&reclaimedAt); err != nil {
					t.Fatal(err)
				}
			}
			if err := blocker.Commit(); err != nil {
				t.Fatal(err)
			}

			calls := waitForDeadlockAckOutcome(t, ctx, sink, runDone)
			stopRun()
			if err := <-runDone; err != nil {
				t.Fatalf("reducer run = %v, want nil after a retried 40P01 ACK", err)
			}
			var pgErr interface{ SQLState() string }
			if calls[0].size != 2 || !errors.As(calls[0].err, &pgErr) || pgErr.SQLState() != "40P01" {
				t.Fatalf("first ACK = %d items, %v; want a two-row batch aborted with 40P01", calls[0].size, calls[0].err)
			}
			if len(calls) != 2 {
				t.Fatalf("AckBatch calls = %+v, want the deadlock then exactly one retry", calls)
			}
			stale := []reducer.Intent{claimed[first], claimed[second]}
			if variant.reclaim {
				if !errors.Is(calls[1].err, ErrReducerClaimRejected) {
					t.Fatalf("retried ACK after reclaim = %v, want %v", calls[1].err, ErrReducerClaimRejected)
				}
				assertDeadlockAckRow(t, ctx, db, second, "succeeded", 1)
				before := assertDeadlockAckRow(t, ctx, db, first, "running", 2)
				if !before.lastAttemptAt.Equal(reclaimedAt) || before.leaseOwner != owner {
					t.Fatalf("reclaimed row = %+v, want the new claim at %v owned by %s", before, reclaimedAt, owner)
				}
				stale = stale[:1]
				if err := queue.AckBatch(ctx, stale, nil); !errors.Is(err, ErrReducerClaimRejected) {
					t.Fatalf("stale ACK replay = %v, want %v", err, ErrReducerClaimRejected)
				}
				if after := assertDeadlockAckRow(t, ctx, db, first, "running", 2); !after.same(before) {
					t.Fatalf("stale ACK replay changed the reclaimed row: %+v -> %+v", before, after)
				}
				return
			}
			if calls[1].err != nil {
				t.Fatalf("retried ACK = %v, want success", calls[1].err)
			}
			firstRow := assertDeadlockAckRow(t, ctx, db, first, "succeeded", 1)
			secondRow := assertDeadlockAckRow(t, ctx, db, second, "succeeded", 1)
			if err := queue.AckBatch(ctx, stale, nil); !errors.Is(err, ErrReducerClaimRejected) {
				t.Fatalf("second ACK of completed rows = %v, want %v", err, ErrReducerClaimRejected)
			}
			if got := assertDeadlockAckRow(t, ctx, db, first, "succeeded", 1); !got.same(firstRow) {
				t.Fatalf("second ACK recompleted %s: %+v -> %+v", first, firstRow, got)
			}
			if got := assertDeadlockAckRow(t, ctx, db, second, "succeeded", 1); !got.same(secondRow) {
				t.Fatalf("second ACK recompleted %s: %+v -> %+v", second, secondRow, got)
			}
		})
	}
}

// deadlockAckRecorder is the real ReducerQueue sink with every AckBatch
// outcome recorded, so the proof can show the 40P01 actually happened.
type deadlockAckRecorder struct {
	ReducerQueue
	mu    sync.Mutex
	calls []deadlockAckCall
}

type deadlockAckCall struct {
	size int
	err  error
}

func (r *deadlockAckRecorder) AckBatch(ctx context.Context, intents []reducer.Intent, results []reducer.Result) error {
	err := r.ReducerQueue.AckBatch(ctx, intents, results)
	r.mu.Lock()
	r.calls = append(r.calls, deadlockAckCall{size: len(intents), err: err})
	r.mu.Unlock()
	return err
}

func (r *deadlockAckRecorder) snapshot() []deadlockAckCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]deadlockAckCall(nil), r.calls...)
}

// deadlockAckBarrierExecutor reports each claim, then succeeds on release.
type deadlockAckBarrierExecutor struct {
	arrived chan reducer.Intent
	release chan struct{}
}

func (e *deadlockAckBarrierExecutor) Execute(ctx context.Context, intent reducer.Intent) (reducer.Result, error) {
	e.arrived <- intent
	select {
	case <-e.release:
		return reducer.Result{}, nil
	case <-ctx.Done():
		return reducer.Result{}, ctx.Err()
	}
}

// waitForDeadlockAckOutcome waits for an ACK that is not a transient failure,
// failing fast if the run ends first (the pre-#7267 behavior).
func waitForDeadlockAckOutcome(t *testing.T, ctx context.Context, sink *deadlockAckRecorder, runDone <-chan error) []deadlockAckCall {
	t.Helper()
	var pgErr interface{ SQLState() string }
	for {
		if calls := sink.snapshot(); len(calls) >= 2 && !errors.As(calls[len(calls)-1].err, &pgErr) {
			return calls
		}
		select {
		case err := <-runDone:
			t.Fatalf("reducer run ended on the deadlocked ACK: %v (ACKs: %+v)", err, sink.snapshot())
		case <-ctx.Done():
			t.Fatalf("no settled ACK outcome: %v (ACKs: %+v)", ctx.Err(), sink.snapshot())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func waitForReducerBackendBlockedBy(t *testing.T, ctx context.Context, db *sql.DB, blockerPID int) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var pid int
		err := db.QueryRowContext(ctx, `SELECT pid FROM pg_stat_activity WHERE $1::int = ANY(pg_blocking_pids(pid)) LIMIT 1`, blockerPID).Scan(&pid)
		if err == nil {
			return pid
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no ACK backend ever waited on blocker %d", blockerPID)
	return 0
}

type deadlockAckRowState struct {
	updatedAt     time.Time
	lastAttemptAt time.Time
	leaseOwner    string
}

func (r deadlockAckRowState) same(o deadlockAckRowState) bool {
	return r.updatedAt.Equal(o.updatedAt) && r.lastAttemptAt.Equal(o.lastAttemptAt) && r.leaseOwner == o.leaseOwner
}

func assertDeadlockAckRow(t *testing.T, ctx context.Context, db *sql.DB, id, wantStatus string, wantAttempts int) deadlockAckRowState {
	t.Helper()
	var status string
	var attempts int
	var owner sql.NullString
	var row deadlockAckRowState
	if err := db.QueryRowContext(ctx, `
SELECT status, attempt_count, lease_owner, updated_at, last_attempt_at
FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&status, &attempts, &owner, &row.updatedAt, &row.lastAttemptAt); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	row.leaseOwner = owner.String
	if wantStatus == "running" && status == "claimed" {
		status = "running"
	}
	if status != wantStatus || attempts != wantAttempts {
		t.Fatalf("%s = status:%s attempts:%d, want %s/%d", id, status, attempts, wantStatus, wantAttempts)
	}
	return row
}
