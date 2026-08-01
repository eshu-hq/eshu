// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// TestEnsureDeferredMaintenanceBarrierEpochNeverCommittedShardDoesNotOpenNewEpoch
// pins the join-only half of the #5852 follow-up fix directly at the
// decision point: a shard with nothing committed since its last drain
// (canOpenEpoch=false) must not open a new epoch when none is open, so a
// quiet fleet where nothing ever commits never runs the corpus-wide
// maintenance pass at all.
func TestEnsureDeferredMaintenanceBarrierEpochNeverCommittedShardDoesNotOpenNewEpoch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	rows := &closeTrackingRows{}
	tx := &openRowsRejectingTx{latestRows: rows}

	epoch, hasEpoch, err := ensureDeferredMaintenanceBarrierEpoch(context.Background(), tx, 2, now, false)
	if err != nil {
		t.Fatalf("ensureDeferredMaintenanceBarrierEpoch() error = %v, want nil", err)
	}
	if hasEpoch {
		t.Fatal("hasEpoch = true, want false (a never-committed shard must not open a new epoch)")
	}
	if epoch != 0 {
		t.Fatalf("epoch = %d, want 0", epoch)
	}
	if !rows.closed {
		t.Fatal("latest barrier rows were not closed")
	}
	if got, want := tx.execCount, 0; got != want {
		t.Fatalf("exec count = %d, want %d (must not insert a new epoch)", got, want)
	}
}

// TestEnsureDeferredMaintenanceBarrierEpochNeverCommittedShardJoinsAlreadyOpenEpoch
// pins the other half: canOpenEpoch=false only blocks OPENING a new epoch. A
// never-committed shard must still be able to JOIN an epoch that a committing
// shard already opened — that is the original #5852 stall fix, and it must
// survive the join-only change.
func TestEnsureDeferredMaintenanceBarrierEpochNeverCommittedShardJoinsAlreadyOpenEpoch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	rows := &closeTrackingRows{next: true, epoch: 7, shardCount: 2, completedAt: sql.NullTime{}}
	tx := &openRowsRejectingTx{latestRows: rows}

	epoch, hasEpoch, err := ensureDeferredMaintenanceBarrierEpoch(context.Background(), tx, 2, now, false)
	if err != nil {
		t.Fatalf("ensureDeferredMaintenanceBarrierEpoch() error = %v, want nil", err)
	}
	if !hasEpoch {
		t.Fatal("hasEpoch = false, want true (a never-committed shard must still join an already-open epoch)")
	}
	if epoch != 7 {
		t.Fatalf("epoch = %d, want 7", epoch)
	}
	if !rows.closed {
		t.Fatal("latest barrier rows were not closed")
	}
	if got, want := tx.execCount, 0; got != want {
		t.Fatalf("exec count = %d, want %d (joining an open epoch must not insert a new one)", got, want)
	}
}

