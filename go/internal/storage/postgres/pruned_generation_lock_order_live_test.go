// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8261 generation retention lock order: a real PruneSupersededGenerations pass over a pruned generation's dead letters races the reducer claim sweep and the scoped and stage-only recovery replays through a held middle row in both start orders and completes (main: 6 of 6 40P01 in the FK cascade delete), 150 rounds of the pass, a claim and both replays give 0 x 40P01 and prune every row (main: 14 x 40P01), and a row replayed before the pass's lock step is still pruned; isolated schemas; about 70-90 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// retentionCascadeIDs are the pruned generation's reducer rows, in
// work_item_id COLLATE "C" order. updated_at and visible_at run backwards
// against that order.
var retentionCascadeIDs = []string{"rt-w0", "rt-w1", "rt-w2"}

// retentionCascadeLocker runs one production retention pass
// (PruneSupersededGenerations). Only the superseded generation rt-g1 is
// eligible: every background generation is active. The pass's FK cascade
// delete of rt-g1 removes its fact_work_items rows; the selection and the
// relock before it lock only scope and generation rows.
func retentionCascadeLocker(ctx context.Context, db *sql.DB) ([]string, error) {
	result, err := NewGenerationRetentionStore(SQLDB{DB: db}).PruneSupersededGenerations(ctx, GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100000,
		PolicyScope:              "global",
		PolicyRevision:           "8261-cascade-lock-order",
	})
	if err != nil {
		return nil, err
	}
	if result.GenerationsPruned != 1 {
		return nil, fmt.Errorf("pruned %d generations, want 1 (rt-g1)", result.GenerationsPruned)
	}
	return nil, nil
}

// TestRetentionCascadeHeldRowRaceLive races retention's FK-cascade delete of
// a pruned generation's work rows against the ordered lockers that can hold
// the same rows (#8261): the reducer claim sweep (an older generation's
// dead letters under a newer active generation) and the recovery replays
// (dead letters of any generation). Each pair runs in both start orders
// through the held middle row. Every target row must end deleted.
func TestRetentionCascadeHeldRowRaceLive(t *testing.T) {
	f := openUnorderedLockerFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	f.seedLockerScope(t, ctx, "rt", "rt-g2")

	others := []struct {
		name   string
		locker func(context.Context, *sql.DB) ([]string, error)
	}{
		{name: "claim sweep", locker: claimSweepLocker},
		{name: "scoped replay", locker: recoveryReplayLocker(recovery.ReplayFilter{Stage: recovery.StageReducer, ScopeIDs: []string{"rt"}})},
		{name: "stage-only replay", locker: recoveryReplayLocker(recovery.ReplayFilter{Stage: recovery.StageReducer})},
	}
	for _, other := range others {
		for _, retentionFirst := range []bool{true, false} {
			name := other.name + "/retention second"
			if retentionFirst {
				name = other.name + "/retention first"
			}
			t.Run(name, func(t *testing.T) {
				f.seedRetentionGeneration(t, ctx, retentionCascadeIDs)
				t.Logf("cascade scans fact_work_items via %v", factWorkItemsScans(t, ctx, f.base,
					`DELETE FROM ONLY fact_work_items WHERE $1 OPERATOR(pg_catalog.=) generation_id`, "rt-g1"))
				a, b, nameA, nameB := retentionCascadeLocker, other.locker, "retention cascade", other.name
				if !retentionFirst {
					a, b, nameA, nameB = other.locker, retentionCascadeLocker, other.name, "retention cascade"
				}
				f.heldRowPair(t, ctx, retentionCascadeIDs[1], nameA, nameB, a, b)
				if left := f.workStatuses(t, ctx, retentionCascadeIDs); len(left) != 0 {
					t.Errorf("rows left after the prune: %v", left)
				}
			})
		}
	}
}

