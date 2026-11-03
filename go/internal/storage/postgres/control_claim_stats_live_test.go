// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9871 claim-plan proof on real PostgreSQL: the fair-claim candidate on a freshly bulk-loaded never-analyzed workflow_work_items table, then after ANALYZE; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// statsProofTimeout bounds the 100k-item corpus build plus the cold and warm
// EXPLAIN runs. The cold EXPLAIN (ANALYZE, BUFFERS) executes the ~19 s slow
// plan, so this is far above the isolated-helper 90 s context.
const statsProofTimeout = 15 * time.Minute

// statsProofKeys and statsProofPerKey fix the representative corpus: 100,000
// pending items across 200 fairness keys, the shape whose cold claim took
// 18.7 s in the issue.
const (
	statsProofKeys    = 200
	statsProofPerKey  = 500
	statsProofKind    = "git"
	statsProofInst    = "stats-proof-instance"
	statsProofRun     = "stats-proof-run"
	statsProofKeyFmt  = "stats-proof-key-%03d"
	statsProofClaimMs = 5000
)

// statsProofContext returns a context the corpus build cannot outlive.
func statsProofContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), statsProofTimeout)
}

// workflowWorkItemsAnalyzeState reports whether workflow_work_items has ever
// been analyzed, via the same pg_stat_all_tables row the admission gate reads.
// A table that was never analyzed reports two NULLs; TRUNCATE does not clear
// them, so only a fresh, recreated, or restored table reads cold (#9871).
func workflowWorkItemsAnalyzeState(t *testing.T, database *sql.DB, ctx context.Context) (analyzed, autoAnalyzed bool) {
	t.Helper()
	var lastAnalyze, lastAutoAnalyze sql.NullTime
	row := database.QueryRowContext(ctx, `SELECT last_analyze, last_autoanalyze FROM pg_stat_all_tables WHERE relname = 'workflow_work_items'`)
	if err := row.Scan(&lastAnalyze, &lastAutoAnalyze); err != nil {
		t.Fatalf("read workflow_work_items analyze state: %v", err)
	}
	return lastAnalyze.Valid, lastAutoAnalyze.Valid
}

// requireNeverAnalyzed fails the proof when the table already carries
// statistics: autovacuum firing between the load and this check would make a
// cold-plan assertion meaningless, and the failure names that interference.
func requireNeverAnalyzed(t *testing.T, database *sql.DB, ctx context.Context, stage string) {
	t.Helper()
	analyzed, autoAnalyzed := workflowWorkItemsAnalyzeState(t, database, ctx)
	if analyzed || autoAnalyzed {
		t.Fatalf("workflow_work_items already analyzed at %s (manual=%v auto=%v): autovacuum fired before the cold plan, rerun for a clean cold table", stage, analyzed, autoAnalyzed)
	}
}

