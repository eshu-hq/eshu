// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package items

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8840 disposable PostgreSQL proof that the work-item retention prune keeps its pre-#8840 plan: on a seed where each security_alert target failed for its older half and then recovered, the newer-row probe never uses workflow_work_items_security_alert_scope_updated_idx, its shared buffers stay within 1.2x of the plan with that index dropped in a rolled-back transaction, and the batch outcome matches; a planted full (collector_kind, scope_id, updated_at DESC) index in a rolled-back transaction must be picked and must blow the budget, so the guard is proven to fire; buffers are gated, wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// securityAlertScopeIndex is the migration 231 partial index (#8840). The
// prune's newer-row probe must never use it.
const securityAlertScopeIndex = "workflow_work_items_security_alert_scope_updated_idx"

// pruneBudget is the largest allowed ratio of the prune's shared buffers, and
// of the index entries its newer-row probe walks past, to the same figure for
// the plan without the #8840 index.
const pruneBudget = 1.2

// topBuffersPattern matches the first (top plan node) buffer line.
var topBuffersPattern = regexp.MustCompile(`Buffers: shared hit=(\d+)(?: read=(\d+))?`)

// newerProbePattern matches the newer-row probe node, its loop count, and the
// average rows its filter removed per loop.
var newerProbePattern = regexp.MustCompile(
	`(?s)on workflow_work_items newer .*?loops=(\d+)\).*?Rows Removed by Filter: (\d+)`)

// prunePlan is one EXPLAIN (ANALYZE, BUFFERS) run of pruneBatchQuery.
type prunePlan struct {
	text    string
	buffers int
	walked  int      // newer-row probe entries removed by its filter, all loops
	outcome [3]int64 // scanned, deleted completed, deleted failed_terminal
}

// TestPruneWorkItemsLivePlanIgnoresSecurityAlertIndex seeds 20 security_alert
// targets whose older half is failed_terminal and newer half completed, plus
// git history. A full (collector_kind, scope_id, updated_at DESC) index made
// the newer-row probe walk every newer completed row before it reached a
// failed_terminal one (#8840 review). The partial index must leave the plan
// alone.
func TestPruneWorkItemsLivePlanIgnoresSecurityAlertIndex(t *testing.T) {
	database, ctx := isolatedLiveDB(t)
	var present bool
	if err := database.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)`, securityAlertScopeIndex,
	).Scan(&present); err != nil || !present {
		t.Fatalf("bootstrap schema lacks %s (err=%v)", securityAlertScopeIndex, err)
	}
	seedRun(t, ctx, database, "run-plan", "complete")
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_work_items (
    work_item_id, run_id, collector_kind, collector_instance_id, source_system,
    scope_id, acceptance_unit_id, source_run_id, generation_id, status, created_at, updated_at
)
SELECT md5(k || ':' || s || ':' || i), 'run-plan', k, 'inst-1', k, k || '-' || s, 'unit', 'src', 'gen-' || (i % 20),
       CASE WHEN k = 'security_alert' AND i <= n / 2 THEN 'failed_terminal'
            WHEN k = 'git' AND i % 97 = 0 THEN 'failed_terminal'
            ELSE 'completed' END,
       $1::timestamptz + i * interval '1 minute', $1::timestamptz + i * interval '1 minute'
FROM (VALUES ('security_alert', 20, 2880), ('git', 50, 1000)) AS shape(k, scopes, n),
     generate_series(1, scopes) AS s, generate_series(1, n) AS i`, liveEpoch); err != nil {
		t.Fatalf("seed work items: %v", err)
	}
	if _, err := database.ExecContext(ctx, `VACUUM (ANALYZE) workflow_work_items`); err != nil {
		t.Fatalf("vacuum: %v", err)
	}

	shipped := explainPrune(t, ctx, database)
	baseline := explainPrune(t, ctx, database, "DROP INDEX "+securityAlertScopeIndex)
	t.Logf("prune with %s: %d buffers, %d entries walked\n%s", securityAlertScopeIndex, shipped.buffers, shipped.walked, shipped.text)
	t.Logf("prune without it: %d buffers, %d entries walked", baseline.buffers, baseline.walked)
	if strings.Contains(shipped.text, securityAlertScopeIndex) {
		t.Fatalf("prune plan uses %s:\n%s", securityAlertScopeIndex, shipped.text)
	}
	if err := withinBudget(shipped, baseline); err != "" {
		t.Fatal(err)
	}
	if shipped.outcome != baseline.outcome {
		t.Fatalf("prune outcome = %v, without the index = %v", shipped.outcome, baseline.outcome)
	}

	// Seeded violation: the full key the first #8840 draft shipped. The guard
	// must catch it, or the checks above prove nothing.
	planted := explainPrune(t, ctx, database,
		"DROP INDEX "+securityAlertScopeIndex,
		`CREATE INDEX planted_full_scope_updated_idx ON workflow_work_items (collector_kind, scope_id, updated_at DESC, work_item_id ASC)`,
		`ANALYZE workflow_work_items`)
	t.Logf("prune with the planted full index: %d buffers, %d entries walked\n%s", planted.buffers, planted.walked, planted.text)
	if !strings.Contains(planted.text, "planted_full_scope_updated_idx") {
		t.Fatalf("seeded violation not reproduced: planner ignored the planted full index:\n%s", planted.text)
	}
	if withinBudget(planted, baseline) == "" {
		t.Fatalf("budget did not fire on the planted full index: %d vs %d buffers, %d vs %d entries walked",
			planted.buffers, baseline.buffers, planted.walked, baseline.walked)
	}
	if planted.outcome != baseline.outcome {
		t.Fatalf("planted outcome = %v, without the index = %v", planted.outcome, baseline.outcome)
	}
}

