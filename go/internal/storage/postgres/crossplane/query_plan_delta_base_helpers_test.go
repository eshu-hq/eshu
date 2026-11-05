// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplanestore_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/crossplane"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// crossplaneRedriveQueryRecorder records every statement and its arguments
// a FactStore runs, so the plan proof EXPLAINs the exact derived text the
// collaborator ships instead of a copy.
type crossplaneRedriveQueryRecorder struct {
	db.ExecQueryer
	mu    sync.Mutex
	texts []string
	args  [][]any
}

func (r *crossplaneRedriveQueryRecorder) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	r.mu.Lock()
	r.texts = append(r.texts, query)
	r.args = append(r.args, slices.Clone(args))
	r.mu.Unlock()
	return r.ExecQueryer.QueryContext(ctx, query, args...)
}

// crossplaneRedriveDeltaActivateEven turns every even plan-corpus claim scope
// delta-active: its full generation is stamped at c0 and superseded, and an
// active delta at c1 with baseline c0 (touching none of its claims) becomes
// the scope's active generation.
func crossplaneRedriveDeltaActivateEven(ctx context.Context, t *testing.T, conn crossplaneRedriveExecer, now time.Time) {
	t.Helper()
	const even = `scope_id LIKE 'scope-claim-%' AND (substring(scope_id from 13)::int % 2) = 0`
	crossplaneRedriveMustExec(ctx, t, conn, `UPDATE scope_generations SET source_commit_sha = 'c0' WHERE scope_id LIKE 'scope-claim-%'`)
	crossplaneRedriveMustExec(ctx, t, conn, `
		INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, source_commit_sha, delta_baseline_commit_sha, delta_union_base_commit_sha, observed_at, ingested_at, status, payload)
		SELECT scope_id || '-delta', scope_id, 'sync', TRUE, 'c1', 'c0', 'c0', $1, $1, 'pending', '{}'::jsonb
		FROM ingestion_scopes WHERE `+even, now)
	crossplaneRedriveMustExec(ctx, t, conn, `UPDATE scope_generations SET status = 'superseded' WHERE status = 'active' AND NOT is_delta AND `+even)
	crossplaneRedriveMustExec(ctx, t, conn, `UPDATE scope_generations SET status = 'active', activated_at = $1 WHERE is_delta`, now)
	crossplaneRedriveMustExec(ctx, t, conn, `UPDATE ingestion_scopes AS s SET active_generation_id = g.generation_id FROM scope_generations AS g WHERE g.scope_id = s.scope_id AND g.is_delta`)
	crossplaneRedriveMustExec(ctx, t, conn, `ANALYZE fact_records; ANALYZE ingestion_scopes; ANALYZE scope_generations;`)
}

// crossplaneRedriveAllFullSetup is the all-full counterfactual, run inside a
// rolled-back transaction: every delta-active scope points back at its full.
const crossplaneRedriveAllFullSetup = `UPDATE scope_generations SET status = 'superseded' WHERE is_delta;
UPDATE scope_generations SET status = 'active' WHERE NOT is_delta AND scope_id IN (SELECT scope_id FROM scope_generations WHERE is_delta);
UPDATE ingestion_scopes AS s SET active_generation_id = g.generation_id FROM scope_generations AS g WHERE g.scope_id = s.scope_id AND NOT g.is_delta AND g.scope_id IN (SELECT scope_id FROM scope_generations WHERE is_delta)`

// explainCrossplaneRedrive runs EXPLAIN (ANALYZE, BUFFERS) once in a
// rolled-back transaction after setup, returning the text plan and the
// execution time in milliseconds.
func explainCrossplaneRedrive(ctx context.Context, t *testing.T, conn *sql.Conn, setup, query string, args ...any) (string, float64) {
	t.Helper()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin explain: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if setup != "" {
		if _, err := tx.ExecContext(ctx, setup); err != nil {
			t.Fatalf("explain setup: %v", err)
		}
	}
	var raw string
	if err := tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("explain: %v", err)
	}
	var doc []map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	rows, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+query, args...)
	if err != nil {
		t.Fatalf("explain text: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	return plan.String(), doc[0]["Execution Time"].(float64)
}

// p50CrossplaneRedrive returns the median execution time of 15 runs.
func p50CrossplaneRedrive(ctx context.Context, t *testing.T, conn *sql.Conn, setup, query string, args ...any) float64 {
	t.Helper()
	times := make([]float64, 0, 15)
	for range 15 {
		_, ms := explainCrossplaneRedrive(ctx, t, conn, setup, query, args...)
		times = append(times, ms)
	}
	slices.Sort(times)
	return times[7]
}

// assertCrossplaneRedriveIndexedPlan fails unless plan uses the redrive
// partial index and never sequentially scans fact_records.
func assertCrossplaneRedriveIndexedPlan(t *testing.T, label, plan string) {
	t.Helper()
	if !strings.Contains(plan, "fact_records_active_k8s_claim_redrive_idx") {
		t.Fatalf("%s plan does not use fact_records_active_k8s_claim_redrive_idx:\n%s", label, plan)
	}
	if strings.Contains(plan, "Seq Scan on fact_records") {
		t.Fatalf("%s plan sequentially scans fact_records:\n%s", label, plan)
	}
}

// crossplaneRedriveIndexCondHasGenerationANY reports whether the Index
// Cond of the redrive index scan binds generation_id = ANY.
func crossplaneRedriveIndexCondHasGenerationANY(plan string) bool {
	lines := strings.Split(plan, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "using fact_records_active_k8s_claim_redrive_idx") {
			continue
		}
		for _, next := range lines[i+1:] {
			if strings.Contains(next, "Index Cond:") {
				return strings.Contains(next, "generation_id = ANY")
			}
		}
	}
	return false
}

