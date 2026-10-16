// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/admin/store"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Fixture for the #8205 admin replay lock-order proofs, the admin twin of
// go/internal/storage/postgres/recovery_replay_lock_order_live_helpers_test.go
// (an external test package cannot share it). Six scopes hold three
// dead-lettered reducer rows each, and 400,000 background rows plus ANALYZE
// give the planner production-shaped statistics. A scoped recovery replay
// scans by scope and visits s0 first. The admin replay's selection orders by
// updated_at DESC, and updated_at runs forward with the scope order, so on
// main the admin UPDATE visited s5 first: the opposite order.
const (
	adminLockOrderScopes   = 6
	adminLockOrderPerScope = 3
	adminLockOrderRows     = adminLockOrderScopes * adminLockOrderPerScope
	adminLockOrderClass    = "retry_exhausted"
	adminLockOrderSleepGUC = "eshu.replay_lock_order_sleep_ms"
	adminLockOrderSleepMS  = "150"
)

// adminLockOrderFixture is one seeded schema.
type adminLockOrderFixture struct {
	dsn, schema string
	base        *sql.DB
	scopes, ids []string
	at          time.Time
}

// openAdminLockOrderFixture bootstraps a schema, seeds the target rows (plus
// the background rows when background is set), installs the per-row sleep
// trigger and runs ANALYZE. It skips without a proof DSN.
func openAdminLockOrderFixture(t *testing.T, background bool) *adminLockOrderFixture {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_REDUCER_FAIRNESS_PROOF_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8205 admin replay lock-order proofs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	f := &adminLockOrderFixture{
		dsn: dsn, schema: fmt.Sprintf("replay_lock_order_%d", time.Now().UnixNano()),
		at: time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
	}
	f.base = interleaveConn(t, ctx, dsn, f.schema, true)
	if err := postgres.ApplyBootstrapWithoutContentSearchIndexes(ctx, postgres.SQLDB{DB: f.base}); err != nil {
		t.Fatalf("bootstrap schema: %v", err)
	}
	started := time.Now()
	if background {
		f.seedBackground(t, ctx)
	}
	for s := 0; s < adminLockOrderScopes; s++ {
		scope := fmt.Sprintf("lo-s%d", s)
		f.scopes = append(f.scopes, scope)
		interleaveExec(t, ctx, f.base, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, NULL, 'git', $1, $2, $2, 'active', $1 || '-g', '{}'::jsonb)`, scope, f.at)
		interleaveExec(t, ctx, f.base, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload)
VALUES ($1 || '-g', $1, 'snapshot', 'x', $2, $2, 'active', $2, NULL, '{}'::jsonb)`, scope, f.at)
		for r := 0; r < adminLockOrderPerScope; r++ {
			id := fmt.Sprintf("%s-w%d", scope, r)
			f.ids = append(f.ids, id)
			interleaveExec(t, ctx, f.base, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, failure_class, payload, created_at, updated_at)
VALUES ($1, $2, $2 || '-g', 'reducer', 'workload_identity', 'scope', $1, 'dead_letter', 3, $3,
    jsonb_build_object('source_system', 'git'), $4, $4)`, id, scope, adminLockOrderClass, f.at)
		}
	}
	f.reset(t, ctx)
	interleaveExec(t, ctx, f.base, `ANALYZE fact_work_items`)
	interleaveExec(t, ctx, f.base, `ANALYZE ingestion_scopes`)
	interleaveExec(t, ctx, f.base, `ANALYZE scope_generations`)
	interleaveExec(t, ctx, f.base, `
CREATE FUNCTION replay_lock_order_sleep() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
    IF COALESCE(current_setting('`+adminLockOrderSleepGUC+`', true), '') <> '' THEN
        PERFORM pg_sleep(current_setting('`+adminLockOrderSleepGUC+`')::int / 1000.0);
    END IF;
    RETURN NEW;
END
$fn$`)
	interleaveExec(t, ctx, f.base, `
CREATE TRIGGER zz_replay_lock_order_sleep BEFORE UPDATE ON fact_work_items
FOR EACH ROW EXECUTE FUNCTION replay_lock_order_sleep()`)
	t.Logf("seeded %d target rows (background %t) and analyzed in %s", adminLockOrderRows, background,
		time.Since(started).Round(time.Millisecond))
	return f
}

// seedBackground inserts 2,000 scopes of 200 rows: mostly succeeded reducer
// and projector rows plus a projector dead-letter tail.
func (f *adminLockOrderFixture) seedBackground(t *testing.T, ctx context.Context) {
	t.Helper()
	interleaveExec(t, ctx, f.base, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'bg-' || lpad(g::text, 5, '0'), 'repository', 'git', 'bg-' || g, NULL, 'git', 'bg-' || g,
    $1, $1, 'active', 'bg-' || lpad(g::text, 5, '0') || '-g', '{}'::jsonb
FROM generate_series(1, 2000) AS g`, f.at)
	interleaveExec(t, ctx, f.base, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload)
SELECT scope_id || '-g', scope_id, 'snapshot', 'x', $1, $1, 'active', $1, NULL, '{}'::jsonb
FROM ingestion_scopes WHERE scope_id LIKE 'bg-%'`, f.at)
	interleaveExec(t, ctx, f.base, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, failure_class, payload, created_at, updated_at, visible_at)
SELECT s.scope_id || '-w' || lpad(r::text, 3, '0'), s.scope_id, s.scope_id || '-g',
    CASE WHEN r % 4 = 0 THEN 'projector' ELSE 'reducer' END,
    CASE WHEN r % 4 = 0 THEN 'source_local' ELSE 'd' || (r % 10) END,
    'scope', s.scope_id || '-w' || r,
    CASE WHEN r % 40 = 0 THEN 'dead_letter' ELSE 'succeeded' END, 1,
    CASE WHEN r % 40 = 0 THEN 'projection_bug' END,
    jsonb_build_object('source_system', 'git'), $1::timestamptz,
    $1::timestamptz + r * interval '1 second', $1::timestamptz
FROM ingestion_scopes AS s, generate_series(1, 200) AS r
WHERE s.scope_id LIKE 'bg-%'`, f.at)
}

