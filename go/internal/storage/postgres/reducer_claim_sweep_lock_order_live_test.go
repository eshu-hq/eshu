// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8205 reducer claim-time supersede sweep lock order over 400,000 analyzed background rows: two-session sweep against scoped and stage-only recovery replays in both start orders waits one way and the held-middle-row race of the same pairs completes (main: mutual block, then 40P01), 150 rounds of a claim racing scoped, stage-only and bounded replays with 0 x 40P01 and every stale row superseded exactly once (main: 111 of 150 deadlocked), the active-generation guard keeps every active-generation row, and the EvalPlanQual case keeps a row claimed before the sweep's lock step; isolated schemas; about 60 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// staleLockOrderFixture turns the lock-order fixture's 18 target rows into
// stale work: each target scope gets a newer active generation, so the
// reducer claim-time supersede sweep (supersedeInactiveReducerGenerationsCTE)
// retires the rows while replays try to reset them. The layout inverts every
// unordered locker on main against the sweep, which scans
// fact_work_items_stage_domain_status_idx in visible_at order (s5 first):
// a scoped replay scans by scope (s0 first), a stage-only replay scans
// fact_work_items_reducer_source_claim_idx led by source_system (s0 first,
// "src-0" .. "src-5"), and the admin replay selects updated_at DESC (s0
// first).
func staleLockOrderFixture(t *testing.T, background bool) *lockOrderFixture {
	t.Helper()
	f := openLockOrderFixture(t, background)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	lockOrderExec(t, ctx, f.base, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload)
SELECT scope_id || '-g2', scope_id, 'snapshot', 'x', $1::timestamptz,
    $1::timestamptz + interval '1 minute', 'pending', NULL, NULL, '{}'::jsonb
FROM ingestion_scopes WHERE scope_id LIKE 'lo-s%'`, f.at)
	lockOrderExec(t, ctx, f.base, `UPDATE scope_generations SET status = 'superseded' WHERE generation_id LIKE 'lo-s%-g'`)
	lockOrderExec(t, ctx, f.base, `UPDATE scope_generations SET status = 'active', activated_at = $1 WHERE generation_id LIKE 'lo-s%-g2'`, f.at)
	lockOrderExec(t, ctx, f.base, `UPDATE ingestion_scopes SET active_generation_id = scope_id || '-g2' WHERE scope_id LIKE 'lo-s%'`)
	lockOrderExec(t, ctx, f.base, `
UPDATE fact_work_items SET payload = jsonb_build_object('source_system', 'src-' || substr(scope_id, 5))
WHERE scope_id LIKE 'lo-s%'`)
	f.reset(t, ctx)
	lockOrderExec(t, ctx, f.base, `ANALYZE fact_work_items`)
	lockOrderExec(t, ctx, f.base, `ANALYZE scope_generations`)
	return f
}

// lockOrderClaim runs one reducer claim (and with it the supersede sweep) on db.
func lockOrderClaim(ctx context.Context, db *sql.DB) lockOrderReplay {
	r := lockOrderReplay{done: make(chan lockOrderReplayResult, 1)}
	go func() {
		clock := time.Now().UTC()
		_, _, err := newOwnGateQueue(db, &clock, reducer.DomainWorkloadIdentity).Claim(ctx)
		r.done <- lockOrderReplayResult{err: err}
	}()
	return r
}

// supersededAccounting returns "" when every target row is superseded and no
// replay reported a row twice, else a description.
func supersededAccounting(t *testing.T, ctx context.Context, f *lockOrderFixture, replays ...lockOrderReplayResult) string {
	t.Helper()
	var problems []string
	if got := f.statusCounts(t, ctx); got["superseded"] != lockOrderTargetRows {
		problems = append(problems, fmt.Sprintf("target statuses %v, want all %d superseded", got, lockOrderTargetRows))
	}
	seen := map[string]int{}
	for _, replay := range replays {
		for _, id := range replay.ids {
			seen[id]++
		}
	}
	for id, n := range seen {
		if n > 1 {
			problems = append(problems, fmt.Sprintf("%s replayed %d times", id, n))
		}
	}
	return strings.Join(problems, "; ")
}

// assertAllSuperseded fails the test unless supersededAccounting is clean.
func assertAllSuperseded(t *testing.T, ctx context.Context, f *lockOrderFixture, replays ...lockOrderReplayResult) {
	t.Helper()
	if problem := supersededAccounting(t, ctx, f, replays...); problem != "" {
		t.Error(problem)
	}
}

// TestReducerClaimSweepAndReplayBlockOneWayLive drives the claim-time
// supersede sweep against each recovery replay variant on two sessions with
// a 150 ms per-row trigger sleep, in both start orders. On main the sweep
// locked in scan order (s5 first) and every replay variant here locked s0
// first, so each pair met in the middle and one died with 40P01. With both on
// work_item_id COLLATE "C" the second statement waits for the first one way,
// both succeed, and every row ends superseded: a replay that runs first resets
// the rows, and the sweep then retires them as stale pending work.
func TestReducerClaimSweepAndReplayBlockOneWayLive(t *testing.T) {
	f := staleLockOrderFixture(t, true)
	f.installSleepTrigger(t, lockOrderSetupContext(t))
	for _, tc := range []struct {
		name       string
		filter     recovery.ReplayFilter
		sweepFirst bool
	}{
		{name: "sweep then scoped replay", filter: f.scopedFilter(), sweepFirst: true},
		{name: "sweep then stage-only replay", filter: stageOnlyFilter(), sweepFirst: true},
		{name: "scoped replay then sweep", filter: f.scopedFilter()},
		{name: "stage-only replay then sweep", filter: stageOnlyFilter()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			f.reset(t, ctx)
			sweeper, replayer := f.sleepingSession(t, ctx), f.sleepingSession(t, ctx)
			pidSweep, pidReplay := lockOrderBackendPID(t, ctx, sweeper), lockOrderBackendPID(t, ctx, replayer)
			first, second := pidSweep, pidReplay
			var sweep, replay lockOrderReplay
			if tc.sweepFirst {
				sweep = lockOrderClaim(ctx, sweeper)
				waitHoldsRowLock(t, ctx, f.base, pidSweep)
				replay = startLockOrderReplay(ctx, replayer, tc.filter)
			} else {
				first, second = pidReplay, pidSweep
				replay = startLockOrderReplay(ctx, replayer, tc.filter)
				waitHoldsRowLock(t, ctx, f.base, pidReplay)
				sweep = lockOrderClaim(ctx, sweeper)
			}
			blockers := waitBlocked(t, ctx, f.base, second)
			for sample := 0; sample < 3; sample++ {
				if firstBlockers := lockOrderBlockingPIDs(t, ctx, f.base, first); len(firstBlockers) != 0 {
					t.Errorf("sample %d: first statement (pid %d) blocked by %v while second (pid %d) blocked by %v",
						sample, first, firstBlockers, second, lockOrderBlockingPIDs(t, ctx, f.base, second))
				}
				time.Sleep(100 * time.Millisecond)
			}
			sweepResult, replayResult := sweep.wait(), replay.wait()
			if sweepResult.err != nil || replayResult.err != nil {
				t.Fatalf("sweep err=%v (40P01=%t), replay err=%v (40P01=%t)", sweepResult.err,
					isDeadlock(sweepResult.err), replayResult.err, isDeadlock(replayResult.err))
			}
			assertAllSuperseded(t, ctx, f, replayResult)
			t.Logf("second statement waited on %v; replay reported %d rows", blockers, len(replayResult.ids))
		})
	}
}

// TestReducerClaimSweepHeldRowRaceLive makes the claim sweep and each
// recovery replay variant meet at a held middle row (raceThroughHeldRow), in
// both start orders, so the outcome depends only on lock order: in scan order
// the pair held rows on both sides of it and one died with 40P01; in one
// global order both succeed and every row ends superseded.
func TestReducerClaimSweepHeldRowRaceLive(t *testing.T) {
	f := staleLockOrderFixture(t, true)
	claim := func(ctx context.Context, db *sql.DB) lockOrderReplay { return lockOrderClaim(ctx, db) }
	for _, tc := range []struct {
		name   string
		filter recovery.ReplayFilter
	}{
		{name: "scoped", filter: f.scopedFilter()},
		{name: "stage-only", filter: stageOnlyFilter()},
	} {
		replay := func(ctx context.Context, db *sql.DB) lockOrderReplay { return startLockOrderReplay(ctx, db, tc.filter) }
		for _, sweepFirst := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s sweep first %t", tc.name, sweepFirst), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				f.reset(t, ctx)
				first, second := replay, claim
				if sweepFirst {
					first, second = claim, replay
				}
				a, b := f.raceThroughHeldRow(t, ctx, f.ids[len(f.ids)/2], first, second)
				if a.err != nil || b.err != nil {
					t.Fatalf("first err=%v (40P01=%t), second err=%v (40P01=%t)", a.err, isDeadlock(a.err), b.err, isDeadlock(b.err))
				}
				assertAllSuperseded(t, ctx, f, a, b)
			})
		}
	}
}

// TestReducerClaimSweepConcurrentWithReplaysNeverDeadlockLive runs 150 rounds
// of a reducer claim (with its supersede sweep) racing a scoped, a stage-only
// and a bounded replay over the same 18 stale rows. It requires zero 40P01 and
// exact accounting: every row ends superseded and no replay reports a row
// twice.
func TestReducerClaimSweepConcurrentWithReplaysNeverDeadlockLive(t *testing.T) {
	f := staleLockOrderFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	conns := []*sql.DB{f.session(t), f.session(t), f.session(t), f.session(t)}
	reversed := slices.Clone(f.scopes)
	slices.Reverse(reversed)
	var deadlocks [4]int
	accountingFailures := 0
	roundDeadlocks := 0
	started := time.Now()
	for round := 0; round < lockOrderRounds; round++ {
		f.reset(t, ctx)
		roundDeadlocks = 0
		start := make(chan struct{})
		var wg sync.WaitGroup
		var mu sync.Mutex
		var results []lockOrderReplayResult
		run := func(i int, op func(*sql.DB) lockOrderReplayResult) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				result := op(conns[i])
				mu.Lock()
				defer mu.Unlock()
				results = append(results, result)
				switch {
				case isDeadlock(result.err):
					deadlocks[i]++
					roundDeadlocks++
				case result.err != nil:
					t.Errorf("round %d op %d: %v", round, i, result.err)
				}
			}()
		}
		run(0, func(db *sql.DB) lockOrderReplayResult { return lockOrderClaim(ctx, db).wait() })
		run(1, func(db *sql.DB) lockOrderReplayResult { return startLockOrderReplay(ctx, db, f.scopedFilter()).wait() })
		run(2, func(db *sql.DB) lockOrderReplayResult { return startLockOrderReplay(ctx, db, stageOnlyFilter()).wait() })
		run(3, func(db *sql.DB) lockOrderReplayResult {
			var out lockOrderReplayResult
			for _, scope := range reversed {
				r := startLockOrderReplay(ctx, db, recovery.ReplayFilter{
					Stage: recovery.StageReducer, ScopeIDs: []string{scope}, Limit: lockOrderRowsPerScope,
				}).wait()
				out.ids = append(out.ids, r.ids...)
				if r.err != nil {
					out.err = r.err
					return out
				}
			}
			return out
		})
		close(start)
		wg.Wait()
		// A replay may run after the sweep committed and find nothing, or
		// before it and hand the sweep pending rows; either way no row may be
		// left behind and none may be replayed twice. A deadlock aborts one
		// statement, so its round is judged by the deadlock count instead.
		if roundDeadlocks == 0 {
			if problem := supersededAccounting(t, ctx, f, results...); problem != "" {
				accountingFailures++
				if accountingFailures <= 5 {
					t.Logf("round %d accounting: %s", round, problem)
				}
			}
		}
	}
	t.Logf("%d rounds in %s: 40P01 claim=%d scoped=%d stage-only=%d bounded=%d; rounds failing accounting=%d",
		lockOrderRounds, time.Since(started).Round(time.Millisecond),
		deadlocks[0], deadlocks[1], deadlocks[2], deadlocks[3], accountingFailures)
	if total := deadlocks[0] + deadlocks[1] + deadlocks[2] + deadlocks[3]; total != 0 {
		t.Errorf("claim sweep and replays deadlocked %d times in %d rounds, want 0", total, lockOrderRounds)
	}
	if accountingFailures != 0 {
		t.Errorf("%d of %d deadlock-free rounds failed exact accounting", accountingFailures, lockOrderRounds)
	}
}

// TestReducerClaimSweepKeepsActiveGenerationRowsLive pins the sweep's
// generation guard under the new lock step: rows on the scope's active
// generation are never superseded, whatever their status, while the same
// scope's older-generation rows are.
func TestReducerClaimSweepKeepsActiveGenerationRowsLive(t *testing.T) {
	f := staleLockOrderFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for i, status := range []string{"pending", "retrying", "failed", "dead_letter"} {
		id := fmt.Sprintf("lo-active-%d", i)
		lockOrderExec(t, ctx, f.base, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, failure_class, payload, created_at, updated_at, visible_at)
VALUES ($1, 'lo-s0', 'lo-s0-g2', 'reducer', 'workload_identity', 'scope', $1, $2, 3, 'retry_exhausted',
    '{"source_system": "git"}'::jsonb, $3::timestamptz, $3::timestamptz, $3::timestamptz + interval '1 day')`, id, status, f.at)
	}
	if result := lockOrderClaim(ctx, f.session(t)).wait(); result.err != nil {
		t.Fatalf("claim: %v", result.err)
	}
	assertAllSuperseded(t, ctx, f)
	rows, err := f.base.QueryContext(ctx, `SELECT work_item_id, status FROM fact_work_items WHERE work_item_id LIKE 'lo-active-%' ORDER BY 1`)
	if err != nil {
		t.Fatalf("read active rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for i := 0; rows.Next(); i++ {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if want := []string{"pending", "retrying", "failed", "dead_letter"}[i]; status != want {
			t.Errorf("active-generation row %s = %s, want %s untouched", id, status, want)
		}
	}
}