// seedStatsProofCorpusRaw bulk-loads the representative corpus with plain SQL,
// bypassing every production admission path. The loader is intentionally not
// the store: the theory proof needs a table that stays cold no matter what
// admission does, so the cold and warm plans stay comparable after the fix.
func seedStatsProofCorpusRaw(t *testing.T, database *sql.DB, ctx context.Context) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `INSERT INTO workflow_runs (run_id, trigger_kind, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)`,
		statsProofRun, string(workflow.TriggerKindSchedule), string(workflow.RunStatusCollectionPending), time.Now().UTC().Truncate(time.Second)); err != nil {
		t.Fatalf("insert stats proof run: %v", err)
	}
	res, err := database.ExecContext(ctx, fmt.Sprintf(`INSERT INTO workflow_work_items (
		work_item_id, run_id, collector_kind, collector_instance_id,
		source_system, scope_id, acceptance_unit_id, source_run_id,
		generation_id, fairness_key, status, created_at, updated_at
	) SELECT
		'stats-proof-item-' || g,
		$1, $2, $3, $2,
		'stats-proof-scope-' || ((g - 1) / %d),
		'stats-proof-scope-' || ((g - 1) / %d),
		'stats-proof-gen-' || ((g - 1) / %d),
		'stats-proof-gen-' || ((g - 1) / %d),
		'stats-proof-key-' || lpad((((g - 1) / %d)::text), 3, '0'),
		'pending', now(), now()
	FROM generate_series(1, %d) AS g`, statsProofPerKey, statsProofPerKey, statsProofPerKey, statsProofPerKey, statsProofPerKey, statsProofKeys*statsProofPerKey),
		statsProofRun, statsProofKind, statsProofInst)
	if err != nil {
		t.Fatalf("bulk-load stats proof corpus: %v", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != statsProofKeys*statsProofPerKey {
		t.Fatalf("bulk-loaded rows = %d, %v; want %d", n, err, statsProofKeys*statsProofPerKey)
	}
}

// explainClaimCandidate runs the production fair-claim candidate read path
// (derived from the shipped claimNextWorkflowWorkItemQuery constant, never
// hand-copied) under the given EXPLAIN prefix. The derivation matches
// TestWorkflowClaimCandidateUsesFairClaimIndex: truncating at the candidate
// CTE boundary keeps the proof on the shipped statement.
func explainClaimCandidate(t *testing.T, database *sql.DB, ctx context.Context, explainPrefix, kind, instance string, now time.Time) string {
	t.Helper()
	const candidateMarker = "WITH RECURSIVE keys AS ("
	trimmed := strings.TrimLeft(claimNextWorkflowWorkItemQuery, "\n")
	head, ok := strings.CutPrefix(trimmed, candidateMarker)
	if !ok {
		t.Fatalf("claim query does not start with %q; derivation boundary moved", candidateMarker)
	}
	candidate, _, found := strings.Cut(head, "),\nclaimed_item AS (")
	if !found {
		t.Fatalf("claim query candidate CTE boundary moved; update the derivation")
	}
	explainQuery := "EXPLAIN (" + explainPrefix + ") " + candidateMarker + candidate + ") SELECT work_item_id FROM candidate"
	explainQuery = strings.ReplaceAll(explainQuery, "$7", "'stats-proof-plan'")
	rows, err := database.QueryContext(ctx, explainQuery, kind, instance, now.UTC())
	if err != nil {
		t.Fatalf("EXPLAIN claim candidate: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan EXPLAIN row: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read EXPLAIN rows: %v", err)
	}
	return plan.String()
}

// timedStatsProofClaims grants n claims through the production claim path and
// returns each grant latency in milliseconds.
func timedStatsProofClaims(t *testing.T, store *WorkflowControlStore, ctx context.Context, kind, instance string, now time.Time, n int) []float64 {
	t.Helper()
	latencies := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		_, _, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
			CollectorKind:       scope.CollectorKind(kind),
			CollectorInstanceID: instance,
			OwnerID:             "stats-proof-owner",
			ClaimID:             fmt.Sprintf("stats-proof-claim-%d", i),
			RotationSalt:        "stats-proof-salt",
		}, now, 5*time.Minute)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("ClaimNextEligible #%d: %v", i, err)
		}
		if !found {
			t.Fatalf("ClaimNextEligible #%d found = false with eligible work pending", i)
		}
		latencies = append(latencies, float64(elapsed.Microseconds())/1000.0)
	}
	return latencies
}

// summarizeLatencies reports p50 and max over claim latencies in milliseconds.
func summarizeLatencies(latencies []float64) (p50, max float64) {
	sorted := append([]float64(nil), latencies...)
	sort.Float64s(sorted)
	max = sorted[len(sorted)-1]
	p50 = sorted[len(sorted)/2]
	return p50, max
}

// TestWorkflowClaimColdTableTheory pins the #9871 mechanism with the shipped
// statement on the representative corpus: a freshly loaded never-analyzed
// table plans the claim candidate on workflow_work_items_family_queue_depth_idx
// with a rows=1 estimate, and the same candidate plans on
// workflow_work_items_fair_claim_idx after ANALYZE. The seed bypasses
// admission, so this characterization holds before and after the fix.
func TestWorkflowClaimColdTableTheory(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx, cancel := statsProofContext(t)
	defer cancel()
	now := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)

	requireNeverAnalyzed(t, database, ctx, "post-schema")
	seedStatsProofCorpusRaw(t, database, ctx)
	requireNeverAnalyzed(t, database, ctx, "post-load")

	coldPlan := explainClaimCandidate(t, database, ctx, "COSTS OFF", statsProofKind, statsProofInst, now)
	t.Logf("cold candidate plan (never analyzed):\n%s", coldPlan)
	if !strings.Contains(coldPlan, "Index Scan using workflow_work_items_family_queue_depth_idx") {
		t.Fatalf("cold candidate misses the family-queue-depth index; the theory premise changed:\n%s", coldPlan)
	}

	coldTimed := explainClaimCandidate(t, database, ctx, "ANALYZE, BUFFERS", statsProofKind, statsProofInst, now)
	t.Logf("cold EXPLAIN (ANALYZE, BUFFERS):\n%s", coldTimed)

	if _, err := database.ExecContext(ctx, `ANALYZE workflow_work_items`); err != nil {
		t.Fatalf("ANALYZE workflow_work_items: %v", err)
	}
	warmPlan := explainClaimCandidate(t, database, ctx, "ANALYZE, BUFFERS", statsProofKind, statsProofInst, now)
	t.Logf("warm EXPLAIN (ANALYZE, BUFFERS) after explicit ANALYZE:\n%s", warmPlan)
	if !strings.Contains(warmPlan, "Index Scan using workflow_work_items_fair_claim_idx") {
		t.Fatalf("warm candidate misses the fair-claim index:\n%s", warmPlan)
	}
	if strings.Contains(warmPlan, "Seq Scan on workflow_work_items") {
		t.Fatalf("warm candidate sequentially scans the backlog:\n%s", warmPlan)
	}

	latencies := timedStatsProofClaims(t, store, ctx, statsProofKind, statsProofInst, now, 20)
	p50, max := summarizeLatencies(latencies)
	t.Logf("warm claims n=20: p50=%.1fms max=%.1fms", p50, max)
	for i, ms := range latencies {
		if ms > statsProofClaimMs {
			t.Fatalf("warm claim #%d took %.1fms, above the %dms pathology bound", i, ms, statsProofClaimMs)
		}
	}
}