// seedRetentionGeneration (re)creates the superseded generation rt-g1 with
// one dead-lettered reducer row per id, under the active generation rt-g2.
// The rows are inserted in reverse id order and their updated_at runs
// backwards against the id order, so neither a heap scan nor an index
// ordered by updated_at DESC visits them in work_item_id order.
func (f *lockOrderFixture) seedRetentionGeneration(t *testing.T, ctx context.Context, ids []string) {
	t.Helper()
	lockOrderExec(t, ctx, f.base, `DELETE FROM scope_generations WHERE scope_id = 'rt'`)
	f.seedLockerGeneration(t, ctx, "rt", "rt-g1", "superseded", -2*time.Hour)
	f.seedLockerGeneration(t, ctx, "rt", "rt-g2", "active", 0)
	for i := len(ids) - 1; i >= 0; i-- {
		f.seedLockerWork(t, ctx, ids[i], "rt", "rt-g1", "reducer", "dead_letter", -time.Duration(i)*time.Second)
	}
	f.analyzeLockerTables(t, ctx)
}

// TestRetentionCascadeDeletesRowChangedBeforeItsLockLive is the EvalPlanQual
// case of retention's ordered work-row lock (#8261). Session X replays the
// first row in lock order (dead_letter to pending) and holds it uncommitted;
// the retention pass reaches its lock step and waits on it. After X commits,
// the pass must still delete every row of the pruned generation: the lock
// step selects by generation only, exactly the rows the cascade deletes,
// whatever their status.
func TestRetentionCascadeDeletesRowChangedBeforeItsLockLive(t *testing.T) {
	f := openUnorderedLockerFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	f.seedLockerScope(t, ctx, "rt", "rt-g2")
	f.seedRetentionGeneration(t, ctx, retentionCascadeIDs)
	x := f.session(t)
	tx, err := x.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin X: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE fact_work_items SET status = 'pending' WHERE work_item_id = $1`,
		retentionCascadeIDs[0]); err != nil {
		t.Fatalf("X replay: %v", err)
	}
	y := f.session(t)
	pidY := lockOrderBackendPID(t, ctx, y)
	run := startLocker(ctx, y, retentionCascadeLocker)
	waitBlocked(t, ctx, f.base, pidY)
	if err := tx.Commit(); err != nil {
		t.Fatalf("X commit: %v", err)
	}
	if result := run.wait(); result.err != nil {
		t.Fatalf("retention pass: %v", result.err)
	}
	if left := f.workStatuses(t, ctx, retentionCascadeIDs); len(left) != 0 {
		t.Errorf("rows left after the prune: %v", left)
	}
}

// TestRetentionCascadeConcurrentWithSweepAndReplaysNeverDeadlockLive runs a
// retention pass, a reducer claim (with its supersede sweep), a scoped and a
// stage-only recovery replay concurrently over a pruned generation's 18
// dead letters for lockOrderRounds rounds (#8261). Every round must prune
// the generation and leave none of its rows.
func TestRetentionCascadeConcurrentWithSweepAndReplaysNeverDeadlockLive(t *testing.T) {
	f := openUnorderedLockerFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	f.seedLockerScope(t, ctx, "rt", "rt-g2")
	ids := make([]string, 0, 18)
	for i := 0; i < 18; i++ {
		ids = append(ids, fmt.Sprintf("rt-w%02d", i))
	}
	f.installSleepTrigger(t, ctx)
	reset := func() {
		lockOrderExec(t, ctx, f.base, `DELETE FROM scope_generations WHERE scope_id = 'rt'`)
		f.seedLockerGeneration(t, ctx, "rt", "rt-g1", "superseded", -2*time.Hour)
		f.seedLockerGeneration(t, ctx, "rt", "rt-g2", "active", 0)
		for i := len(ids) - 1; i >= 0; i-- {
			f.seedLockerWork(t, ctx, ids[i], "rt", "rt-g1", "reducer", "dead_letter", -time.Duration(i)*time.Second)
		}
	}
	reset()
	f.analyzeLockerTables(t, ctx)
	check := func([]lockOrderReplayResult) string {
		if left := f.workStatuses(t, ctx, ids); len(left) != 0 {
			return fmt.Sprintf("%d rows left after the prune", len(left))
		}
		return ""
	}
	f.stressLockers(t, ctx, reset, check, []namedLocker{
		{name: "retention", locker: retentionCascadeLocker},
		{name: "claim", locker: claimSweepLocker},
		{name: "scoped", locker: recoveryReplayLocker(recovery.ReplayFilter{Stage: recovery.StageReducer, ScopeIDs: []string{"rt"}})},
		{name: "stage-only", locker: recoveryReplayLocker(recovery.ReplayFilter{Stage: recovery.StageReducer})},
	})
}