// TestIngestionStoreShardDrainBarrierNeverCommittedShardJoinsAlreadyOpenEpochAndBecomesLeader
// proves the original #5852 stall fix survives the join-only follow-up: a
// never-committed shard (HasCommitted: false) must still be able to join an
// epoch a committing shard already opened, and — if it is the final
// arrival — become leader and run maintenance, exactly like a committing
// shard would. Otherwise the fleet would deadlock again: the one shard that
// owns no repositories would never unblock the others. This mirrors
// TestIngestionStoreShardDrainBarrierLeaderRunsMaintenanceAfterAllShardsArrive
// with only HasCommitted flipped to false on the arriving shard, to isolate
// exactly what join-only changes (nothing, for an already-open epoch).
func TestIngestionStoreShardDrainBarrierNeverCommittedShardJoinsAlreadyOpenEpochAndBecomesLeader(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	barrierTx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{int64(7), 2, sql.NullTime{}}}}, // epoch 7, opened by a committing shard
			{rows: [][]any{{2}}},
		},
	}
	batchTx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"repo-infra", "scope-infra", "gen-infra"}}},
		},
	}
	reopenTx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"work-item-1", "scope-infra", "gen-infra"}}},
		},
	}
	completionTx := &fakeTx{}
	db := &fakeTransactionalDB{
		txs: []*fakeTx{barrierTx, batchTx, reopenTx, completionTx},
		queryResponses: []queueFakeRows{
			{rows: [][]any{{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`), catalogFakeObservedAt}}},
			{rows: [][]any{}},
			{rows: [][]any{{"repo-infra", "scope-infra", "gen-infra"}}},
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	err := store.RunDeferredRelationshipMaintenanceAfterShardDrain(
		context.Background(),
		// This shard has never committed, but epoch 7 is already open — it must
		// still be allowed to join and, as the final arrival, lead.
		DeferredMaintenanceBarrierConfig{ShardCount: 2, ShardIndex: 1, HasCommitted: false},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenanceAfterShardDrain() error = %v, want nil (a never-committed shard must still be able to join an already-open epoch)", err)
	}
	if !barrierTx.committed || !batchTx.committed || !reopenTx.committed || !completionTx.committed {
		t.Fatalf("not all transactions committed: barrier=%v batch=%v reopen=%v completion=%v",
			barrierTx.committed, batchTx.committed, reopenTx.committed, completionTx.committed)
	}
	assertExecContains(t, barrierTx.execs, "INSERT INTO deferred_maintenance_barrier_arrivals")
	assertExecContains(t, batchTx.execs, "INSERT INTO graph_projection_phase_state")
	assertExecContains(t, completionTx.execs, "completed_at = $4")
	// Joining an already-open epoch must never insert a new one.
	for _, exec := range barrierTx.execs {
		if strings.Contains(exec.query, "INSERT INTO deferred_maintenance_barriers") {
			t.Fatalf("joining shard inserted a new epoch: %q", exec.query)
		}
	}
}

// quietFleetBarrierState, quietFleetDB, quietFleetTx, quietFleetIdleSource,
// quietFleetNoopCommitter, and
// TestIngestionStoreShardDrainBarrierQuietRestartOpensExactlyOneEpochAcrossManyIdlePolls
// live in deferred_maintenance_barrier_quiet_fleet_test.go (split out to keep
// this file under the 500-line cap).

// alwaysFailBarrierDB fails any DB interaction. It proves a never-committed
// single-shard drain takes a no-op join-only path without touching Postgres
// at all — the ShardCount==1 analogue of the multi-shard quiet-restart case
// above (a single shard IS the entire fleet, so the same "nothing committed,
// nothing owed" invariant applies without needing barrier bookkeeping).
type alwaysFailBarrierDB struct {
	t *testing.T
}

func (d *alwaysFailBarrierDB) Begin(context.Context) (Transaction, error) {
	d.t.Fatal("Begin called for a never-committed single-shard drain; want no DB interaction at all")
	return nil, nil
}

func (d *alwaysFailBarrierDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	d.t.Fatal("ExecContext called for a never-committed single-shard drain; want no DB interaction at all")
	return nil, nil
}

func (d *alwaysFailBarrierDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	d.t.Fatal("QueryContext called for a never-committed single-shard drain; want no DB interaction at all")
	return nil, nil
}

// TestIngestionStoreShardDrainBarrierSingleShardNeverCommittedSkipsMaintenance
// is the regression for the ShardCount==1 storm: before the fix, the
// ShardCount==1 short-circuit ran RunDeferredRelationshipMaintenance
// unconditionally on every idle poll, ignoring commit status — the same
// corpus-wide-maintenance-on-a-quiet-restart storm as the multi-shard case,
// just reached through the single-shard path instead of the barrier, and
// arguably more impactful since single-shard is the default deployment
// topology. Watched-fail evidence for this exact test against the pre-fix
// code is in the PR/handoff report; it failed on the first poll with
// "QueryContext called for a never-committed single-shard drain", proving
// RunDeferredRelationshipMaintenance ran and touched Postgres despite no
// commit ever happening.
func TestIngestionStoreShardDrainBarrierSingleShardNeverCommittedSkipsMaintenance(t *testing.T) {
	t.Parallel()

	db := &alwaysFailBarrierDB{t: t}
	store := NewIngestionStore(db)

	for poll := 0; poll < 5; poll++ {
		err := store.RunDeferredRelationshipMaintenanceAfterShardDrain(
			context.Background(),
			DeferredMaintenanceBarrierConfig{ShardCount: 1, ShardIndex: 0, HasCommitted: false},
			nil,
			nil,
		)
		if err != nil {
			t.Fatalf("RunDeferredRelationshipMaintenanceAfterShardDrain(poll=%d) error = %v, want nil", poll, err)
		}
	}
}

// TestIngestionStoreShardDrainBarrierSingleShardHasCommittedRunsMaintenance
// is the companion positive case: a single shard that HAS committed must
// still run maintenance directly, exactly as before the join-only change.
func TestIngestionStoreShardDrainBarrierSingleShardHasCommittedRunsMaintenance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	batchTx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"repo-infra", "scope-infra", "gen-infra"}}},
		},
	}
	reopenTx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"work-item-1", "scope-infra", "gen-infra"}}},
		},
	}
	db := &fakeTransactionalDB{
		txs: []*fakeTx{batchTx, reopenTx},
		queryResponses: []queueFakeRows{
			{rows: [][]any{{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`), catalogFakeObservedAt}}},
			{rows: [][]any{}},
			{rows: [][]any{{"repo-infra", "scope-infra", "gen-infra"}}},
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	err := store.RunDeferredRelationshipMaintenanceAfterShardDrain(
		context.Background(),
		DeferredMaintenanceBarrierConfig{ShardCount: 1, ShardIndex: 0, HasCommitted: true},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenanceAfterShardDrain() error = %v, want nil", err)
	}
	assertExecContains(t, batchTx.execs, "INSERT INTO graph_projection_phase_state")
	if !batchTx.committed {
		t.Fatal("batch transaction committed = false, want true")
	}
}