// TestWorkflowClaimFirstClaimAfterBulkAdmission is the #9871 regression: the
// first claim after a bulk admission through the production enqueue path must
// plan on workflow_work_items_fair_claim_idx with no manual ANALYZE. It fails
// while admission leaves a never-analyzed table behind, and passes once
// admission analyzes it.
func TestWorkflowClaimFirstClaimAfterBulkAdmission(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx, cancel := statsProofContext(t)
	defer cancel()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	now := base.Add(time.Hour)

	requireNeverAnalyzed(t, database, ctx, "post-schema")

	keys := make([]claimDepthKeyBacklog, 0, statsProofKeys)
	for k := 0; k < statsProofKeys; k++ {
		keys = append(keys, claimDepthKeyBacklog{
			FairnessKey: fmt.Sprintf(statsProofKeyFmt, k),
			Count:       statsProofPerKey,
		})
	}
	seeded := seedClaimDepthBacklog(t, store, ctx, "stats-proof-admission-run", base, keys)
	if len(seeded) != statsProofKeys*statsProofPerKey {
		t.Fatalf("admitted %d items, want %d", len(seeded), statsProofKeys*statsProofPerKey)
	}

	plan := explainClaimCandidate(t, database, ctx, "COSTS OFF", string(scope.CollectorGit), "claim-depth-instance", now)
	t.Logf("first-claim candidate plan after bulk admission, no manual ANALYZE:\n%s", plan)
	if !strings.Contains(plan, "Index Scan using workflow_work_items_fair_claim_idx") {
		t.Fatalf("first claim after bulk admission misses the fair-claim index (no manual ANALYZE ran):\n%s", plan)
	}
	if strings.Contains(plan, "Seq Scan on workflow_work_items") {
		t.Fatalf("first claim after bulk admission sequentially scans the backlog:\n%s", plan)
	}

	latencies := timedStatsProofClaims(t, store, ctx, string(scope.CollectorGit), "claim-depth-instance", now, 20)
	p50, max := summarizeLatencies(latencies)
	t.Logf("post-admission claims n=20: p50=%.1fms max=%.1fms", p50, max)
	for i, ms := range latencies {
		if ms > statsProofClaimMs {
			t.Fatalf("post-admission claim #%d took %.1fms, above the %dms pathology bound", i, ms, statsProofClaimMs)
		}
	}
}

