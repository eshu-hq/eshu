// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8261 admin repository skip (SkipRepositoryWorkItems) lock order against the recovery replay, the admin replay and the reducer claim sweep over failed rows of a superseded generation: the held-middle-row race completes in both start orders (main: 6 of 6 40P01), 150 rounds of all four with a 2 ms per-row trigger sleep and random start delays give 0 x 40P01 (main: 98 x 40P01), and the EvalPlanQual case drops a row claimed or replayed before the skip's lock step; isolated schemas; about 100-180 s locally including two 400,000-row seeds; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/admin/store"
	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// skipLockOrderIDs are the skipped scope's rows in work_item_id COLLATE "C"
// order. updated_at runs forward with that order, so the skip's selection
// (newest updated_at first) visits them backwards.
var skipLockOrderIDs = []string{"sk-w0", "sk-w1", "sk-w2"}

// startAdminLocker runs fn on its own goroutine as an adminLockOrderReplay.
func startAdminLocker(ctx context.Context, db *sql.DB, fn func(context.Context, *sql.DB) ([]string, error)) adminLockOrderReplay {
	r := adminLockOrderReplay{done: make(chan adminLockOrderResult, 1)}
	go func() {
		ids, err := fn(ctx, db)
		r.done <- adminLockOrderResult{ids: ids, err: err}
	}()
	return r
}

// skipLocker runs the production admin repository skip for scope sk.
func skipLocker(ctx context.Context, db *sql.DB) ([]string, error) {
	items, err := store.NewStore(db).SkipRepositoryWorkItems(ctx, "sk", "skip lock-order repro")
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.WorkItemID)
	}
	return ids, err
}

// TestSkipRepositoryHeldRowRaceLive races the admin repository skip
// (SkipRepositoryWorkItems) against the ordered lockers that hold the same
// rows (#8261): the recovery and admin replays on failed rows, and the
// reducer claim sweep on an older generation's failed reducer rows. The skip
// locks in its selection order (updated_at DESC); the others lock in
// work_item_id COLLATE "C" order. Each pair runs in both start orders
// through the held middle row, and both statements must succeed.
func TestSkipRepositoryHeldRowRaceLive(t *testing.T) {
	f := openAdminLockOrderFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	f.seedSkipScope(t, ctx)

	for _, other := range skipOtherLockers() {
		for _, skipFirst := range []bool{true, false} {
			name := other.name + "/skip second"
			a, b, nameA, nameB := other.locker, skipLocker, other.name, "skip"
			if skipFirst {
				name = other.name + "/skip first"
				a, b, nameA, nameB = skipLocker, other.locker, "skip", other.name
			}
			t.Run(name, func(t *testing.T) {
				f.seedSkipRows(t, ctx, skipLockOrderIDs)
				resultA, resultB := f.raceThroughHeldRow(t, ctx, skipLockOrderIDs[1],
					func(ctx context.Context, db *sql.DB) adminLockOrderReplay { return startAdminLocker(ctx, db, a) },
					func(ctx context.Context, db *sql.DB) adminLockOrderReplay { return startAdminLocker(ctx, db, b) })
				verdict := "one-way"
				for label, result := range map[string]adminLockOrderResult{nameA: resultA, nameB: resultB} {
					if result.err != nil {
						verdict = "error"
						if isDeadlock(result.err) {
							verdict = "40P01"
						}
						t.Errorf("%s: %v (40P01=%t)", label, result.err, isDeadlock(result.err))
					}
				}
				t.Logf("%s then %s through %s: %s (%s reported %v, %s reported %v)",
					nameA, nameB, skipLockOrderIDs[1], verdict, nameA, resultA.ids, nameB, resultB.ids)
			})
		}
	}
}

