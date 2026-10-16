// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// lockOrderSleepMS is the per-row trigger sleep of the two-session proofs.
// Eighteen rows at 150 ms hold the first replay's locks for about 2.7 s.
const lockOrderSleepMS = "150"

// TestRecoveryReplayTwoSessionsBlockOneWayLive drives the #8205 interleave
// deterministically. A test-only BEFORE UPDATE trigger sleeps per row, so the
// scoped replay A holds its row locks for seconds; the stage-only replay B
// starts once A holds a lock. Before the fix A locked s0 to s5 and B s5 to
// s0, so they met in the middle, each blocked by the other (the 40P01
// cycle). With the shared work_item_id order B waits for A's first row and A
// waits for nobody; both statements succeed and every row is replayed once.
func TestRecoveryReplayTwoSessionsBlockOneWayLive(t *testing.T) {
	f := openLockOrderFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f.installSleepTrigger(t, ctx)
	f.reset(t, ctx)

	sessionA, sessionB := f.sleepingSession(t, ctx), f.sleepingSession(t, ctx)
	pidA, pidB := lockOrderBackendPID(t, ctx, sessionA), lockOrderBackendPID(t, ctx, sessionB)

	a := startLockOrderReplay(ctx, sessionA, f.scopedFilter())
	waitHoldsRowLock(t, ctx, f.base, pidA)
	b := startLockOrderReplay(ctx, sessionB, stageOnlyFilter())
	blockersOfB := waitBlocked(t, ctx, f.base, pidB)

	// Sample the wait graph while A still sleeps through its rows.
	for sample := 0; sample < 3; sample++ {
		blockersOfA := lockOrderBlockingPIDs(t, ctx, f.base, pidA)
		blockersOfB = lockOrderBlockingPIDs(t, ctx, f.base, pidB)
		if len(blockersOfA) != 0 {
			t.Errorf("sample %d: replay A (pid %d) is blocked by %v while B (pid %d) is blocked by %v; "+
				"want a one-way wait", sample, pidA, blockersOfA, pidB, blockersOfB)
		}
		if len(blockersOfB) > 0 && !slices.Equal(blockersOfB, []int{pidA}) {
			t.Errorf("sample %d: replay B blocked by %v, want only A (%d)", sample, blockersOfB, pidA)
		}
		time.Sleep(100 * time.Millisecond)
	}

	resultA, resultB := a.wait(), b.wait()
	for name, result := range map[string]lockOrderReplayResult{"A scoped": resultA, "B stage-only": resultB} {
		if result.err != nil {
			t.Errorf("replay %s: %v (40P01=%t)", name, result.err, isDeadlock(result.err))
		}
	}
	counted := map[string]int{}
	for _, id := range append(slices.Clone(resultA.ids), resultB.ids...) {
		counted[id]++
	}
	if problem := lockOrderAccounting(f, counted, f.statusCounts(t, ctx)); problem != "" {
		t.Errorf("accounting: %s", problem)
	}
	t.Logf("A replayed %d rows, B replayed %d rows; B was blocked by %v", len(resultA.ids), len(resultB.ids), blockersOfB)
}

// TestRecoveryReplayHeldRowRaceLive makes the scoped and the stage-only
// replay meet at a held middle row (raceThroughHeldRow), so the result depends
// only on their lock order: on main each held rows on both sides of it and one
// died with 40P01; in one global order both succeed and every row is replayed
// exactly once.
func TestRecoveryReplayHeldRowRaceLive(t *testing.T) {
	f := openLockOrderFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	f.reset(t, ctx)
	a, b := f.raceThroughHeldRow(t, ctx, f.ids[len(f.ids)/2],
		func(ctx context.Context, db *sql.DB) lockOrderReplay {
			return startLockOrderReplay(ctx, db, f.scopedFilter())
		},
		func(ctx context.Context, db *sql.DB) lockOrderReplay {
			return startLockOrderReplay(ctx, db, stageOnlyFilter())
		})
	if a.err != nil || b.err != nil {
		t.Fatalf("scoped err=%v (40P01=%t), stage-only err=%v (40P01=%t)", a.err, isDeadlock(a.err), b.err, isDeadlock(b.err))
	}
	counted := map[string]int{}
	for _, id := range append(slices.Clone(a.ids), b.ids...) {
		counted[id]++
	}
	if problem := lockOrderAccounting(f, counted, f.statusCounts(t, ctx)); problem != "" {
		t.Errorf("accounting: %s", problem)
	}
}

