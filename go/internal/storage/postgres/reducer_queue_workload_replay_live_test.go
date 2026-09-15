// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestWorkloadReplayDuringClaimReturnsAckToPending proves a cross-repo update
// cannot be lost when workload materialization is already executing. Replay
// dirties the exact in-flight row without stealing its lease; the existing
// database ACK trigger then returns it to pending for one fresh pass.
func TestWorkloadReplayDuringClaimReturnsAckToPending(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, generationID, _ := refinalizeResetScope(t, ctx, db, suffix)
	entityKey := "repo:workload-replay-" + suffix
	intent := projector.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainWorkloadMaterialization,
		EntityKey:    entityKey,
		Reason:       "workload replay claim proof",
		SourceSystem: "reducer",
	}

	queue := NewReducerQueue(SQLDB{DB: db}, "workload-replay-worker", time.Minute)
	queue.ClaimDomain = reducer.DomainWorkloadMaterialization
	if _, err := queue.Enqueue(ctx, []projector.ReducerIntent{intent}); err != nil {
		t.Fatalf("enqueue workload materialization: %v", err)
	}
	claimed, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("claim workload materialization: %v", err)
	}
	if !ok {
		t.Fatal("claim workload materialization returned no work")
	}

	replayed, err := queue.ReplayWorkloadMaterialization(ctx, scopeID, generationID, entityKey)
	if err != nil {
		t.Fatalf("schedule replay during claim: %v", err)
	}
	if !replayed {
		t.Fatal("schedule replay during claim returned false")
	}
	var replayRequired bool
	if err := db.QueryRowContext(ctx,
		`SELECT cross_scope_replay_required FROM fact_work_items WHERE work_item_id = $1`,
		claimed.IntentID,
	).Scan(&replayRequired); err != nil {
		t.Fatalf("read replay-required bit: %v", err)
	}
	if !replayRequired {
		t.Fatal("claimed workload row replay-required = false, want true")
	}

	if err := queue.Ack(ctx, claimed, reducer.Result{}); err != nil {
		t.Fatalf("ack dirtied workload materialization: %v", err)
	}
	state := readClaimTokenWorkState(t, ctx, db, claimed.IntentID)
	if state.status != "pending" {
		t.Fatalf("status after dirtied ACK = %q, want pending", state.status)
	}
	if state.leaseOwner != "" || state.claimUntil.Valid {
		t.Fatalf("lease after dirtied ACK = owner:%q until:%v, want cleared", state.leaseOwner, state.claimUntil)
	}

	second, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("claim replayed workload materialization: %v", err)
	}
	if !ok || second.IntentID != claimed.IntentID {
		t.Fatalf("replayed claim = (%q, %v), want (%q, true)", second.IntentID, ok, claimed.IntentID)
	}
}

func TestWorkloadReplayAndBatchAckContentionConvergesToPending(t *testing.T) {
	t.Run("ack_first_replay_reopens", testWorkloadReplayAfterUncommittedBatchAck)
	t.Run("replay_first_batch_ack_trigger_reopens", testWorkloadBatchAckAfterUncommittedReplay)
}

func testWorkloadReplayAfterUncommittedBatchAck(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	queue, claimed, scopeID, generationID, entityKey := seedClaimedWorkloadReplay(t, ctx, db, "ack-first")
	ackConn := ackFanoutProbeConnection(t, ctx, db, "workload-replay-ack-first")
	replayConn := ackFanoutProbeConnection(t, ctx, db, "workload-replay-waiter")
	ackPID := workloadReplayBackendPID(t, ctx, ackConn)
	replayPID := workloadReplayBackendPID(t, ctx, replayConn)

	ackTx, err := ackConn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin batch ACK transaction: %v", err)
	}
	t.Cleanup(func() { _ = ackTx.Rollback() })
	ackQueue := queue
	ackQueue.db = SQLTx{Tx: ackTx}
	if err := ackQueue.AckBatch(ctx, []reducer.Intent{claimed}, nil); err != nil {
		t.Fatalf("batch ACK before replay: %v", err)
	}
	var status string
	if err := ackTx.QueryRowContext(ctx,
		`SELECT status FROM fact_work_items WHERE work_item_id = $1`,
		claimed.IntentID,
	).Scan(&status); err != nil {
		t.Fatalf("read uncommitted ACK status: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("uncommitted ACK status = %q, want succeeded", status)
	}

	replayQueue := queue
	replayQueue.db = replayConn
	replayDone := make(chan workloadReplayOutcome, 1)
	go func() {
		replayed, replayErr := replayQueue.ReplayWorkloadMaterialization(
			ctx, scopeID, generationID, entityKey,
		)
		replayDone <- workloadReplayOutcome{replayed: replayed, err: replayErr}
	}()
	waitForReducerRowLockWaiter(t, ctx, db, replayPID, ackPID)
	if err := ackTx.Commit(); err != nil {
		t.Fatalf("commit batch ACK transaction: %v", err)
	}
	outcome := <-replayDone
	if outcome.err != nil || !outcome.replayed {
		t.Fatalf("replay after committed batch ACK = (%v, %v), want (true, nil)", outcome.replayed, outcome.err)
	}
	assertWorkloadReplayPendingAndReclaimable(t, ctx, db, queue, claimed.IntentID)
}

