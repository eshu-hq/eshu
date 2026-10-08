// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
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
	intent := runtime.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainWorkloadMaterialization,
		EntityKey:    entityKey,
		Reason:       "workload replay claim proof",
		SourceSystem: "reducer",
	}

	queue := NewReducerQueue(SQLDB{DB: db}, "workload-replay-worker", time.Minute)
	queue.ClaimDomain = reducer.DomainWorkloadMaterialization
	if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{intent}); err != nil {
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

// TestWorkloadReplayClaimAfterAckToleratesSkewedAppClock proves #6828: the
// claim visibility check runs on the database clock, so a just-reopened row
// (visible_at stamped by the dirty-reopen trigger from clock_timestamp())
// stays claimable even when the app clock lags the database clock by
// seconds. Both the single-claim and batch-claim paths are covered. Pre-fix
// this test fails with "claim after ACK returned no work"; no sleeps or
// retries are involved, only the queue.Now seam.
func TestWorkloadReplayClaimAfterAckToleratesSkewedAppClock(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, generationID, _ := refinalizeResetScope(t, ctx, db, suffix)

	queue := NewReducerQueue(SQLDB{DB: db}, "workload-replay-skew", time.Minute)
	queue.ClaimDomain = reducer.DomainWorkloadMaterialization
	// Skew the APP clock 5s behind the database clock for the whole flow.
	// Fresh rows carry app-stamped visible_at at the skewed claim instant,
	// so the first claim passes through the $1 arm; only the post-ACK
	// reopened rows (visible_at re-stamped from the database clock)
	// exercise the pending arm.
	queue.Now = func() time.Time { return time.Now().Add(-5 * time.Second) }

	enqueue := func(entityKey string) {
		t.Helper()
		intent := runtime.ReducerIntent{
			ScopeID:      scopeID,
			GenerationID: generationID,
			Domain:       reducer.DomainWorkloadMaterialization,
			EntityKey:    entityKey,
			Reason:       "6828 skewed-clock proof",
			SourceSystem: "reducer",
		}
		if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{intent}); err != nil {
			t.Fatalf("enqueue %s: %v", entityKey, err)
		}
	}
	dirty := func(entityKey string) {
		t.Helper()
		replayed, err := queue.ReplayWorkloadMaterialization(ctx, scopeID, generationID, entityKey)
		if err != nil {
			t.Fatalf("schedule replay for %s: %v", entityKey, err)
		}
		if !replayed {
			t.Fatalf("schedule replay for %s returned false", entityKey)
		}
	}

	// Single-claim path: enqueue, claim, dirty, ack, then the skewed claim
	// must still find the reopened row.
	singleKey := "repo:workload-replay-skew-single-" + suffix
	enqueue(singleKey)
	claimed, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("claim single workload materialization: %v", err)
	}
	if !ok {
		t.Fatal("claim single workload materialization returned no work")
	}
	dirty(singleKey)
	if err := queue.Ack(ctx, claimed, reducer.Result{}); err != nil {
		t.Fatalf("ack dirtied single workload materialization: %v", err)
	}
	second, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("claim reopened single workload materialization: %v", err)
	}
	if !ok || second.IntentID != claimed.IntentID {
		t.Fatalf("reopened single claim = (%q, %v), want (%q, true)", second.IntentID, ok, claimed.IntentID)
	}
	// Retire the single row so the batch phase sees only its own row.
	if err := queue.Ack(ctx, second, reducer.Result{}); err != nil {
		t.Fatalf("ack reopened single workload materialization: %v", err)
	}

	// Batch-claim path: same flow through ClaimBatch/AckBatch.
	batchKey := "repo:workload-replay-skew-batch-" + suffix
	enqueue(batchKey)
	first, err := queue.ClaimBatch(ctx, 10)
	if err != nil {
		t.Fatalf("batch-claim workload materialization: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("batch-claim workload materialization returned %d intents, want 1", len(first))
	}
	dirty(batchKey)
	if err := queue.AckBatch(ctx, first, []reducer.Result{{}}); err != nil {
		t.Fatalf("batch-ack dirtied workload materialization: %v", err)
	}
	reopened, err := queue.ClaimBatch(ctx, 10)
	if err != nil {
		t.Fatalf("batch-claim reopened workload materialization: %v", err)
	}
	if len(reopened) != 1 || reopened[0].IntentID != first[0].IntentID {
		t.Fatalf("reopened batch claim = %d intents, want the 1 reopened row", len(reopened))
	}
}