// reset returns every target row to dead_letter, visible_at running backwards
// and updated_at forwards with the scope order (see the fixture comment).
func (f *adminLockOrderFixture) reset(t *testing.T, ctx context.Context) {
	t.Helper()
	interleaveExec(t, ctx, f.base, `
UPDATE fact_work_items
SET status = 'dead_letter', failure_class = $2, attempt_count = 3, lease_owner = NULL,
    claim_until = NULL, next_attempt_at = NULL,
    visible_at = $1::timestamptz - ((substr(scope_id, 5)::int * 3 + substr(work_item_id, 8)::int) * interval '1 second'),
    updated_at = $1::timestamptz + ((substr(scope_id, 5)::int * 3 + substr(work_item_id, 8)::int) * interval '1 second')
WHERE scope_id LIKE 'lo-s%'`, f.at, adminLockOrderClass)
}

// sleepingSession opens a session whose row updates sleep in the trigger and
// whose deadlock_timeout is widened so a cycle stays visible.
func (f *adminLockOrderFixture) sleepingSession(t *testing.T, ctx context.Context) (*sql.DB, int) {
	t.Helper()
	db := interleaveConn(t, ctx, f.dsn, f.schema, false)
	interleaveExec(t, ctx, db, "SET "+adminLockOrderSleepGUC+" = '"+adminLockOrderSleepMS+"'")
	if _, err := db.ExecContext(ctx, "SET deadlock_timeout = '10s'"); err != nil {
		t.Logf("deadlock_timeout stays at its default: %v", err)
	}
	var pid int
	if err := db.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("backend pid: %v", err)
	}
	return db, pid
}

// adminLockOrderReplay is one replay running on its own goroutine.
type adminLockOrderReplay struct {
	done chan adminLockOrderResult
}

// adminLockOrderResult is one finished replay.
type adminLockOrderResult struct {
	ids []string
	err error
}

// wait returns the replay's result.
func (r adminLockOrderReplay) wait() adminLockOrderResult { return <-r.done }

// startAdminReplay runs the public admin store replay on db.
func startAdminReplay(ctx context.Context, db *sql.DB, filter admin.ReplayWorkItemFilter) adminLockOrderReplay {
	r := adminLockOrderReplay{done: make(chan adminLockOrderResult, 1)}
	go func() {
		items, err := store.NewStore(db).ReplayFailedWorkItems(ctx, filter)
		ids := make([]string, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.WorkItemID)
		}
		r.done <- adminLockOrderResult{ids: ids, err: err}
	}()
	return r
}

// startRecoveryReplay runs the recovery store replay on db.
func startRecoveryReplay(ctx context.Context, db *sql.DB, filter recovery.ReplayFilter) adminLockOrderReplay {
	r := adminLockOrderReplay{done: make(chan adminLockOrderResult, 1)}
	go func() {
		result, err := postgres.NewRecoveryStore(postgres.SQLDB{DB: db}).ReplayFailedWorkItems(ctx, filter, time.Now())
		r.done <- adminLockOrderResult{ids: result.WorkItemIDs, err: err}
	}()
	return r
}

