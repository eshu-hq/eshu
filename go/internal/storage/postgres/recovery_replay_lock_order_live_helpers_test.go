// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// Shared fixture for the #8205 replay lock-order proofs. Six target scopes
// hold three dead-lettered reducer rows each. Their visible_at runs backwards
// against the scope order, so an index ordered by (scope_id, ...) visits s0
// first while an index ordered by (status, visible_at, ...) or
// (source_system, domain, status, visible_at, ...) visits s5 first. About
// 400,000 background rows plus ANALYZE make the planner pick those different
// indexes for a scoped and a stage-only replay, which is what inverts two
// unordered replays' lock order on main.
const (
	lockOrderScopes          = 6
	lockOrderRowsPerScope    = 3
	lockOrderTargetRows      = lockOrderScopes * lockOrderRowsPerScope
	lockOrderBackgroundScope = 2000
	lockOrderBackgroundRows  = 200 // rows per background scope: 400,000 total
	lockOrderClass           = "retry_exhausted"
	lockOrderSleepGUC        = "eshu.replay_lock_order_sleep_ms"
	lockOrderSeedTimeout     = 5 * time.Minute
)

// lockOrderFixture is one seeded disposable database.
type lockOrderFixture struct {
	targetDSN string
	base      *sql.DB
	scopes    []string
	ids       []string
	at        time.Time
}

// openLockOrderFixture bootstraps a disposable database, seeds the target rows
// (plus the 400,000 background rows when background is set) and runs ANALYZE.
// Only the proofs whose RED depends on the planner choosing different indexes
// need the background. It skips without a proof DSN.
func openLockOrderFixture(t *testing.T, background bool) *lockOrderFixture {
	t.Helper()
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8205 replay lock-order proofs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), lockOrderSeedTimeout)
	defer cancel()
	base, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)
	f := &lockOrderFixture{targetDSN: targetDSN, base: base, at: time.Now().UTC().Add(-time.Hour).Truncate(time.Second)}
	started := time.Now()
	if background {
		f.seedBackground(t, ctx)
	}
	f.seedTargets(t, ctx)
	f.reset(t, ctx)
	lockOrderExec(t, ctx, base, `ANALYZE fact_work_items`)
	lockOrderExec(t, ctx, base, `ANALYZE ingestion_scopes`)
	lockOrderExec(t, ctx, base, `ANALYZE scope_generations`)
	t.Logf("seeded %d target fact_work_items rows (background %t) and analyzed in %s",
		lockOrderTargetRows, background, time.Since(started).Round(time.Millisecond))
	return f
}

// seedBackground inserts 2,000 scopes of 200 rows: mostly succeeded reducer
// and projector rows, plus a projector dead-letter tail so terminal status is
// not a near-empty value to the planner.
func (f *lockOrderFixture) seedBackground(t *testing.T, ctx context.Context) {
	t.Helper()
	base := f.base
	lockOrderExec(t, ctx, base, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'bg-' || lpad(g::text, 5, '0'), 'repository', 'git', 'bg-' || g, NULL, 'git', 'bg-' || g,
    $1, $1, 'active', 'bg-' || lpad(g::text, 5, '0') || '-g', '{}'::jsonb
FROM generate_series(1, $2::int) AS g`, f.at, lockOrderBackgroundScope)
	lockOrderExec(t, ctx, base, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload)
SELECT scope_id || '-g', scope_id, 'snapshot', 'x', $1, $1, 'active', $1, NULL, '{}'::jsonb
FROM ingestion_scopes WHERE scope_id LIKE 'bg-%'`, f.at)
	lockOrderExec(t, ctx, base, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, failure_class, payload, created_at, updated_at, visible_at)
SELECT s.scope_id || '-w' || lpad(r::text, 3, '0'), s.scope_id, s.scope_id || '-g',
    CASE WHEN r % 4 = 0 THEN 'projector' ELSE 'reducer' END,
    CASE WHEN r % 4 = 0 THEN 'source_local' ELSE 'd' || (r % 10) END,
    'scope', s.scope_id || '-w' || r,
    CASE WHEN r % 40 = 0 THEN 'dead_letter' ELSE 'succeeded' END, 1,
    CASE WHEN r % 40 = 0 THEN 'projection_bug' END,
    jsonb_build_object('source_system', 'git'), $1::timestamptz, $1::timestamptz + r * interval '1 second', $1::timestamptz
FROM ingestion_scopes AS s, generate_series(1, $2::int) AS r
WHERE s.scope_id LIKE 'bg-%'`, f.at, lockOrderBackgroundRows)
}

// seedTargets inserts the six target scopes and their 18 dead-lettered rows.
func (f *lockOrderFixture) seedTargets(t *testing.T, ctx context.Context) {
	t.Helper()
	base := f.base
	for s := 0; s < lockOrderScopes; s++ {
		scope := fmt.Sprintf("lo-s%d", s)
		f.scopes = append(f.scopes, scope)
		lockOrderExec(t, ctx, base, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, NULL, 'git', $1, $2, $2, 'active', $1 || '-g', '{}'::jsonb)`, scope, f.at)
		lockOrderExec(t, ctx, base, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload)