func TestWorkloadReplayConcurrentFirstScheduleReportsSuccess(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, generationID, _ := refinalizeResetScope(t, ctx, db, suffix)
	entityKey := "repo:workload-first-replay-" + suffix
	queue := NewReducerQueue(SQLDB{DB: db}, "workload-first-replay", time.Minute)
	firstConn := ackFanoutProbeConnection(t, ctx, db, "workload-first-replay-winner")
	waiterConn := ackFanoutProbeConnection(t, ctx, db, "workload-first-replay-waiter")
	firstPID := workloadReplayBackendPID(t, ctx, firstConn)
	waiterPID := workloadReplayBackendPID(t, ctx, waiterConn)

	firstTx, err := firstConn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin first replay transaction: %v", err)
	}
	t.Cleanup(func() { _ = firstTx.Rollback() })
	firstQueue := queue
	firstQueue.database = SQLTx{Tx: firstTx}
	replayed, err := firstQueue.ReplayWorkloadMaterialization(ctx, scopeID, generationID, entityKey)
	if err != nil || !replayed {
		t.Fatalf("first replay = (%v, %v), want (true, nil)", replayed, err)
	}

	waiterQueue := queue
	waiterQueue.database = waiterConn
	waiterDone := make(chan workloadReplayOutcome, 1)
	go func() {
		got, replayErr := waiterQueue.ReplayWorkloadMaterialization(ctx, scopeID, generationID, entityKey)
		waiterDone <- workloadReplayOutcome{replayed: got, err: replayErr}
	}()
	waitForReducerRowLockWaiter(t, ctx, db, waiterPID, firstPID)
	if err := firstTx.Commit(); err != nil {
		t.Fatalf("commit first replay transaction: %v", err)
	}
	outcome := <-waiterDone
	if outcome.err != nil || !outcome.replayed {
		t.Fatalf("concurrent replay loser = (%v, %v), want (true, nil)", outcome.replayed, outcome.err)
	}
}

func TestWorkloadFencedReplaySupersedesOnlyOlderInFlightToken(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	queue, claimed, scopeID, generationID, entityKey := seedClaimedWorkloadReplay(
		t, ctx, db, "fenced-token",
	)
	repoID := "repository:fenced-token"

	replayed, err := queue.ReplayWorkloadMaterializationForFence(
		ctx, scopeID, generationID, entityKey, repoID, "fence-b",
	)
	if err != nil || !replayed {
		t.Fatalf("schedule fence B = (%v, %v), want (true, nil)", replayed, err)
	}
	assertWorkloadReplayFenceState(t, ctx, db, claimed.IntentID, "fence-b", true)
	if err := queue.Ack(ctx, claimed, reducer.Result{}); err != nil {
		t.Fatalf("ack pre-fence claim: %v", err)
	}

	claimB, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim fence B = (%v, %v), want work", ok, err)
	}
	if got := claimB.Payload[reducer.RepoDependencyReadinessFencePayloadKey]; got != "fence-b" {
		t.Fatalf("claim B fence = %v, want fence-b", got)
	}
	if replayed, err = queue.ReplayWorkloadMaterializationForFence(
		ctx, scopeID, generationID, entityKey, repoID, "fence-b",
	); err != nil || !replayed {
		t.Fatalf("repeat fence B = (%v, %v), want (true, nil)", replayed, err)
	}
	assertWorkloadReplayFenceState(t, ctx, db, claimB.IntentID, "fence-b", false)

	if replayed, err = queue.ReplayWorkloadMaterializationForFence(
		ctx, scopeID, generationID, entityKey, repoID, "fence-c",
	); err != nil || !replayed {
		t.Fatalf("schedule fence C = (%v, %v), want (true, nil)", replayed, err)
	}
	assertWorkloadReplayFenceState(t, ctx, db, claimB.IntentID, "fence-c", true)
	if err := queue.Ack(ctx, claimB, reducer.Result{}); err != nil {
		t.Fatalf("ack superseded fence B claim: %v", err)
	}
	claimC, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim fence C = (%v, %v), want work", ok, err)
	}
	if got := claimC.Payload[reducer.RepoDependencyReadinessFencePayloadKey]; got != "fence-c" {
		t.Fatalf("claim C fence = %v, want fence-c", got)
	}
}

