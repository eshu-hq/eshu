// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

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
// reducer claim-time supersede sweep retires the 18 target rows, and runs
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

// startClaim runs one reducer claim, and with it the supersede sweep, on db.
func startClaim(ctx context.Context, db *sql.DB) adminLockOrderReplay {
	r := adminLockOrderReplay{done: make(chan adminLockOrderResult, 1)}
	go func() {
		_, _, err := interleaveQueue(db, reducer.DomainWorkloadIdentity).Claim(ctx)
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
// the claim-time supersede sweep on two sessions with a 150 ms per-row
// trigger sleep, in both start orders (#8205). On main the admin UPDATE locked
// s0 first and the sweep s5 first, so the pair formed a lock cycle; with both
// on work_item_id COLLATE "C" the second waits for the first one way, both
// succeed, and every row ends superseded.
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
			first, second := pidReplay, pidSweep
			var sweep, replay adminLockOrderReplay
			if sweepFirst {
				first, second = pidSweep, pidReplay
				sweep = startClaim(ctx, sweeper)
				f.waitHoldsRowLock(t, ctx, pidSweep)
				replay = startAdminReplay(ctx, replayer, stageOnly)
			} else {
				replay = startAdminReplay(ctx, replayer, stageOnly)
				f.waitHoldsRowLock(t, ctx, pidReplay)
				sweep = startClaim(ctx, sweeper)
			}
			blockers := f.waitBlocked(t, ctx, second)
			for sample := 0; sample < 3; sample++ {
				if firstBlockers := f.blockingPIDs(t, ctx, first); len(firstBlockers) != 0 {
					t.Errorf("sample %d: first statement (pid %d) blocked by %v while second (pid %d) blocked by %v",
						sample, first, firstBlockers, second, f.blockingPIDs(t, ctx, second))
				}
				time.Sleep(100 * time.Millisecond)
			}
			sweepResult, replayResult := sweep.wait(), replay.wait()
			if sweepResult.err != nil || replayResult.err != nil {
				t.Fatalf("sweep err=%v (40P01=%t), admin replay err=%v (40P01=%t)", sweepResult.err,
					isDeadlock(sweepResult.err), replayResult.err, isDeadlock(replayResult.err))
			}
			if problem := f.supersededAccounting(t, ctx, replayResult); problem != "" {
				t.Errorf("accounting: %s", problem)
			}
			t.Logf("second statement waited on %v; admin replay reported %d rows", blockers, len(replayResult.ids))
		})
	}
}

// TestAdminReplayAndClaimSweepHeldRowRaceLive makes the admin replay and the
// claim-time supersede sweep meet at a held middle row (raceThroughHeldRow),
// in both start orders: in scan order they deadlocked (admin s0 first, sweep
// s5 first); in one global order both succeed and every row ends superseded.
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
			first, second := adminReplay, startClaim
			if sweepFirst {
				first, second = startClaim, adminReplay
			}
			a, b := f.raceThroughHeldRow(t, ctx, f.ids[len(f.ids)/2], first, second)
			if a.err != nil || b.err != nil {
				t.Fatalf("first err=%v (40P01=%t), second err=%v (40P01=%t)", a.err, isDeadlock(a.err), b.err, isDeadlock(b.err))
			}
			if problem := f.supersededAccounting(t, ctx, a, b); problem != "" {
				t.Errorf("accounting: %s", problem)
			}
		})
	}
}

// TestReplaysAndClaimSweepConcurrentNeverDeadlockLive is the three-way #8205
// proof: 150 rounds of a reducer claim (with its supersede sweep), a scoped
// and a stage-only recovery replay, and an admin replay, all started at once
// over the same 18 stale rows on independent connections. It requires zero
// 40P01 and exact accounting in every round.
func TestReplaysAndClaimSweepConcurrentNeverDeadlockLive(t *testing.T) {
	f := openAdminLockOrderFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	f.makeStale(t, ctx)
	ops := []func(*sql.DB) adminLockOrderResult{
		func(db *sql.DB) adminLockOrderResult { return startClaim(ctx, db).wait() },
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
	t.Logf("%d rounds in %s: 40P01 claim=%d recovery scoped=%d recovery stage-only=%d admin=%d; rounds failing accounting=%d",
		rounds, time.Since(started).Round(time.Millisecond), deadlocks[0], deadlocks[1], deadlocks[2], deadlocks[3], accountingFailures)
	if total != 0 {
		t.Errorf("claim sweep and replays deadlocked %d times in %d rounds, want 0", total, rounds)
	}
	if accountingFailures != 0 {
		t.Errorf("%d deadlock-free rounds failed exact accounting", accountingFailures)
	}
}