VALUES ($1 || '-g', $1, 'snapshot', 'x', $2, $2, 'active', $2, NULL, '{}'::jsonb)`, scope, f.at)
		for r := 0; r < lockOrderRowsPerScope; r++ {
			id := fmt.Sprintf("%s-w%d", scope, r)
			f.ids = append(f.ids, id)
			lockOrderExec(t, ctx, base, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, failure_class, payload, created_at, updated_at)
VALUES ($1, $2, $2 || '-g', 'reducer', 'workload_identity', 'scope', $1, 'dead_letter', 3, $3,
    jsonb_build_object('source_system', 'git'), $4, $4)`, id, scope, lockOrderClass, f.at)
		}
	}
}

// reset returns every target row to dead_letter with visible_at running
// backwards against the scope order (see the fixture comment).
func (f *lockOrderFixture) reset(t *testing.T, ctx context.Context) {
	t.Helper()
	lockOrderExec(t, ctx, f.base, `
UPDATE fact_work_items
SET status = 'dead_letter', failure_class = $2, attempt_count = 3, lease_owner = NULL,
    claim_until = NULL, next_attempt_at = NULL,
    visible_at = $1::timestamptz - ((substr(scope_id, 5)::int * 3 + substr(work_item_id, 8)::int) * interval '1 second'),
    updated_at = $1::timestamptz - ((substr(scope_id, 5)::int * 3 + substr(work_item_id, 8)::int) * interval '1 second')
WHERE scope_id LIKE 'lo-s%'`, f.at, lockOrderClass)
}

// session opens one independent single-connection handle on the fixture's
// disposable database.
func (f *lockOrderFixture) session(t *testing.T) *sql.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return openReducerFairnessClaimerDB(t, ctx, f.targetDSN)
}

// installSleepTrigger adds a test-schema-only BEFORE UPDATE trigger that
// sleeps per row when the session sets lockOrderSleepGUC. The row lock is
// already held when a BEFORE ROW trigger fires, so the sleep widens the
// window in which a replay holds some rows and wants others.
func (f *lockOrderFixture) installSleepTrigger(t *testing.T, ctx context.Context) {
	t.Helper()
	lockOrderExec(t, ctx, f.base, `
CREATE FUNCTION replay_lock_order_sleep() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
    IF COALESCE(current_setting('`+lockOrderSleepGUC+`', true), '') <> '' THEN
        PERFORM pg_sleep(current_setting('`+lockOrderSleepGUC+`')::int / 1000.0);
    END IF;
    RETURN NEW;
END
$fn$`)
	lockOrderExec(t, ctx, f.base, `
CREATE TRIGGER zz_replay_lock_order_sleep BEFORE UPDATE ON fact_work_items
FOR EACH ROW EXECUTE FUNCTION replay_lock_order_sleep()`)
}