// seedSkipScope inserts scope sk, its superseded generation sk-g1 (whose
// rows the claim sweep retires) and its active generation sk-g2.
func (f *adminLockOrderFixture) seedSkipScope(t *testing.T, ctx context.Context) {
	t.Helper()
	interleaveExec(t, ctx, f.base, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ('sk', 'repository', 'git', 'sk', NULL, 'git', 'sk', $1, $1, 'active', 'sk-g2', '{}'::jsonb)`, f.at)
	interleaveExec(t, ctx, f.base, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload)
VALUES ('sk-g1', 'sk', 'snapshot', 'x', $1::timestamptz, $1::timestamptz - interval '1 hour', 'superseded', NULL, $1::timestamptz, '{}'::jsonb),
       ('sk-g2', 'sk', 'snapshot', 'x', $1, $1, 'active', $1, NULL, '{}'::jsonb)`, f.at)
}

// skipOtherLockers are the ordered lockers that can hold the skipped rows.
func skipOtherLockers() []struct {
	name   string
	locker func(context.Context, *sql.DB) ([]string, error)
} {
	return []struct {
		name   string
		locker func(context.Context, *sql.DB) ([]string, error)
	}{
		{name: "recovery replay", locker: func(ctx context.Context, db *sql.DB) ([]string, error) {
			r := startRecoveryReplay(ctx, db, recovery.ReplayFilter{Stage: recovery.StageReducer, ScopeIDs: []string{"sk"}}).wait()
			return r.ids, r.err
		}},
		{name: "admin replay", locker: func(ctx context.Context, db *sql.DB) ([]string, error) {
			r := startAdminReplay(ctx, db, admin.ReplayWorkItemFilter{ScopeID: "sk", Stage: "reducer"}).wait()
			return r.ids, r.err
		}},
		{name: "claim sweep", locker: func(ctx context.Context, db *sql.DB) ([]string, error) {
			r := startClaim(ctx, db).wait()
			return r.ids, r.err
		}},
	}
}

