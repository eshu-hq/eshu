// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8803 disposable PostgreSQL 18 plan and lifecycle proof of migration 211: on a populated workflow_work_items (one instance with 24,000 failed_terminal rows plus completed history) EXPLAIN of the windowed terminal-failure read seeks workflow_work_items_failed_terminal_instance_updated_idx with no Sort node, and dropping the index then applying the migration twice leaves exactly one valid index (pg_index.indisvalid); isolated bootstrapped schema per test; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"strings"
	"testing"
	"time"
)

// seedTerminalFailureIndexBacklog writes a terminal-heavy backlog: one
// instance failing every target every cycle, and one instance whose every
// failure a completion supersedes, then ANALYZEs so the planner sees it.
func seedTerminalFailureIndexBacklog(f *terminalFailureLiveFixture) {
	f.t.Helper()
	f.instance("security_alert", "hot", true, false, `{"scan_interval": "15m"}`)
	f.instance("security_alert", "recovered", true, false, `{"scan_interval": "15m"}`)
	f.exec(`INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind, collector_instance_id, scope_id,
    generation_id, status, last_failure_class, last_failure_operation, last_failure_terminal, created_at, updated_at)
SELECT 'hot-' || c || '-' || t, 'run-8803', 'security_alert', 'hot', 'scope-hot-' || t, md5(c || '-' || t),
    'failed_terminal', 'auth_denied', 'list_org_alerts', TRUE,
    $1::timestamptz - make_interval(mins => 15 * c), $1::timestamptz - make_interval(mins => 15 * c)
FROM generate_series(0, 599) AS c, generate_series(0, 39) AS t`, f.asOf)
	f.exec(`INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind, collector_instance_id, scope_id,
    generation_id, status, last_failure_class, last_failure_operation, last_failure_terminal, created_at, updated_at)
SELECT 'rec-' || s || '-' || c || '-' || t, 'run-8803', 'security_alert', 'recovered', 'scope-rec-' || t,
    md5(s || c || '-' || t), s, CASE WHEN s = 'failed_terminal' THEN 'rate_limited' END,
    CASE WHEN s = 'failed_terminal' THEN 'list_org_alerts' END, CASE WHEN s = 'failed_terminal' THEN TRUE END,
    $1::timestamptz - make_interval(mins => 15 * c + CASE WHEN s = 'failed_terminal' THEN 6 ELSE 1 END),
    $1::timestamptz - make_interval(mins => 15 * c + CASE WHEN s = 'failed_terminal' THEN 6 ELSE 1 END)
FROM unnest(ARRAY['failed_terminal', 'completed']) AS s, generate_series(0, 299) AS c, generate_series(0, 39) AS t`, f.asOf)
	f.exec(`ANALYZE workflow_work_items`)
}

func TestCollectorTerminalFailureReadUsesPartialIndexLive(t *testing.T) {
	started := time.Now()
	f := newTerminalFailureLiveFixture(t, "terminal_failure_plan")
	seedTerminalFailureIndexBacklog(f)

	kinds := []string{"security_alert", "security_alert"}
	ids := []string{"hot", "recovered"}
	cutoff := terminalFailureCutoff(f.asOf, 15*time.Minute)
	cutoffs := []time.Time{cutoff, cutoff}
	rows, err := f.database.QueryContext(f.ctx, "EXPLAIN (COSTS OFF) "+workflowCollectorTerminalFailuresQuery, kinds, ids, cutoffs)
	if err != nil {
		t.Fatalf("EXPLAIN terminal failure read: %v", err)
	}
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, line)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close plan rows: %v", err)
	}
	text := strings.Join(plan, "\n")
	if !strings.Contains(text, "Index Scan using "+workflowTerminalFailureIndexName+" on workflow_work_items t") {
		t.Fatalf("terminal failure read does not seek %s:\n%s", workflowTerminalFailureIndexName, text)
	}
	// The only Sort allowed is the outer one over the unnest rows; the
	// per-instance read must come back in index order.
	if sorts := strings.Count(text, "Sort Key:"); sorts > 1 {
		t.Fatalf("plan has %d Sort nodes, want only the outer instance sort:\n%s", sorts, text)
	}
	if strings.Contains(text, "Sort Key: t.updated_at") {
		t.Fatalf("per-instance read sorts instead of reading the index in order:\n%s", text)
	}
	got := f.read()
	if failure, ok := got["hot"]; !ok || failure.FailureClass != "auth_denied" || !failure.ObservedAt.Equal(f.asOf) {
		t.Fatalf("hot failure = %+v (ok=%v), want auth_denied at asOf", failure, ok)
	}
	if failure, ok := got["recovered"]; ok {
		t.Fatalf("recovered instance reported %+v, want none", failure)
	}
	t.Logf("plan proof: 48,000 seeded work items, %s\n%s", time.Since(started), text)
}

func TestCollectorTerminalFailureIndexMigrationReappliesLive(t *testing.T) {
	f := newTerminalFailureLiveFixture(t, "terminal_failure_reapply")
	seedTerminalFailureIndexBacklog(f)
	migration := MigrationSQL(workflowTerminalFailureMigrationName)

	f.exec("DROP INDEX " + workflowTerminalFailureIndexName)
	for attempt := 1; attempt <= 2; attempt++ {
		began := time.Now()
		if _, err := f.database.ExecContext(f.ctx, migration); err != nil {
			t.Fatalf("apply migration 211 attempt %d: %v", attempt, err)
		}
		t.Logf("migration 211 attempt %d on 48,000 rows: %s", attempt, time.Since(began))
	}
	var count, valid int
	if err := f.database.QueryRowContext(f.ctx, `
SELECT count(*), count(*) FILTER (WHERE x.indisvalid AND x.indisready)
FROM pg_index AS x
JOIN pg_class AS c ON c.oid = x.indexrelid
WHERE c.relname = $1
  AND c.relnamespace = to_regnamespace(current_schema())`, workflowTerminalFailureIndexName).Scan(&count, &valid); err != nil {
		t.Fatalf("read index validity: %v", err)
	}
	if count != 1 || valid != 1 {
		t.Fatalf("after two applications: %d indexes named %s, %d valid; want exactly one valid", count, workflowTerminalFailureIndexName, valid)
	}
}
