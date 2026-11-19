// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachabilitystore_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10870 disposable PostgreSQL 18 proof that the idle cursor probe serves from the existing pending index, that a run whose readiness flips with no new intent completion is skipped until the audit and then projected, that a fresh completion is picked up without waiting for the audit, and that two concurrent runners converge; enrolled in the live-postgres-readiness runner. Upper-bound timing: 20s cold including one-time schema bootstrap and the section fixtures, on local postgres:18
//eshu:readiness:env ESHU_REACHABILITY_IDLE_PROBE_PROOF_DSN ESHU_REACHABILITY_IDLE_PROBE_PROOF_DISPOSABLE

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/codeintel"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	reachabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/reachability"
)

// openIdleProbeLiveDB opens the disposable PostgreSQL 18 database this proof
// enrolls in the live-postgres-readiness runner under, skipping without the
// runner's DSN pair:
//
//	ESHU_REACHABILITY_IDLE_PROBE_PROOF_DSN=postgres://user:pass@127.0.0.1:<port>/postgres?sslmode=disable \
//	ESHU_REACHABILITY_IDLE_PROBE_PROOF_DISPOSABLE=1 \
//	  go test ./internal/storage/postgres/code/reachability/ -run TestIdleProbeLiveProof
func openIdleProbeLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ESHU_REACHABILITY_IDLE_PROBE_PROOF_DSN")
	optIn := os.Getenv("ESHU_REACHABILITY_IDLE_PROBE_PROOF_DISPOSABLE")
	if dsn == "" || optIn != "1" {
		t.Skip("ESHU_REACHABILITY_IDLE_PROBE_PROOF_DSN/ESHU_REACHABILITY_IDLE_PROBE_PROOF_DISPOSABLE not set")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open live db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("bootstrap schema: %v", err)
	}
	return ctx, db
}

// idleProbeStore wraps the disposable DB in the production store.
func idleProbeStore(db *sql.DB) *reachabilitystore.CodeReachabilityStore {
	return reachabilitystore.NewCodeReachabilityStore(postgres.SQLDB{DB: db})
}

// idleProbeRunner builds a store-backed runner with test-scale knobs: a long
// audit so only the explicit audit-forcing step can trigger it.
func idleProbeRunner(db *sql.DB) *codeintel.CodeReachabilityProjectionRunner {
	store := idleProbeStore(db)
	return &codeintel.CodeReachabilityProjectionRunner{
		InputLoader: store,
		RowWriter:   store,
		Config: codeintel.CodeReachabilityProjectionRunnerConfig{
			BatchLimit:    10,
			AuditInterval: time.Hour,
		},
	}
}

// idleProbeWatermark reads the repository watermark, reporting whether one
// exists and its updated_at.
func idleProbeWatermark(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID, repoID string) (time.Time, bool) {
	t.Helper()
	var updatedAt time.Time
	err := db.QueryRowContext(ctx, `SELECT updated_at FROM code_reachability_repository_watermarks
	  WHERE scope_id=$1 AND generation_id=$2 AND repository_id=$3`,
		scopeID, generationID, repoID).Scan(&updatedAt)
	if err == sql.ErrNoRows {
		return time.Time{}, false
	}
	if err != nil {
		t.Fatalf("read watermark: %v", err)
	}
	return updatedAt, true
}

// idleProbeBuffers sums every "shared hit=N" in an EXPLAIN (ANALYZE,
// BUFFERS) plan.
func idleProbeBuffers(t *testing.T, plan string) int {
	t.Helper()
	total := 0
	for _, match := range regexp.MustCompile(`shared hit=(\d+)`).FindAllStringSubmatch(plan, -1) {
		n, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse buffers %q: %v", match[1], err)
		}
		total += n
	}
	return total
}

