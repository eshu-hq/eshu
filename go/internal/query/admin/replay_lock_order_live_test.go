// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// TestAdminReplayLockOrderBlocksOneWayLive drives the #8205 interleave for
// the admin replay deterministically: a test-only BEFORE UPDATE trigger sleeps
// 150 ms per row, replay A starts and locks its first row, then replay B
// starts. Before the fix the admin UPDATE locked rows in its selection order
// (updated_at DESC: s5 first) while the scoped recovery replay locked s0
// first, so the pair met in the middle, each blocked by the other, and one
// died with 40P01. With every replay locking in work_item_id COLLATE "C"
// order, B waits for A's first row, A waits for nobody, both succeed, and
// every row is replayed exactly once. The admin-versus-admin pair shares one
// selection order on main as well; it pins that two admin replays stay a
// one-way wait under the new lock step.
func TestAdminReplayLockOrderBlocksOneWayLive(t *testing.T) {
	f := openAdminLockOrderFixture(t, true)
	stageOnly := admin.ReplayWorkItemFilter{Stage: "reducer", Limit: 100}
	for _, tc := range []struct {
		name string
		a, b func(context.Context, *sql.DB) adminLockOrderReplay
	}{
		{
			name: "recovery scoped then admin stage-only",
			a: func(ctx context.Context, db *sql.DB) adminLockOrderReplay {
				return startRecoveryReplay(ctx, db, recovery.ReplayFilter{
					Stage: recovery.StageReducer, ScopeIDs: f.scopes, FailureClass: adminLockOrderClass,
				})
			},
			b: func(ctx context.Context, db *sql.DB) adminLockOrderReplay {
				return startAdminReplay(ctx, db, stageOnly)
			},
		},
		{
			name: "admin stage-only then recovery scoped",
			a: func(ctx context.Context, db *sql.DB) adminLockOrderReplay {
				return startAdminReplay(ctx, db, stageOnly)
			},
			b: func(ctx context.Context, db *sql.DB) adminLockOrderReplay {
				return startRecoveryReplay(ctx, db, recovery.ReplayFilter{
					Stage: recovery.StageReducer, ScopeIDs: f.scopes, FailureClass: adminLockOrderClass,
				})
			},
		},
		{
			name: "admin id list then admin stage-only",
			a: func(ctx context.Context, db *sql.DB) adminLockOrderReplay {
				return startAdminReplay(ctx, db, admin.ReplayWorkItemFilter{WorkItemIDs: f.ids, Stage: "reducer", Limit: 100})
			},
			b: func(ctx context.Context, db *sql.DB) adminLockOrderReplay {
				return startAdminReplay(ctx, db, stageOnly)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			f.reset(t, ctx)
			sessionA, pidA := f.sleepingSession(t, ctx)
			sessionB, pidB := f.sleepingSession(t, ctx)

			a := tc.a(ctx, sessionA)
			f.waitHoldsRowLock(t, ctx, pidA)
			b := tc.b(ctx, sessionB)
			blockersOfB := f.waitBlocked(t, ctx, pidB)
			for sample := 0; sample < 3; sample++ {
				blockersOfA := f.blockingPIDs(t, ctx, pidA)
				blockersOfB = f.blockingPIDs(t, ctx, pidB)
				if len(blockersOfA) != 0 {
					t.Errorf("sample %d: A (pid %d) blocked by %v while B (pid %d) blocked by %v; want a one-way wait",
						sample, pidA, blockersOfA, pidB, blockersOfB)
				}
				if len(blockersOfB) > 0 && !slices.Equal(blockersOfB, []int{pidA}) {
					t.Errorf("sample %d: B blocked by %v, want only A (%d)", sample, blockersOfB, pidA)
				}
				time.Sleep(100 * time.Millisecond)
			}
			resultA, resultB := a.wait(), b.wait()
			if resultA.err != nil || resultB.err != nil {
				t.Fatalf("replay errors: A=%v B=%v", resultA.err, resultB.err)
			}
			if problem := f.accounting(t, ctx, resultA, resultB); problem != "" {
				t.Errorf("accounting: %s", problem)
			}
			t.Logf("A replayed %d rows, B replayed %d rows; B was blocked by %v (A=%d)",
				len(resultA.ids), len(resultB.ids), blockersOfB, pidA)
		})
	}
}