// TestReducerClaimSweepDropsRowClaimedBeforeItsLockLive is the EvalPlanQual
// case of the sweep's lock step: session X claims the first stale row in lock
// order and holds it uncommitted; the sweep takes its snapshot while the row
// still reads dead_letter and waits on it. After X commits, the sweep must
// leave X's claim alone and supersede only the other 17 rows.
func TestReducerClaimSweepDropsRowClaimedBeforeItsLockLive(t *testing.T) {
	f := staleLockOrderFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	target := f.ids[0]
	tx, err := f.session(t).BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin X: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
UPDATE fact_work_items SET status = 'claimed', lease_owner = 'epq-worker', claim_until = $2
WHERE work_item_id = $1`, target, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("X claim: %v", err)
	}
	var pidX int
	if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pidX); err != nil {
		t.Fatalf("X pid: %v", err)
	}
	sweeper := f.session(t)
	pidSweep := lockOrderBackendPID(t, ctx, sweeper)
	sweep := lockOrderClaim(ctx, sweeper)
	if blockers := waitBlocked(t, ctx, f.base, pidSweep); !slices.Equal(blockers, []int{pidX}) {
		t.Fatalf("sweep blocked by %v, want X (%d)", blockers, pidX)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit X: %v", err)
	}
	if result := sweep.wait(); result.err != nil {
		t.Fatalf("claim: %v", result.err)
	}
	counts := f.statusCounts(t, ctx)
	if counts["superseded"] != lockOrderTargetRows-1 || counts["claimed"] != 1 {
		t.Errorf("target statuses %v, want %d superseded and X's 1 claimed", counts, lockOrderTargetRows-1)
	}
}

// lockOrderSetupContext returns a one-minute fixture-setup context bound to t.
func lockOrderSetupContext(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}