// blockingPIDs returns pg_blocking_pids(pid), sorted.
func (f *adminLockOrderFixture) blockingPIDs(t *testing.T, ctx context.Context, pid int) []int {
	t.Helper()
	rows, err := f.base.QueryContext(ctx, `SELECT unnest(pg_blocking_pids($1))`, pid)
	if err != nil {
		t.Fatalf("pg_blocking_pids(%d): %v", pid, err)
	}
	defer func() { _ = rows.Close() }()
	var out []int
	for rows.Next() {
		var blocker int64
		if err := rows.Scan(&blocker); err != nil {
			t.Fatalf("scan blocker: %v", err)
		}
		out = append(out, int(blocker))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate blockers: %v", err)
	}
	slices.Sort(out)
	return out
}

// waitBlocked polls until pid waits on a lock and returns its blockers.
func (f *adminLockOrderFixture) waitBlocked(t *testing.T, ctx context.Context, pid int) []int {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if blockers := f.blockingPIDs(t, ctx, pid); len(blockers) > 0 {
			return blockers
		}
	}
	t.Fatalf("pid %d never waited on a lock", pid)
	return nil
}

// waitHoldsRowLock polls until pid holds a transaction id (its first row lock).
func (f *adminLockOrderFixture) waitHoldsRowLock(t *testing.T, ctx context.Context, pid int) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		var held bool
		if err := f.base.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid = $1 AND locktype = 'transactionid' AND granted)`,
			pid).Scan(&held); err != nil {
			t.Fatalf("pg_locks(%d): %v", pid, err)
		}
		if held {
			return
		}
	}
	t.Fatalf("pid %d never locked a row", pid)
}

// accounting returns "" when the replays reported every target row exactly
// once and every target row is pending.
func (f *adminLockOrderFixture) accounting(t *testing.T, ctx context.Context, results ...adminLockOrderResult) string {
	t.Helper()
	counted := map[string]int{}
	for _, result := range results {
		for _, id := range result.ids {
			counted[id]++
		}
	}
	var problems []string
	for _, id := range f.ids {
		if counted[id] != 1 {
			problems = append(problems, fmt.Sprintf("%s reported %d times", id, counted[id]))
		}
	}
	var pending int
	if err := f.base.QueryRowContext(ctx, `
SELECT count(*) FROM fact_work_items WHERE scope_id LIKE 'lo-s%' AND status = 'pending'`).Scan(&pending); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pending != adminLockOrderRows {
		problems = append(problems, fmt.Sprintf("%d of %d target rows pending", pending, adminLockOrderRows))
	}
	return strings.Join(problems, "; ")
}

// raceThroughHeldRow makes two lockers meet at one row deterministically: a
// third session keeps held locked while a, then b, advance until they wait,
// and then releases it. Lockers in one global order queue one behind the
// other; lockers in opposite orders hold rows on both sides of held and
// deadlock when it is released. Twin of the storage/postgres helper.
func (f *adminLockOrderFixture) raceThroughHeldRow(
	t *testing.T,
	ctx context.Context,
	held string,
	a, b func(context.Context, *sql.DB) adminLockOrderReplay,
) (adminLockOrderResult, adminLockOrderResult) {
	t.Helper()
	tx, err := interleaveConn(t, ctx, f.dsn, f.schema, false).BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT work_item_id FROM fact_work_items WHERE work_item_id = $1 FOR UPDATE`, held).Scan(&locked); err != nil {
		t.Fatalf("hold %s: %v", held, err)
	}
	sessionA, sessionB := interleaveConn(t, ctx, f.dsn, f.schema, false), interleaveConn(t, ctx, f.dsn, f.schema, false)
	var pidA, pidB int
	if err := sessionA.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pidA); err != nil {
		t.Fatalf("pid A: %v", err)
	}
	if err := sessionB.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pidB); err != nil {
		t.Fatalf("pid B: %v", err)
	}
	runA := a(ctx, sessionA)
	f.waitBlocked(t, ctx, pidA)
	runB := b(ctx, sessionB)
	f.waitBlocked(t, ctx, pidB)
	if err := tx.Rollback(); err != nil {
		t.Fatalf("release %s: %v", held, err)
	}
	return runA.wait(), runB.wait()
}