// seedSkipRows (re)creates one failed reducer row of the superseded
// generation sk-g1 per id, updated_at running forward with the id order.
func (f *adminLockOrderFixture) seedSkipRows(t *testing.T, ctx context.Context, ids []string) {
	t.Helper()
	interleaveExec(t, ctx, f.base, `DELETE FROM fact_work_items WHERE scope_id = 'sk'`)
	for i, id := range ids {
		interleaveExec(t, ctx, f.base, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, failure_class, payload, created_at, updated_at, visible_at)
VALUES ($1, 'sk', 'sk-g1', 'reducer', 'workload_identity', 'scope', $1, 'failed', 1, 'transient',
    jsonb_build_object('source_system', 'git'), $2, $2, $2)`, id, f.at.Add(time.Duration(i)*time.Second))
	}
	interleaveExec(t, ctx, f.base, `ANALYZE fact_work_items`)
}

// skipStatuses reads the status of each skipped-scope row.
func (f *adminLockOrderFixture) skipStatuses(t *testing.T, ctx context.Context) map[string]string {
	t.Helper()
	rows, err := f.base.QueryContext(ctx, `SELECT work_item_id, status FROM fact_work_items WHERE scope_id = 'sk'`)
	if err != nil {
		t.Fatalf("read skip statuses: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatalf("scan skip status: %v", err)
		}
		out[id] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate skip statuses: %v", err)
	}
	return out
}

// TestSkipRepositoryDropsRowChangedBeforeItsLockLive is the EvalPlanQual
// case of the skip's ordered lock step (#8261). Session X changes the first
// row in lock order and holds it uncommitted; the skip selects it as failed
// and waits on it. X then commits a claim, or a replay to pending. The skip
// must drop the row and dead-letter only the other two.
func TestSkipRepositoryDropsRowChangedBeforeItsLockLive(t *testing.T) {
	f := openAdminLockOrderFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	f.seedSkipScope(t, ctx)
	for _, tc := range []struct{ name, status, leaseOwner string }{
		{name: "claimed", status: "claimed", leaseOwner: "epq-worker"},
		{name: "replayed", status: "pending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.seedSkipRows(t, ctx, skipLockOrderIDs)
			target := skipLockOrderIDs[0]
			tx, err := interleaveConn(t, ctx, f.dsn, f.schema, false).BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin X: %v", err)
			}
			defer func() { _ = tx.Rollback() }()
			if _, err := tx.ExecContext(ctx, `UPDATE fact_work_items SET status = $2, lease_owner = NULLIF($3, '') WHERE work_item_id = $1`,
				target, tc.status, tc.leaseOwner); err != nil {
				t.Fatalf("X change: %v", err)
			}
			y := interleaveConn(t, ctx, f.dsn, f.schema, false)
			var pidY int
			if err := y.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pidY); err != nil {
				t.Fatalf("pid Y: %v", err)
			}
			run := startAdminLocker(ctx, y, skipLocker)
			f.waitBlocked(t, ctx, pidY)
			if err := tx.Commit(); err != nil {
				t.Fatalf("X commit: %v", err)
			}
			result := run.wait()
			if result.err != nil {
				t.Fatalf("skip: %v", result.err)
			}
			if got := strings.Join(result.ids, ","); got != "sk-w1,sk-w2" {
				t.Errorf("skip reported %q, want sk-w1,sk-w2", got)
			}
			want := map[string]string{target: tc.status, "sk-w1": "dead_letter", "sk-w2": "dead_letter"}
			if got := f.skipStatuses(t, ctx); fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("statuses %v, want %v", got, want)
			}
		})
	}
}

// TestSkipRepositoryConcurrentWithReplaysAndSweepNeverDeadlockLive runs the
// skip, a recovery replay, an admin replay and a reducer claim concurrently
// over 18 failed rows for 150 rounds (#8261), each after a random start
// delay, with a 2 ms per-row trigger sleep. No round may deadlock or leave a
// row outside the states those statements produce.
func TestSkipRepositoryConcurrentWithReplaysAndSweepNeverDeadlockLive(t *testing.T) {
	f := openAdminLockOrderFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	f.seedSkipScope(t, ctx)
	ids := make([]string, 0, 18)
	for i := 0; i < 18; i++ {
		ids = append(ids, fmt.Sprintf("sk-w%02d", i))
	}
	type namedLocker struct {
		name   string
		locker func(context.Context, *sql.DB) ([]string, error)
	}
	lockers := []namedLocker{{name: "skip", locker: skipLocker}}
	for _, other := range skipOtherLockers() {
		lockers = append(lockers, namedLocker{name: other.name, locker: other.locker})
	}
	conns := make([]*sql.DB, len(lockers))
	for i := range lockers {
		conns[i] = interleaveConn(t, ctx, f.dsn, f.schema, false)
		interleaveExec(t, ctx, conns[i], "SET "+adminLockOrderSleepGUC+" = '2'")
	}
	deadlocks := make([]int, len(lockers))
	badRounds := 0
	started := time.Now()
	const rounds = 150
	for round := 0; round < rounds; round++ {
		f.seedSkipRows(t, ctx, ids)
		errs := make([]error, len(lockers))
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i, l := range lockers {
			wg.Add(1)
			go func(i int, l namedLocker) {
				defer wg.Done()
				<-start
				time.Sleep(time.Duration(rand.IntN(160)) * time.Millisecond)
				_, errs[i] = l.locker(ctx, conns[i])
			}(i, l)
		}
		close(start)
		wg.Wait()
		for i, err := range errs {
			switch {
			case isDeadlock(err):
				deadlocks[i]++
			case err != nil:
				t.Errorf("round %d %s: %v", round, lockers[i].name, err)
			}
		}
		for id, status := range f.skipStatuses(t, ctx) {
			if status != "dead_letter" && status != "pending" && status != "superseded" && status != "failed" {
				badRounds++
				t.Logf("round %d: %s ended %q", round, id, status)
				break
			}
		}
	}
	parts := make([]string, len(lockers))
	total := 0
	for i, l := range lockers {
		parts[i] = fmt.Sprintf("%s=%d", l.name, deadlocks[i])
		total += deadlocks[i]
	}
	t.Logf("%d rounds in %s: 40P01 %s; rounds with a row in an unexpected state=%d",
		rounds, time.Since(started).Round(time.Millisecond), strings.Join(parts, " "), badRounds)
	if total != 0 || badRounds != 0 {
		t.Errorf("deadlocks=%d, bad rounds=%d; want 0 and 0", total, badRounds)
	}
}