// TestRecoveryReplayDropsRowChangedBeforeItsLockLive is the EvalPlanQual case
// of the ordered lock (#8205). Session X changes the first target row in
// lock order and holds it uncommitted; the scoped replay Y takes its snapshot
// while the row still reads dead_letter and waits on it. X commits the row as
// pending (a concurrent replay) or claimed (a worker claim after one). Y must
// drop the row: its lock recheck and its UPDATE qual both re-read status, so
// the row keeps X's values and Y replays only the other 17 rows.
func TestRecoveryReplayDropsRowChangedBeforeItsLockLive(t *testing.T) {
	f := openLockOrderFixture(t, false)
	for _, tc := range []struct {
		name, status, leaseOwner string
	}{
		{name: "made pending", status: "pending"},
		{name: "claimed", status: "claimed", leaseOwner: "epq-worker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			f.reset(t, ctx)
			target := f.ids[0]
			sentinel := f.at.Add(-24 * time.Hour)

			sessionX, sessionY := f.session(t), f.session(t)
			tx, err := sessionX.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin X: %v", err)
			}
			defer func() { _ = tx.Rollback() }()
			if _, err := tx.ExecContext(ctx, `
UPDATE fact_work_items
SET status = $2, lease_owner = NULLIF($3, ''), visible_at = $4, updated_at = $4
WHERE work_item_id = $1`, target, tc.status, tc.leaseOwner, sentinel); err != nil {
				t.Fatalf("X update: %v", err)
			}
			var pidX int
			if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pidX); err != nil {
				t.Fatalf("X pid: %v", err)
			}

			pidY := lockOrderBackendPID(t, ctx, sessionY)
			y := startLockOrderReplay(ctx, sessionY, f.scopedFilter())
			if blockers := waitBlocked(t, ctx, f.base, pidY); !slices.Equal(blockers, []int{pidX}) {
				t.Fatalf("replay Y blocked by %v, want X (%d)", blockers, pidX)
			}
			if err := tx.Commit(); err != nil {
				t.Fatalf("commit X: %v", err)
			}
			result := y.wait()
			if result.err != nil {
				t.Fatalf("replay Y: %v", result.err)
			}
			if slices.Contains(result.ids, target) {
				t.Errorf("replay Y reported %s, which X changed to %s before Y could lock it", target, tc.status)
			}
			if got, want := len(result.ids), lockOrderTargetRows-1; got != want {
				t.Errorf("replay Y replayed %d rows, want %d", got, want)
			}
			var status string
			var leaseOwner sql.NullString
			var visibleAt, updatedAt time.Time
			if err := f.base.QueryRowContext(ctx, `
SELECT status, lease_owner, visible_at, updated_at FROM fact_work_items WHERE work_item_id = $1`,
				target).Scan(&status, &leaseOwner, &visibleAt, &updatedAt); err != nil {
				t.Fatalf("read %s: %v", target, err)
			}
			if status != tc.status || leaseOwner.String != tc.leaseOwner ||
				!visibleAt.Equal(sentinel) || !updatedAt.Equal(sentinel) {
				t.Errorf("%s = (%s, %q, visible %s, updated %s), want X's (%s, %q, %s): the replay reset it twice",
					target, status, leaseOwner.String, visibleAt, updatedAt, tc.status, tc.leaseOwner, sentinel)
			}
		})
	}
}

// lockOrderReplayResult is one finished replay.
type lockOrderReplayResult struct {
	ids []string
	err error
}

// lockOrderReplay is a replay running on its own goroutine.
type lockOrderReplay struct {
	done chan lockOrderReplayResult
}

// wait returns the replay's result.
func (r lockOrderReplay) wait() lockOrderReplayResult { return <-r.done }

// startLockOrderReplay runs the production replay for filter on db.
func startLockOrderReplay(ctx context.Context, db *sql.DB, filter recovery.ReplayFilter) lockOrderReplay {
	r := lockOrderReplay{done: make(chan lockOrderReplayResult, 1)}
	go func() {
		result, err := NewRecoveryStore(SQLDB{DB: db}).ReplayFailedWorkItems(ctx, filter, time.Now())
		r.done <- lockOrderReplayResult{ids: result.WorkItemIDs, err: err}
	}()
	return r
}

// sleepingSession opens a session whose row updates sleep in the test
// trigger. It widens deadlock_timeout so a cycle on unfixed SQL stays visible
// to pg_blocking_pids instead of being broken after the 1 s default; a role
// that may not set it keeps the default and the outcome assertions still hold.
func (f *lockOrderFixture) sleepingSession(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	db := f.session(t)
	lockOrderExec(t, ctx, db, "SET "+lockOrderSleepGUC+" = '"+lockOrderSleepMS+"'")
	if _, err := db.ExecContext(ctx, "SET deadlock_timeout = '10s'"); err != nil {
		t.Logf("deadlock_timeout stays at its default: %v", err)
	}
	return db
}

// lockOrderBackendPID returns the backend pid of a single-connection handle.
func lockOrderBackendPID(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var pid int
	if err := db.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("backend pid: %v", err)
	}
	return pid
}

// lockOrderBlockingPIDs returns pg_blocking_pids(pid), sorted.
func lockOrderBlockingPIDs(t *testing.T, ctx context.Context, db *sql.DB, pid int) []int {
	t.Helper()
	var raw []int64
	rows, err := db.QueryContext(ctx, `SELECT unnest(pg_blocking_pids($1))`, pid)
	if err != nil {
		t.Fatalf("pg_blocking_pids(%d): %v", pid, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var blocker int64
		if err := rows.Scan(&blocker); err != nil {
			t.Fatalf("scan blocker: %v", err)
		}
		raw = append(raw, blocker)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate blockers: %v", err)
	}
	out := make([]int, 0, len(raw))
	for _, blocker := range raw {
		out = append(out, int(blocker))
	}
	slices.Sort(out)
	return out
}

// waitBlocked polls until pid waits on a lock and returns its blockers.
func waitBlocked(t *testing.T, ctx context.Context, db *sql.DB, pid int) []int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if blockers := lockOrderBlockingPIDs(t, ctx, db, pid); len(blockers) > 0 {
			return blockers
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pid %d never waited on a lock", pid)
	return nil
}

// waitHoldsRowLock polls until pid holds a transaction id, which a backend
// gets when it locks or updates its first row.
func waitHoldsRowLock(t *testing.T, ctx context.Context, db *sql.DB, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var held bool
		if err := db.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid = $1 AND locktype = 'transactionid' AND granted)`,
			pid).Scan(&held); err != nil {
			t.Fatalf("pg_locks(%d): %v", pid, err)
		}
		if held {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pid %d never locked a row", pid)
}