// TestIdleProbeLiveProof is the #10870 live proof, sequential in one
// function on the isolated disposable DB so every section's timestamps stay
// the global newest: probe correctness and plan shape, the audit-miss case
// (readiness flips with no new intent completion), the intent-driven pickup,
// and two-runner convergence.
func TestIdleProbeLiveProof(t *testing.T) {
	ctx, db := openIdleProbeLiveDB(t)
	base := time.Now().UTC().Truncate(time.Second)
	suffix := routeLivenessTestSuffix(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}

	// Section A: the probe reports the newest completed intent in either
	// traversed domain, and its plan is two backward index-only tip scans.
	tA := base.Add(time.Minute)
	keyA := "probe-" + suffix
	exec(`INSERT INTO ingestion_scopes
	  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
	   observed_at, ingested_at, status, active_generation_id, payload)
	  VALUES ($1,'repository','git',$1,'git',$1,$2,$2,'active',$3,'{}'::jsonb)`,
		"scope-"+keyA, tA, "gen-"+keyA)
	registerRouteLivenessCleanup(t, db, "scope-"+keyA, "repo-"+keyA)
	exec(`INSERT INTO shared_projection_intents
	  (intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id, repository_id,
	   source_run_id, generation_id, payload, created_at, completed_at)
	  VALUES ('intent-a-calls-`+suffix+`','code_calls','p','scope-`+keyA+`','repo-`+keyA+`','repo-`+keyA+`',
	   'run-`+keyA+`','gen-`+keyA+`','{}'::jsonb,$1,$2)`, tA, tA)
	exec(`INSERT INTO shared_projection_intents
	  (intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id, repository_id,
	   source_run_id, generation_id, payload, created_at, completed_at)
	  VALUES ('intent-a-inh-`+suffix+`','inheritance_edges','p','scope-`+keyA+`','repo-`+keyA+`','repo-`+keyA+`',
	   'run-`+keyA+`','gen-`+keyA+`','{}'::jsonb,$1,$2)`, tA, tA.Add(-time.Hour))
	// Fixture scale below the section timestamps: 20k completed intents
	// across both domains so the planner prices the backward-limit probe
	// as it does on production, while the section seeds stay the global
	// maxima deterministically.
	exec(`INSERT INTO shared_projection_intents
	  (intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id, repository_id,
	   source_run_id, generation_id, payload, created_at, completed_at)
	  SELECT 'bulk-`+suffix+`-' || g,
	         CASE WHEN (g % 2) = 0 THEN 'code_calls' ELSE 'inheritance_edges' END,
	         'part', 'scope-bulk-`+suffix+`', 'repo-bulk', 'repo-bulk',
	         'run-bulk', 'gen-bulk', '{}'::jsonb,
	         $1::timestamptz - interval '3 hours' + ((g % 7000) || ' seconds')::interval,
	         $1::timestamptz - interval '2 hours' + ((g % 7000) || ' seconds')::interval
	  FROM generate_series(1, 20000) AS g`, tA)
	registerRouteLivenessCleanup(t, db, "scope-bulk-"+suffix, "repo-bulk")
	if _, err := db.ExecContext(ctx, `VACUUM ANALYZE shared_projection_intents`); err != nil {
		t.Fatalf("vacuum: %v", err)
	}

	store := idleProbeStore(db)
	latest, ok, err := store.LatestCompletedIntentTime(ctx)
	if err != nil {
		t.Fatalf("LatestCompletedIntentTime() error = %v", err)
	}
	if !ok {
		t.Fatal("LatestCompletedIntentTime() ok = false, want true (seeded completed intents)")
	}
	if !latest.Equal(tA) {
		t.Fatalf("LatestCompletedIntentTime() = %v, want %v (newest across both domains)", latest, tA)
	}

	var planLines []string
	rows, err := db.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+reachabilitystore.LatestCompletedIntentTimeSQL)
	if err != nil {
		t.Fatalf("explain probe: %v", err)
	}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		planLines = append(planLines, line)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	plan := strings.Join(planLines, "\n")
	for _, want := range []string{"Index Only Scan Backward", "shared_projection_intents_pending_idx"} {
		if !strings.Contains(plan, want) {
			t.Fatalf("probe plan missing %q:\n%s", want, plan)
		}
	}
	// Execution buffers only: planning catalog lookups vary by cache state
	// and are not the probe's cost (the production <= 50 pre-registered
	// ceiling covers the whole EXPLAIN and is verified post-deploy).
	execution, _, _ := strings.Cut(plan, "Planning:")
	if buffers := idleProbeBuffers(t, execution); buffers > 30 {
		t.Fatalf("probe execution buffers = %d, want <= 30 at fixture scale:\n%s", buffers, plan)
	}

	// Section B: the audit-miss case. The run's gate fails on a running
	// materialization work item; intents completed at T0. After the startup
	// audit learns T0, the ack flips with no new intent completion: the
	// probe-equal cycle must skip (no watermark), and only the audit may
	// project it.
	tB := base.Add(10 * time.Minute)
	scopeB := "scope-miss-" + suffix
	genB := "gen-miss-" + suffix
	repoB := "repo-miss-" + suffix
	seedLoaderGateRun(t, ctx, db, suffix, loaderGateRun{
		name:        "miss",
		workItems:   map[string]string{"code_call_materialization": "running", "inheritance_materialization": "succeeded"},
		completedAt: tB,
	})
	runner := idleProbeRunner(db)
	if result, err := runner.ProcessNextCycle(ctx, tB.Add(time.Minute)); err != nil {
		t.Fatalf("miss startup cycle error = %v", err)
	} else if result.InputsProcessed != 0 {
		t.Fatalf("miss startup InputsProcessed = %d, want 0 (gate fails on the running item)", result.InputsProcessed)
	}
	if _, found := idleProbeWatermark(t, ctx, db, scopeB, genB, repoB); found {
		t.Fatal("watermark exists after the startup audit, want none (run not ready)")
	}
	exec(`UPDATE fact_work_items SET status='succeeded', updated_at=$1
	  WHERE scope_id=$2 AND generation_id=$3 AND domain='code_call_materialization'`,
		tB.Add(2*time.Minute), scopeB, genB)
	if result, err := runner.ProcessNextCycle(ctx, tB.Add(3*time.Minute)); err != nil {
		t.Fatalf("miss probe-equal cycle error = %v", err)
	} else if result.InputsProcessed != 0 {
		t.Fatalf("miss probe-equal InputsProcessed = %d, want 0 (no cursor advance, audit not due)", result.InputsProcessed)
	}
	if _, found := idleProbeWatermark(t, ctx, db, scopeB, genB, repoB); found {
		t.Fatal("watermark exists after the probe-equal cycle: the audit-miss case was projected without an audit")
	}
	runner.Config.AuditInterval = time.Nanosecond
	if result, err := runner.ProcessNextCycle(ctx, tB.Add(4*time.Minute)); err != nil {
		t.Fatalf("miss audit cycle error = %v", err)
	} else if result.InputsProcessed != 1 {
		t.Fatalf("miss audit InputsProcessed = %d, want 1 (audit finds the ack-flipped run)", result.InputsProcessed)
	}
	if watermark, found := idleProbeWatermark(t, ctx, db, scopeB, genB, repoB); !found {
		t.Fatal("no watermark after the audit cycle, want the ack-flipped run projected")
	} else if !watermark.Equal(tB) {
		t.Fatalf("watermark updated_at = %v, want completed_at %v", watermark, tB)
	}

	// Section C: intent-driven pickup. A pending intent completes mid-idle;
	// the next cycle must project without waiting for the audit.
	tC := base.Add(20 * time.Minute)
	scopeC := "scope-pickup-" + suffix
	genC := "gen-pickup-" + suffix
	repoC := "repo-pickup-" + suffix
	keyC := "pickup-" + suffix
	seedLoaderGateRun(t, ctx, db, suffix, loaderGateRun{name: "pickup", pending: "code_calls", completedAt: tC})
	runnerC := idleProbeRunner(db)
	if result, err := runnerC.ProcessNextCycle(ctx, tC.Add(time.Minute)); err != nil {
		t.Fatalf("pickup gate-blocked cycle error = %v", err)
	} else if result.InputsProcessed != 0 {
		t.Fatalf("pickup gate-blocked InputsProcessed = %d, want 0 (pending intent blocks the gate)", result.InputsProcessed)
	}
	tC2 := base.Add(25 * time.Minute)
	exec(`UPDATE shared_projection_intents SET completed_at=$1
	  WHERE intent_id=$2`, tC2, "intent-pending-"+keyC)
	if result, err := runnerC.ProcessNextCycle(ctx, tC2.Add(time.Minute)); err != nil {
		t.Fatalf("pickup cycle error = %v", err)
	} else if result.InputsProcessed != 1 {
		t.Fatalf("pickup InputsProcessed = %d, want 1 (fresh completion projects without audit)", result.InputsProcessed)
	}
	if watermark, found := idleProbeWatermark(t, ctx, db, scopeC, genC, repoC); !found {
		t.Fatal("no watermark after the pickup cycle")
	} else if !watermark.Equal(tC2) {
		t.Fatalf("watermark updated_at = %v, want completed_at %v", watermark, tC2)
	}

	// Section D: two concurrent runners over the same ready run converge on
	// one identical watermark without error (claim-free idempotent
	// DELETE+INSERT partition writes are unchanged by #10870).
	tD := base.Add(30 * time.Minute)
	scopeD := "scope-conc-" + suffix
	genD := "gen-conc-" + suffix
	repoD := "repo-conc-" + suffix
	seedLoaderGateRun(t, ctx, db, suffix, loaderGateRun{name: "conc", completedAt: tD})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := idleProbeRunner(db)
			_, errs[i] = r.ProcessNextCycle(ctx, tD.Add(time.Minute))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent runner %d error = %v", i, err)
		}
	}
	var watermarkCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM code_reachability_repository_watermarks
	  WHERE scope_id=$1 AND generation_id=$2 AND repository_id=$3`,
		scopeD, genD, repoD).Scan(&watermarkCount); err != nil {
		t.Fatalf("count watermarks: %v", err)
	}
	if watermarkCount != 1 {
		t.Fatalf("watermark rows = %d, want exactly 1 (convergent idempotent writes)", watermarkCount)
	}
	if watermark, found := idleProbeWatermark(t, ctx, db, scopeD, genD, repoD); !found {
		t.Fatal("no watermark after concurrent runners")
	} else if !watermark.Equal(tD) {
		t.Fatalf("watermark updated_at = %v, want completed_at %v", watermark, tD)
	}
}