func assertWorkloadReplayFenceState(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	wantFence string,
	wantReplay bool,
) {
	t.Helper()
	var fence string
	var replay bool
	if err := db.QueryRowContext(ctx, `
SELECT COALESCE(payload->>'repo_dependency_readiness_fence', ''), cross_scope_replay_required
FROM fact_work_items
WHERE work_item_id = $1`, workItemID).Scan(&fence, &replay); err != nil {
		t.Fatalf("read workload replay fence state: %v", err)
	}
	if fence != wantFence || replay != wantReplay {
		t.Fatalf("workload replay fence state = (%q, %v), want (%q, %v)", fence, replay, wantFence, wantReplay)
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
	ackQueue.database = SQLTx{Tx: ackTx}
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
	replayQueue.database = replayConn
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
	replayQueue.database = SQLTx{Tx: replayTx}
	replayed, err := replayQueue.ReplayWorkloadMaterialization(ctx, scopeID, generationID, entityKey)
	if err != nil || !replayed {
		t.Fatalf("uncommitted replay = (%v, %v), want (true, nil)", replayed, err)
	}
	assertUncommittedWorkloadReplayKeepsClaim(
		t, ctx, replayTx, claimed, queue.LeaseOwner, preReplay.claimUntil,
	)

	ackQueue := queue
	ackQueue.database = ackConn
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
	intent := runtime.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainWorkloadMaterialization,
		EntityKey:    entityKey,
		Reason:       "workload replay contention proof",
		SourceSystem: "reducer",
	}
	queue := NewReducerQueue(SQLDB{DB: db}, "workload-replay-worker-"+suffix, time.Minute)
	queue.ClaimDomain = reducer.DomainWorkloadMaterialization
	if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{intent}); err != nil {
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

// TestWorkloadReplayOutcomeReportsSupersededStableItem drives a real
// superseded workload_materialization row through the replay (#7670). The
// UPDATE excludes superseded, the enqueue conflicts on the stable identity, and
// the follow-up read must report the closed outcome superseded while the
// boolean method keeps answering false for its other callers. The replay must
// also leave the row superseded: the queue never revives it.
func TestWorkloadReplayOutcomeReportsSupersededStableItem(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, _, retiredGeneration := refinalizeResetScope(t, ctx, db, suffix)
	entityKey := "repo:workload-superseded-replay-" + suffix
	intent := runtime.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: retiredGeneration,
		Domain:       reducer.DomainWorkloadMaterialization,
		EntityKey:    entityKey,
		Reason:       "superseded stable item proof",
		SourceSystem: "reducer",
	}
	queue := NewReducerQueue(SQLDB{DB: db}, "workload-superseded-replay", time.Minute)
	if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{intent}); err != nil {
		t.Fatalf("enqueue workload materialization: %v", err)
	}
	workItemID := reducerWorkItemID(intent)
	seeded, err := db.ExecContext(ctx,
		`UPDATE fact_work_items SET status = 'superseded' WHERE work_item_id = $1`, workItemID,
	)
	if err != nil {
		t.Fatalf("mark stable item superseded: %v", err)
	}
	if affected, err := seeded.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("superseded seed rows affected = (%d, %v), want (1, nil): the proof needs the enqueued row", affected, err)
	}

	outcome, err := queue.ReplayWorkloadMaterializationOutcome(ctx, scopeID, retiredGeneration, entityKey)
	if err != nil {
		t.Fatalf("ReplayWorkloadMaterializationOutcome() error = %v", err)
	}
	if outcome != reducer.WorkloadMaterializationReplaySuperseded {
		t.Fatalf("outcome = %q, want %q", outcome, reducer.WorkloadMaterializationReplaySuperseded)
	}
	replayed, err := queue.ReplayWorkloadMaterialization(ctx, scopeID, retiredGeneration, entityKey)
	if err != nil {
		t.Fatalf("ReplayWorkloadMaterialization() error = %v", err)
	}
	if replayed {
		t.Fatal("ReplayWorkloadMaterialization() = true for a superseded stable item, want false")
	}
	var status string
	if err := db.QueryRowContext(ctx,
		`SELECT status FROM fact_work_items WHERE work_item_id = $1`, workItemID,
	).Scan(&status); err != nil {
		t.Fatalf("read stable item status: %v", err)
	}
	if status != "superseded" {
		t.Fatalf("stable item status after replay = %q, want superseded: replay must not revive it", status)
	}
}