func testWorkloadBatchAckAfterUncommittedReplay(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	queue, claimed, scopeID, generationID, entityKey := seedClaimedWorkloadReplay(t, ctx, db, "replay-first")
	replayConn := ackFanoutProbeConnection(t, ctx, db, "workload-replay-first")
	ackConn := ackFanoutProbeConnection(t, ctx, db, "workload-ack-waiter")
	replayPID := workloadReplayBackendPID(t, ctx, replayConn)
	ackPID := workloadReplayBackendPID(t, ctx, ackConn)
	preReplay := readClaimTokenWorkState(t, ctx, db, claimed.IntentID)

	replayTx, err := replayConn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin replay transaction: %v", err)
	}
	t.Cleanup(func() { _ = replayTx.Rollback() })
	replayQueue := queue
	replayQueue.db = SQLTx{Tx: replayTx}
	replayed, err := replayQueue.ReplayWorkloadMaterialization(ctx, scopeID, generationID, entityKey)
	if err != nil || !replayed {
		t.Fatalf("uncommitted replay = (%v, %v), want (true, nil)", replayed, err)
	}
	assertUncommittedWorkloadReplayKeepsClaim(
		t, ctx, replayTx, claimed, queue.LeaseOwner, preReplay.claimUntil,
	)

	ackQueue := queue
	ackQueue.db = ackConn
	ackDone := make(chan error, 1)
	go func() { ackDone <- ackQueue.AckBatch(ctx, []reducer.Intent{claimed}, nil) }()
	waitForReducerRowLockWaiter(t, ctx, db, ackPID, replayPID)
	if err := replayTx.Commit(); err != nil {
		t.Fatalf("commit replay transaction: %v", err)
	}
	if err := <-ackDone; err != nil {
		t.Fatalf("batch ACK after committed replay: %v", err)
	}
	assertWorkloadReplayPendingAndReclaimable(t, ctx, db, queue, claimed.IntentID)
}

type workloadReplayOutcome struct {
	replayed bool
	err      error
}

func seedClaimedWorkloadReplay(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	suffixLabel string,
) (ReducerQueue, reducer.Intent, string, string, string) {
	t.Helper()
	suffix := testSuffix(t) + "-" + suffixLabel
	scopeID, generationID, _ := refinalizeResetScope(t, ctx, db, suffix)
	entityKey := "repo:workload-replay-" + suffix
	intent := projector.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainWorkloadMaterialization,
		EntityKey:    entityKey,
		Reason:       "workload replay contention proof",
		SourceSystem: "reducer",
	}
	queue := NewReducerQueue(SQLDB{DB: db}, "workload-replay-worker-"+suffix, time.Minute)
	queue.ClaimDomain = reducer.DomainWorkloadMaterialization
	if _, err := queue.Enqueue(ctx, []projector.ReducerIntent{intent}); err != nil {
		t.Fatalf("enqueue workload materialization: %v", err)
	}
	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim workload materialization = (%v, %v), want work", ok, err)
	}
	return queue, claimed, scopeID, generationID, entityKey
}

func workloadReplayBackendPID(t *testing.T, ctx context.Context, conn ackFanoutProbeConn) int {
	t.Helper()
	var pid int
	if err := conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("read Postgres backend PID: %v", err)
	}
	return pid
}

func assertUncommittedWorkloadReplayKeepsClaim(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
	claimed reducer.Intent,
	leaseOwner string,
	wantClaimUntil sql.NullTime,
) {
	t.Helper()
	var status, owner string
	var claimUntil sql.NullTime
	var claimedAt time.Time
	var replayRequired bool
	err := tx.QueryRowContext(ctx, `
SELECT status, COALESCE(lease_owner, ''), claim_until, last_attempt_at,
       cross_scope_replay_required
FROM fact_work_items
WHERE work_item_id = $1`, claimed.IntentID).Scan(
		&status, &owner, &claimUntil, &claimedAt, &replayRequired,
	)
	if err != nil {
		t.Fatalf("read uncommitted replay state: %v", err)
	}
	if status != "claimed" || owner != leaseOwner || !claimUntil.Valid || !replayRequired {
		t.Fatalf("uncommitted replay state = status:%q owner:%q until:%v dirty:%v", status, owner, claimUntil, replayRequired)
	}
	if !wantClaimUntil.Valid || !claimUntil.Time.Equal(wantClaimUntil.Time) {
		t.Fatalf("uncommitted replay claim_until = %v, want %v", claimUntil, wantClaimUntil)
	}
	if !claimedAt.Equal(claimedAtValue(claimed)) {
		t.Fatalf("uncommitted replay claim token = %s, want %s", claimedAt, claimedAtValue(claimed))
	}
}

func assertWorkloadReplayPendingAndReclaimable(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	queue ReducerQueue,
	workItemID string,
) {
	t.Helper()
	state := readClaimTokenWorkState(t, ctx, db, workItemID)
	var replayRequired bool
	if err := db.QueryRowContext(ctx,
		`SELECT cross_scope_replay_required FROM fact_work_items WHERE work_item_id = $1`,
		workItemID,
	).Scan(&replayRequired); err != nil {
		t.Fatalf("read final replay-required bit: %v", err)
	}
	if state.status != "pending" || state.leaseOwner != "" || state.claimUntil.Valid || replayRequired {
		t.Fatalf("final replay state = status:%q owner:%q until:%v dirty:%v, want pending and clear", state.status, state.leaseOwner, state.claimUntil, replayRequired)
	}
	reclaimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok || reclaimed.IntentID != workItemID {
		t.Fatalf("replayed claim = (%q, %v, %v), want (%q, true, nil)", reclaimed.IntentID, ok, err, workItemID)
	}
}