// TestAdminReplayHeldRowRaceLive makes the admin replay and the scoped
// recovery replay meet at a held middle row (raceThroughHeldRow), in both
// start orders, so the outcome depends only on lock order: on main the admin
// UPDATE (s5 first) and the scoped replay (s0 first) deadlocked; in one global
// order both succeed and every row is replayed exactly once.
func TestAdminReplayHeldRowRaceLive(t *testing.T) {
	f := openAdminLockOrderFixture(t, true)
	adminReplay := func(ctx context.Context, db *sql.DB) adminLockOrderReplay {
		return startAdminReplay(ctx, db, admin.ReplayWorkItemFilter{Stage: "reducer", Limit: 100})
	}
	scoped := func(ctx context.Context, db *sql.DB) adminLockOrderReplay {
		return startRecoveryReplay(ctx, db, recovery.ReplayFilter{
			Stage: recovery.StageReducer, ScopeIDs: f.scopes, FailureClass: adminLockOrderClass,
		})
	}
	for _, adminFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("admin first %t", adminFirst), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			f.reset(t, ctx)
			first, second := scoped, adminReplay
			if adminFirst {
				first, second = adminReplay, scoped
			}
			a, b := f.raceThroughHeldRow(t, ctx, f.ids[len(f.ids)/2], first, second)
			if a.err != nil || b.err != nil {
				t.Fatalf("first err=%v (40P01=%t), second err=%v (40P01=%t)", a.err, isDeadlock(a.err), b.err, isDeadlock(b.err))
			}
			if problem := f.accounting(t, ctx, a, b); problem != "" {
				t.Errorf("accounting: %s", problem)
			}
		})
	}
}

// TestAdminReplayDropsRowClaimedBeforeItsLockLive is the EvalPlanQual case
// of the admin lock step (#8205). Session X claims the first target row in
// lock order and holds it uncommitted; the admin replay Y selects it as
// dead_letter, waits on it, and must drop it once X commits, leaving X's
// claim intact and replaying only the other 17 rows.
func TestAdminReplayDropsRowClaimedBeforeItsLockLive(t *testing.T) {
	f := openAdminLockOrderFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	f.reset(t, ctx)
	target := f.ids[0]
	sentinel := f.at.Add(-24 * time.Hour)

	sessionX := interleaveConn(t, ctx, f.dsn, f.schema, false)
	tx, err := sessionX.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin X: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
UPDATE fact_work_items SET status = 'claimed', lease_owner = 'epq-worker', updated_at = $2
WHERE work_item_id = $1`, target, sentinel); err != nil {
		t.Fatalf("X claim: %v", err)
	}
	var pidX int
	if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pidX); err != nil {
		t.Fatalf("X pid: %v", err)
	}
	sessionY := interleaveConn(t, ctx, f.dsn, f.schema, false)
	var pidY int
	if err := sessionY.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pidY); err != nil {
		t.Fatalf("Y pid: %v", err)
	}
	y := startAdminReplay(ctx, sessionY, admin.ReplayWorkItemFilter{Stage: "reducer", Limit: 100})
	if blockers := f.waitBlocked(t, ctx, pidY); !slices.Equal(blockers, []int{pidX}) {
		t.Fatalf("admin replay blocked by %v, want X (%d)", blockers, pidX)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit X: %v", err)
	}
	result := y.wait()
	if result.err != nil {
		t.Fatalf("admin replay: %v", result.err)
	}
	if slices.Contains(result.ids, target) || len(result.ids) != adminLockOrderRows-1 {
		t.Errorf("admin replay reported %d rows (target included: %t), want %d without %s",
			len(result.ids), slices.Contains(result.ids, target), adminLockOrderRows-1, target)
	}
	var status, owner string
	var updatedAt time.Time
	if err := f.base.QueryRowContext(ctx, `
SELECT status, COALESCE(lease_owner, ''), updated_at FROM fact_work_items WHERE work_item_id = $1`,
		target).Scan(&status, &owner, &updatedAt); err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	if status != "claimed" || owner != "epq-worker" || !updatedAt.Equal(sentinel) {
		t.Errorf("%s = (%s, %q, %s), want X's claim (claimed, epq-worker, %s)", target, status, owner, updatedAt, sentinel)
	}
}