// testCrossplaneRedriveDeltaBasePlanLive extends the plan corpus with
// delta-active scopes (#10094): every even claim scope, so 30 of the 60
// matching scopes keep their claims only in the base, and the base leg
// binds 2000 candidate base generation ids. The base text is the one the
// real postgres.FactStore collaborator derives and runs.
func testCrossplaneRedriveDeltaBasePlanLive(ctx context.Context, t *testing.T, database *sql.DB, conn *sql.Conn, group, kind, xrdScopeID string) {
	crossplaneRedriveDeltaActivateEven(ctx, t, conn, time.Now().UTC())

	recorder := &crossplaneRedriveQueryRecorder{ExecQueryer: postgres.SQLDB{DB: database}}
	store := postgres.NewFactStore(recorder)
	lookupStarted := time.Now()
	baseGenerations, err := store.CrossplaneRedriveDeltaBaseGenerations(ctx)
	lookupWall := time.Since(lookupStarted)
	if err != nil || len(baseGenerations) != 2000 {
		t.Fatalf("CrossplaneRedriveDeltaBaseGenerations() = (%d generations, %v), want 2000", len(baseGenerations), err)
	}
	page := crossplanestore.CrossplaneRedriveTargetPage{Group: group, ClaimKind: kind, XRDScopeID: xrdScopeID, Limit: 500}
	targets, err := store.ListCrossplaneRedriveDeltaBaseTargetScopes(ctx, page, baseGenerations)
	if err != nil || len(targets) != 30 {
		t.Fatalf("delta base targets = (%d, %v), want the 30 delta-active matching scopes", len(targets), err)
	}
	for _, target := range targets {
		if target.GenerationID != target.ScopeID+"-delta" {
			t.Fatalf("delta base target %+v does not carry its active delta generation", target)
		}
	}
	if len(recorder.texts) != 2 {
		t.Fatalf("collaborator ran %d statements, want the lookup and one base page", len(recorder.texts))
	}
	lookupText, baseText, baseArgs := recorder.texts[0], recorder.texts[1], recorder.args[1]

	basePlan, _ := explainCrossplaneRedrive(ctx, t, conn, "", baseText, baseArgs...)
	t.Logf("=== delta base leg (custom plan) ===\n%s", basePlan)
	assertCrossplaneRedriveIndexedPlan(t, "delta base", basePlan)
	// The candidate base generation ids must reach the index scan as an
	// index condition, not a per-row filter over every matching claim.
	if !crossplaneRedriveIndexCondHasGenerationANY(basePlan) {
		t.Fatalf("delta base plan does not push the generation_id = ANY into the redrive index condition:\n%s", basePlan)
	}
	activeArgs := page.QueryArgs()
	activePlan, _ := explainCrossplaneRedrive(ctx, t, conn, "", crossplanestore.ListCrossplaneRedriveTargetScopesQuery, activeArgs...)
	assertCrossplaneRedriveIndexedPlan(t, "active (delta corpus)", activePlan)

	// Generic plans: pgx caches named statements, so after five runs the
	// server may switch to a generic plan; both legs must stay indexed.
	literal := "'{" + strings.Join(baseGenerations, ",") + "}'"
	call := fmt.Sprintf("('%s','%s','%s','',500", group, kind, xrdScopeID)
	if _, err := conn.ExecContext(ctx, "PREPARE redrive_base(text,text,text,text,int,text[]) AS "+baseText); err != nil {
		t.Fatalf("prepare base: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "PREPARE redrive_active(text,text,text,text,int) AS "+crossplanestore.ListCrossplaneRedriveTargetScopesQuery); err != nil {
		t.Fatalf("prepare active: %v", err)
	}
	p50 := map[string]float64{}
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		setup := "SET LOCAL plan_cache_mode = " + mode
		for name, execute := range map[string]string{"base": "EXECUTE redrive_base" + call + "," + literal + ")", "active": "EXECUTE redrive_active" + call + ")"} {
			plan, _ := explainCrossplaneRedrive(ctx, t, conn, setup, execute)
			assertCrossplaneRedriveIndexedPlan(t, name+" "+mode, plan)
			p50[name+" "+mode] = p50CrossplaneRedrive(ctx, t, conn, setup, execute)
		}
	}
	p50["active"] = p50CrossplaneRedrive(ctx, t, conn, "", crossplanestore.ListCrossplaneRedriveTargetScopesQuery, activeArgs...)
	p50["base"] = p50CrossplaneRedrive(ctx, t, conn, "", baseText, baseArgs...)
	p50["lookup"] = p50CrossplaneRedrive(ctx, t, conn, "", lookupText)
	p50["all-full counterfactual"] = p50CrossplaneRedrive(ctx, t, conn, crossplaneRedriveAllFullSetup, crossplanestore.ListCrossplaneRedriveTargetScopesQuery, activeArgs...)
	names := slices.Sorted(func(yield func(string) bool) {
		for name := range p50 {
			if !yield(name) {
				return
			}
		}
	})
	for _, name := range names {
		t.Logf("p50 %-32s %8.3f ms", name, p50[name])
	}
	t.Logf("lookup wall (one call, pgx) %s; base/active %.2fx; added (lookup+base) %.3f ms; (active+base)/counterfactual %.2fx",
		lookupWall, p50["base"]/p50["active"], p50["lookup"]+p50["base"],
		(p50["active"]+p50["base"])/p50["all-full counterfactual"])
}