// statusCounts reports how many target rows are in each status.
func (f *lockOrderFixture) statusCounts(t *testing.T, ctx context.Context) map[string]int {
	t.Helper()
	rows, err := f.base.QueryContext(ctx, `SELECT status, count(*) FROM fact_work_items WHERE scope_id LIKE 'lo-s%' GROUP BY status`)
	if err != nil {
		t.Fatalf("count target statuses: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			t.Fatalf("scan target status: %v", err)
		}
		out[status] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate target statuses: %v", err)
	}
	return out
}

// lockOrderScopedFilter is the unbounded scoped replay (scope + class).
func (f *lockOrderFixture) scopedFilter() recovery.ReplayFilter {
	return recovery.ReplayFilter{Stage: recovery.StageReducer, ScopeIDs: f.scopes, FailureClass: lockOrderClass}
}

// stageOnlyFilter is the unbounded stage-only replay.
func stageOnlyFilter() recovery.ReplayFilter {
	return recovery.ReplayFilter{Stage: recovery.StageReducer}
}

// lockOrderExec runs one statement or fails the test.
func lockOrderExec(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec %.80q: %v", strings.TrimSpace(query), err)
	}
}

// lockOrderPlanNode is the subset of EXPLAIN (FORMAT JSON) the replay plan
// assertions read.
type lockOrderPlanNode struct {
	NodeType     string              `json:"Node Type"`
	RelationName string              `json:"Relation Name"`
	IndexName    string              `json:"Index Name"`
	Plans        []lockOrderPlanNode `json:"Plans"`
}

// factWorkItemsScans walks an EXPLAIN (FORMAT JSON) plan of query and returns
// every scan node on fact_work_items as "Node Type:Index Name". A bitmap scan
// is reported by its Bitmap Index Scan children, which name the index.
func factWorkItemsScans(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	var raw string
	if err := db.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("explain: %v", err)
	}
	var plans []struct {
		Plan lockOrderPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode plan: %v (%d plans)", err, len(plans))
	}
	var out []string
	var walk func(lockOrderPlanNode)
	walk = func(n lockOrderPlanNode) {
		switch {
		case n.NodeType == "Bitmap Index Scan" && strings.HasPrefix(n.IndexName, "fact_work_items"):
			out = append(out, n.NodeType+":"+n.IndexName)
		case n.RelationName == "fact_work_items" && strings.Contains(n.NodeType, "Scan") &&
			n.NodeType != "Bitmap Heap Scan":
			out = append(out, n.NodeType+":"+n.IndexName)
		}
		for _, child := range n.Plans {
			walk(child)
		}
	}
	walk(plans[0].Plan)
	sort.Strings(out)
	return out
}

// raceThroughHeldRow makes two lockers meet at one row deterministically. A
// third session locks held (a target row in the middle of the work_item_id
// order) and keeps it; a starts and advances until it waits, then b starts
// and advances until it waits, and the held row is released. Lockers in one
// global order both stop at the first row they share, so they then queue one
// behind the other. Lockers in opposite orders stop holding rows on both sides
// of held, and releasing it closes the cycle (40P01).
func (f *lockOrderFixture) raceThroughHeldRow(
	t *testing.T,
	ctx context.Context,
	held string,
	a, b func(context.Context, *sql.DB) lockOrderReplay,
) (lockOrderReplayResult, lockOrderReplayResult) {
	t.Helper()
	holder := f.session(t)
	tx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT work_item_id FROM fact_work_items WHERE work_item_id = $1 FOR UPDATE`, held).Scan(&locked); err != nil {
		t.Fatalf("hold %s: %v", held, err)
	}
	sessionA, sessionB := f.session(t), f.session(t)
	pidA, pidB := lockOrderBackendPID(t, ctx, sessionA), lockOrderBackendPID(t, ctx, sessionB)
	runA := a(ctx, sessionA)
	waitBlocked(t, ctx, f.base, pidA)
	runB := b(ctx, sessionB)
	waitBlocked(t, ctx, f.base, pidB)
	if err := tx.Rollback(); err != nil {
		t.Fatalf("release %s: %v", held, err)
	}
	return runA.wait(), runB.wait()
}
