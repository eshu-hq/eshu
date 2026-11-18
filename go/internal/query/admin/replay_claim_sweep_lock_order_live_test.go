// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8205 admin replay against the standalone stale-generation sweep (SKIP LOCKED since #10863, so it never waits) in both start orders and through the held-middle-row race (main: 40P01), and 150 rounds of a sweep, a scoped and a stage-only recovery replay and an admin replay started together over the same stale rows give 0 x 40P01 with exact accounting after follow-up passes (main: 212 x 40P01); isolated schemas; about 60-90 s locally including two 400,000-row seeds; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// makeStale gives every target scope a newer active generation, so the
// standalone stale-generation sweep retires the 18 target rows, and runs
// source_system forward with the scope order.
func (f *adminLockOrderFixture) makeStale(t *testing.T, ctx context.Context) {
	t.Helper()
	interleaveExec(t, ctx, f.base, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload)
SELECT scope_id || '-g2', scope_id, 'snapshot', 'x', $1::timestamptz,
    $1::timestamptz + interval '1 minute', 'pending', NULL, NULL, '{}'::jsonb
FROM ingestion_scopes WHERE scope_id LIKE 'lo-s%'`, f.at)
	interleaveExec(t, ctx, f.base, `UPDATE scope_generations SET status = 'superseded' WHERE generation_id LIKE 'lo-s%-g'`)
	interleaveExec(t, ctx, f.base, `UPDATE scope_generations SET status = 'active', activated_at = $1 WHERE generation_id LIKE 'lo-s%-g2'`, f.at)
	interleaveExec(t, ctx, f.base, `UPDATE ingestion_scopes SET active_generation_id = scope_id || '-g2' WHERE scope_id LIKE 'lo-s%'`)
	interleaveExec(t, ctx, f.base, `
UPDATE fact_work_items SET payload = jsonb_build_object('source_system', 'src-' || substr(scope_id, 5))
WHERE scope_id LIKE 'lo-s%'`)
	f.resetStale(t, ctx)
	interleaveExec(t, ctx, f.base, `ANALYZE fact_work_items`)
	interleaveExec(t, ctx, f.base, `ANALYZE scope_generations`)
}

// resetStale returns every target row to dead_letter with visible_at and
// updated_at both running backwards against the scope order. The sweep then
// scans s5 first (visible_at order) while the admin selection, newest
// updated_at first, takes s0 first: the opposite order on main.
func (f *adminLockOrderFixture) resetStale(t *testing.T, ctx context.Context) {
	t.Helper()
	interleaveExec(t, ctx, f.base, `
UPDATE fact_work_items
SET status = 'dead_letter', failure_class = $2, attempt_count = 3, lease_owner = NULL,
    claim_until = NULL, next_attempt_at = NULL, failure_details = NULL,
    visible_at = $1::timestamptz - ((substr(scope_id, 5)::int * 3 + substr(work_item_id, 8)::int) * interval '1 second'),
    updated_at = $1::timestamptz - ((substr(scope_id, 5)::int * 3 + substr(work_item_id, 8)::int) * interval '1 second')
WHERE scope_id LIKE 'lo-s%'`, f.at, adminLockOrderClass)
}

// startSweep runs one bounded standalone supersede sweep pass on db
// (#10863: the sweep no longer rides a claim).
func startSweep(ctx context.Context, db *sql.DB) adminLockOrderReplay {
	r := adminLockOrderReplay{done: make(chan adminLockOrderResult, 1)}
	go func() {
		_, err := interleaveQueue(db, reducer.DomainWorkloadIdentity).SupersedeStaleReducerGenerations(ctx, 1000)
		r.done <- adminLockOrderResult{err: err}
	}()
	return r
}

// supersededAccounting returns "" when every target row is superseded and no
// replay reported a row twice.
func (f *adminLockOrderFixture) supersededAccounting(t *testing.T, ctx context.Context, results ...adminLockOrderResult) string {
	t.Helper()
	var problems []string
	var superseded int
	if err := f.base.QueryRowContext(ctx, `
SELECT count(*) FROM fact_work_items WHERE scope_id LIKE 'lo-s%' AND status = 'superseded'`).Scan(&superseded); err != nil {
		t.Fatalf("count superseded: %v", err)
	}
	if superseded != adminLockOrderRows {
		problems = append(problems, fmt.Sprintf("%d of %d target rows superseded", superseded, adminLockOrderRows))
	}
	seen := map[string]int{}
	for _, result := range results {
		for _, id := range result.ids {
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

// TestAdminReplayAndClaimSweepBlockOneWayLive drives the admin replay against
// the standalone supersede sweep on two sessions with a 150 ms per-row
// trigger sleep, in both start orders (#8205). On main the admin UPDATE
// locked s0 first and the in-claim sweep s5 first, so the pair formed a
// lock cycle. The sweep now locks in work_item_id COLLATE "C" order with
// SKIP LOCKED (#10863), so it never waits on the replay: no cycle can
// form, both statements succeed, and after follow-up sweep passes (the
// rows a pass skipped while the replay held them) every row ends
// superseded. The name keeps its enrolled form; the one-way wait it
// names is now the replay waiting on the sweep, never the reverse.
func TestAdminReplayAndClaimSweepBlockOneWayLive(t *testing.T) {
	f := openAdminLockOrderFixture(t, true)
	setup, cancelSetup := context.WithTimeout(context.Background(), time.Minute)
	defer cancelSetup()
	f.makeStale(t, setup)
	stageOnly := admin.ReplayWorkItemFilter{Stage: "reducer", Limit: 100}
	for _, sweepFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("sweep first %t", sweepFirst), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			f.resetStale(t, ctx)
			sweeper, pidSweep := f.sleepingSession(t, ctx)
			replayer, pidReplay := f.sleepingSession(t, ctx)
			var sweep, replay adminLockOrderReplay
			if sweepFirst {
				sweep = startSweep(ctx, sweeper)
				f.waitHoldsRowLock(t, ctx, pidSweep)
				replay = startAdminReplay(ctx, replayer, stageOnly)
			} else {
				replay = startAdminReplay(ctx, replayer, stageOnly)
				f.waitHoldsRowLock(t, ctx, pidReplay)
				sweep = startSweep(ctx, sweeper)
			}
			// SKIP LOCKED: the sweep must never be blocked, in either
			// start order, so no lock cycle with the replay can form.
			for sample := 0; sample < 3; sample++ {
				if sweepBlockers := f.blockingPIDs(t, ctx, pidSweep); len(sweepBlockers) != 0 {
					t.Errorf("sample %d: sweep (pid %d) blocked by %v; SKIP LOCKED must never wait", sample, pidSweep, sweepBlockers)
				}
				time.Sleep(100 * time.Millisecond)
			}
			sweepResult, replayResult := sweep.wait(), replay.wait()
			if sweepResult.err != nil || replayResult.err != nil {
				t.Fatalf("sweep err=%v (40P01=%t), admin replay err=%v (40P01=%t)", sweepResult.err,
					isDeadlock(sweepResult.err), replayResult.err, isDeadlock(replayResult.err))
			}
			adminSweepUntilDrained(t, ctx, sweeper)
			if problem := f.supersededAccounting(t, ctx, replayResult); problem != "" {
				t.Errorf("accounting: %s", problem)
			}
			t.Logf("admin replay reported %d rows", len(replayResult.ids))
		})
	}
}

// TestAdminReplayAndClaimSweepHeldRowRaceLive makes the admin replay and the
// standalone supersede sweep meet at a held middle row, in both start
// orders: in scan order they deadlocked (admin s0 first, in-claim sweep
// s5 first). The SKIP LOCKED sweep completes around the held row without
// waiting, the replay queues behind the holder alone, and follow-up
// passes retire what the racing pass skipped, so every row ends
// superseded.
func TestAdminReplayAndClaimSweepHeldRowRaceLive(t *testing.T) {
	f := openAdminLockOrderFixture(t, true)
	setup, cancelSetup := context.WithTimeout(context.Background(), time.Minute)
	defer cancelSetup()
	f.makeStale(t, setup)
	adminReplay := func(ctx context.Context, db *sql.DB) adminLockOrderReplay {
		return startAdminReplay(ctx, db, admin.ReplayWorkItemFilter{Stage: "reducer", Limit: 100})
	}
	for _, sweepFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("sweep first %t", sweepFirst), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			f.resetStale(t, ctx)
			sweepResult, replayResult := f.raceSweepThroughHeldRow(t, ctx, f.ids[len(f.ids)/2], sweepFirst, startSweep, adminReplay)
			if sweepResult.err != nil || replayResult.err != nil {
				t.Fatalf("sweep err=%v (40P01=%t), admin replay err=%v (40P01=%t)", sweepResult.err,
					isDeadlock(sweepResult.err), replayResult.err, isDeadlock(replayResult.err))
			}
			adminSweepUntilDrained(t, ctx, f.base)
			if problem := f.supersededAccounting(t, ctx, sweepResult, replayResult); problem != "" {
				t.Errorf("accounting: %s", problem)
			}
		})
	}
}

// TestReplaysAndClaimSweepConcurrentNeverDeadlockLive is the three-way #8205
// proof: 150 rounds of a standalone supersede sweep pass, a scoped and a
// stage-only recovery replay, and an admin replay, all started at once
// over the same 18 stale rows on independent connections. It requires
// zero 40P01 and exact accounting in every round, judged after follow-up
// sweep passes retire what the racing pass skipped.
func TestReplaysAndClaimSweepConcurrentNeverDeadlockLive(t *testing.T) {
	f := openAdminLockOrderFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	f.makeStale(t, ctx)
	ops := []func(*sql.DB) adminLockOrderResult{
		func(db *sql.DB) adminLockOrderResult { return startSweep(ctx, db).wait() },
		func(db *sql.DB) adminLockOrderResult {
			return startRecoveryReplay(ctx, db, recovery.ReplayFilter{
				Stage: recovery.StageReducer, ScopeIDs: f.scopes, FailureClass: adminLockOrderClass,
			}).wait()
		},
		func(db *sql.DB) adminLockOrderResult {
			return startRecoveryReplay(ctx, db, recovery.ReplayFilter{Stage: recovery.StageReducer}).wait()
		},
		func(db *sql.DB) adminLockOrderResult {
			return startAdminReplay(ctx, db, admin.ReplayWorkItemFilter{Stage: "reducer", Limit: 100}).wait()
		},
	}
	conns := make([]*sql.DB, len(ops))
	for i := range conns {
		conns[i] = interleaveConn(t, ctx, f.dsn, f.schema, false)
	}
	const rounds = 150
	deadlocks := make([]int, len(ops))
	accountingFailures := 0
	started := time.Now()
	for round := 0; round < rounds; round++ {
		f.resetStale(t, ctx)
		var (
			wg             sync.WaitGroup
			mu             sync.Mutex
			results        []adminLockOrderResult
			roundDeadlocks int
			start          = make(chan struct{})
		)
		for i, op := range ops {
			wg.Add(1)
			go func(i int, op func(*sql.DB) adminLockOrderResult) {
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
			}(i, op)
		}
		close(start)
		wg.Wait()
		if roundDeadlocks == 0 {
			adminSweepUntilDrained(t, ctx, conns[0])
			if problem := f.supersededAccounting(t, ctx, results...); problem != "" {
				accountingFailures++
				if accountingFailures <= 5 {
					t.Logf("round %d accounting: %s", round, problem)
				}
			}
		}
	}
	total := 0
	for _, n := range deadlocks {
		total += n
	}
	t.Logf("%d rounds in %s: 40P01 sweep=%d recovery scoped=%d recovery stage-only=%d admin=%d; rounds failing accounting=%d",
		rounds, time.Since(started).Round(time.Millisecond), deadlocks[0], deadlocks[1], deadlocks[2], deadlocks[3], accountingFailures)
	if total != 0 {
		t.Errorf("standalone sweep and replays deadlocked %d times in %d rounds, want 0", total, rounds)
	}
	if accountingFailures != 0 {
		t.Errorf("%d deadlock-free rounds failed exact accounting", accountingFailures)
	}
}