// TestWorkflowClaimFirstClaimAfterBulkAdmissionOntoStatlessTable is the
// #9926 regression: a bulk that lands on a table whose last ANALYZE covered
// a trivially small base (here empty, as autovacuum leaves a fresh table)
// must still refresh statistics through the production admission path, so
// the first claim plans on workflow_work_items_fair_claim_idx with no manual
// ANALYZE. It fails while the gate only checks never-analyzed: the admission
// skips, and the candidate plans all five scans on the family index.
//
// The bulk arrives out-of-band (restore, backfill, or another writer) in the
// narrow stats-proof shape, then a production admission lands on top: the
// gate contract is per admission, so any admission onto a statless table
// must refresh. A fully production-seeded bulk does not reproduce the cold
// shape (wide rows tip the zero-statistics cost tie toward the fair index),
// which is why the bulk here is raw SQL while the trigger admission is the
// production path.
func TestWorkflowClaimFirstClaimAfterBulkAdmissionOntoStatlessTable(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx, cancel := statsProofContext(t)
	defer cancel()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	now := base.Add(time.Hour)

	// Simulate autovacuum's quick first pass over the fresh table: analyzed
	// but describing an empty base. The never-analyzed gate passes here.
	// Autovacuum stays off for the rest of the test so the only analyzer
	// that can refresh the statistics is the admission gate under test;
	// otherwise an already-awake daemon could autoanalyze the bulk load and
	// the plan would prove nothing about the gate.
	if _, err := database.ExecContext(ctx, `ANALYZE workflow_work_items`); err != nil {
		t.Fatalf("ANALYZE empty workflow_work_items: %v", err)
	}
	if _, err := database.ExecContext(ctx, `ALTER TABLE workflow_work_items SET (autovacuum_enabled = false)`); err != nil {
		t.Fatalf("disable autovacuum on workflow_work_items: %v", err)
	}
	if analyzed, _ := workflowWorkItemsAnalyzeState(t, database, ctx); !analyzed {
		t.Fatal("empty table reads never-analyzed after explicit ANALYZE; the stale shape needs an analyzed base")
	}

	// Out-of-band bulk in the narrow stats-proof shape, on the same
	// kind/instance the production admission and the plan check use.
	if _, err := database.ExecContext(ctx, `INSERT INTO workflow_runs (run_id, trigger_kind, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)`,
		"stats-proof-stale-bulk-run", string(workflow.TriggerKindSchedule), string(workflow.RunStatusCollectionPending), base); err != nil {
		t.Fatalf("insert stale bulk run: %v", err)
	}
	res, err := database.ExecContext(ctx, fmt.Sprintf(`INSERT INTO workflow_work_items (
			work_item_id, run_id, collector_kind, collector_instance_id,
			source_system, scope_id, acceptance_unit_id, source_run_id,
			generation_id, fairness_key, status, created_at, updated_at
		) SELECT
			'stale-bulk-item-' || g,
			$1, $2, $3, $2,
			'stale-bulk-scope-' || ((g - 1) / %d),
			'stale-bulk-scope-' || ((g - 1) / %d),
			'stale-bulk-run', 'stale-bulk-gen-1',
			'stale-bulk-key-' || lpad((((g - 1) %% 200) + 1)::text, 3, '0'),
			'pending', $4, $4
		FROM generate_series(1, %d) g`, statsProofPerKey, statsProofPerKey, statsProofKeys*statsProofPerKey),
		"stats-proof-stale-bulk-run", statsProofKind, "claim-depth-instance", base)
	if err != nil {
		t.Fatalf("stale bulk load: %v", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != statsProofKeys*statsProofPerKey {
		t.Fatalf("stale bulk loaded rows = %d, %v; want %d", n, err, statsProofKeys*statsProofPerKey)
	}

	// Production admission on top: ten keys of backlog through
	// EnqueueWorkItems. The gate must fire on the statless table.
	keys := make([]claimDepthKeyBacklog, 0, 10)
	for k := 0; k < 10; k++ {
		keys = append(keys, claimDepthKeyBacklog{
			FairnessKey: fmt.Sprintf(statsProofKeyFmt, k),
			Count:       statsProofPerKey,
		})
	}
	seeded := seedClaimDepthBacklog(t, store, ctx, "stats-proof-stale-run", base, keys)
	if len(seeded) != 10*statsProofPerKey {
		t.Fatalf("admitted %d items, want %d", len(seeded), 10*statsProofPerKey)
	}

	plan := explainClaimCandidate(t, database, ctx, "COSTS OFF", string(scope.CollectorGit), "claim-depth-instance", now)
	t.Logf("first-claim candidate plan after admission onto statless table, no manual ANALYZE:\n%s", plan)
	if !strings.Contains(plan, "Index Scan using workflow_work_items_fair_claim_idx") {
		t.Fatalf("first claim after admission onto statless table misses the fair-claim index (no manual ANALYZE ran):\n%s", plan)
	}
	if strings.Contains(plan, "family_queue_depth_idx") {
		t.Fatalf("first claim after admission onto statless table still probes the family index:\n%s", plan)
	}
	if strings.Contains(plan, "Seq Scan on workflow_work_items") {
		t.Fatalf("first claim after admission onto statless table sequentially scans the backlog:\n%s", plan)
	}

	latencies := timedStatsProofClaims(t, store, ctx, string(scope.CollectorGit), "claim-depth-instance", now, 20)
	p50, max := summarizeLatencies(latencies)
	t.Logf("post-admission claims onto refreshed statistics n=20: p50=%.1fms max=%.1fms", p50, max)
	for i, ms := range latencies {
		if ms > statsProofClaimMs {
			t.Fatalf("post-admission claim #%d took %.1fms, above the %dms pathology bound", i, ms, statsProofClaimMs)
		}
	}
}