// withinBudget returns a failure message when plan exceeds the budget
// relative to baseline on shared buffers or on newer-probe entries walked, or
// "" when it is within both. Both are counts, so the gate does not depend on
// wall time or host load.
func withinBudget(plan, baseline prunePlan) string {
	if float64(plan.buffers) > pruneBudget*float64(baseline.buffers) {
		return fmt.Sprintf("prune read %d shared buffers, over %.1fx the %d of the plan without the #8840 index:\n%s",
			plan.buffers, pruneBudget, baseline.buffers, plan.text)
	}
	if float64(plan.walked) > pruneBudget*float64(baseline.walked) {
		return fmt.Sprintf("prune's newer-row probe walked %d entries, over %.1fx the %d of the plan without the #8840 index:\n%s",
			plan.walked, pruneBudget, baseline.walked, plan.text)
	}
	return ""
}

// explainPrune runs setup and then pruneBatchQuery under EXPLAIN (ANALYZE,
// BUFFERS) and again plainly for its outcome, all in one transaction that is
// rolled back, so nothing is deleted and no index change survives.
func explainPrune(t *testing.T, ctx context.Context, database *sql.DB, setup ...string) prunePlan {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range setup {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			t.Fatalf("setup %q: %v", statement, err)
		}
	}
	cutoff := testPolicy(2000, 1).Cutoff(liveAsOf)
	if _, err := tx.ExecContext(ctx, "SAVEPOINT plan"); err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	rows, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+pruneBatchQuery, "", 2000, cutoff)
	if err != nil {
		t.Fatalf("explain prune: %v", err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	_ = rows.Close()
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT plan"); err != nil {
		t.Fatalf("rollback to savepoint: %v", err)
	}
	out := prunePlan{text: plan.String()}
	match := topBuffersPattern.FindStringSubmatch(out.text)
	if match == nil {
		t.Fatalf("prune plan has no buffer line:\n%s", out.text)
	}
	hit, _ := strconv.Atoi(match[1])
	read := 0
	if match[2] != "" {
		read, _ = strconv.Atoi(match[2])
	}
	out.buffers = hit + read
	probe := newerProbePattern.FindStringSubmatch(out.text)
	if probe == nil {
		t.Fatalf("prune plan has no newer-row probe with a filter:\n%s", out.text)
	}
	loops, _ := strconv.Atoi(probe[1])
	removed, _ := strconv.Atoi(probe[2])
	out.walked = loops * removed
	var next sql.NullString
	if err := tx.QueryRowContext(ctx, pruneBatchQuery, "", 2000, cutoff).
		Scan(&next, &out.outcome[0], &out.outcome[1], &out.outcome[2]); err != nil {
		t.Fatalf("run prune: %v", err)
	}
	if out.outcome[1]+out.outcome[2] == 0 {
		t.Fatalf("prune deleted nothing; the seed does not exercise the newer-row probe")
	}
	return out
}
