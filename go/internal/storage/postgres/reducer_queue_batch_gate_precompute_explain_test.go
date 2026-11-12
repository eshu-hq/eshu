// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestClaimBatchGatePrecomputeStaleHeavyExplain is the C-2 proof for #10565
// step 1: on a stale-generation-heavy fixture (where the supersede sweep
// carries real UPDATE volume), the FULL claim — not only the candidate
// SELECT — must show the per-row SubPlan gate expressions gone.
//
//   - the OLD candidate plan still evaluates per-row SubPlans on this
//     fixture (non-vacuous: the shape the rewrite removes is present), and
//   - the NEW candidate and FULL-claim plans evaluate zero SubPlans, while
//     the full-claim Update node still sweeps the stale volume, and
//   - the FUNNEL plan evaluates zero Update/LockRows nodes (F1: the census
//     replays the sweep read-only instead of re-running its UPDATE).
//
// Every EXPLAIN ANALYZE runs inside a transaction that rolls back, so the
// sweep UPDATE the plans execute never commits and every side observes the
// identical fixture state. Timings and buffer counts are logged as evidence,
// not asserted as gates (single-sample wall time is not a proof, and neither
// fixture is production scale: see the production-scale caveat below).
//
// Skipped unless a DSN is provided, like the other claim proofs.
func TestClaimBatchGatePrecomputeStaleHeavyExplain(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the stale-heavy EXPLAIN proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)
	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	const staleRows = 1200
	seedStaleHeavyFixture(t, ctx, db, now, staleRows)

	oldQuery := frozenPreChangeCandidate(t)
	newQuery, ok := deriveReadOnlyCandidateSelect(claimReducerWorkBatchQuery)
	if !ok {
		t.Fatal("could not derive read-only candidate SELECT from the shipped batch claim query")
	}

	clock := now.Add(2 * time.Hour)
	candArgs := []any{clock, nil, "n/a", clock.Add(time.Hour), true, 0, 0, 8}
	fullArgs := []any{clock, nil, "explain-worker", clock.Add(time.Hour), true, 0, 0, 8}

	oldPlan := explainAnalyzeRollback(t, ctx, db, oldQuery, candArgs)
	newPlan := explainAnalyzeRollback(t, ctx, db, newQuery, candArgs)
	fullPlan := explainAnalyzeRollback(t, ctx, db, claimReducerWorkBatchQuery, fullArgs)

	oldSubPlans := countPlanToken(oldPlan, "SubPlan")
	newSubPlans := countPlanToken(newPlan, "SubPlan")
	fullSubPlans := countPlanToken(fullPlan, "SubPlan")
	t.Logf("SubPlan nodes: old-candidate=%d new-candidate=%d full-claim=%d", oldSubPlans, newSubPlans, fullSubPlans)
	t.Logf("execution time: old-candidate=%s new-candidate=%s full-claim=%s",
		planExecutionTime(oldPlan), planExecutionTime(newPlan), planExecutionTime(fullPlan))
	t.Logf("buffers: old-candidate=[%s] new-candidate=[%s] full-claim=[%s]",
		strings.Join(planBufferLines(oldPlan), " / "),
		strings.Join(planBufferLines(newPlan), " / "),
		strings.Join(planBufferLines(fullPlan), " / "))

	// SubPlan totals are scoped to the CTE base section, not the whole plan.
	// Two correct shapes keep their own subplans outside base: the sweep
	// keeps its correlated readiness gates by design (step 2 defers sweep
	// precompute), and the precompute CTEs carry once-per-claim
	// uncorrelated subplans of their own (semantic_scalars runs its counts
	// as InitPlans, readiness_missing hashes its phase lookup as one
	// hashed SubPlan evaluated once). A whole-plan count therefore rises
	// even though the per-row correlated SubPlans under base are gone.
	// What step 1 removes is exactly the per-row correlated SubPlans under
	// base, so that section must go from >=1 (old) to 0 (new, full).
	oldBaseSubPlans := countPlanToken(planCTESection(t, oldPlan, "base"), "SubPlan")
	newBaseSubPlans := countPlanToken(planCTESection(t, newPlan, "base"), "SubPlan")
	t.Logf("base-section SubPlan nodes: old-candidate=%d new-candidate=%d", oldBaseSubPlans, newBaseSubPlans)
	if oldBaseSubPlans < 1 {
		t.Fatalf("old candidate base section has no SubPlan nodes on the stale-heavy fixture; "+
			"the fixture does not exercise the removed shape:\n%s", strings.Join(oldPlan, "\n"))
	}
	if newBaseSubPlans != 0 {
		t.Fatalf("new candidate base section still evaluates %d SubPlan nodes:\n%s",
			newBaseSubPlans, strings.Join(newPlan, "\n"))
	}
	for _, want := range []string{"projector_live_scopes", "readiness_missing", "semantic_ranked", "semantic_scalars"} {
		if !strings.Contains(strings.Join(newPlan, "\n"), want) {
			t.Fatalf("new candidate plan does not execute precomputed gate %q:\n%s",
				want, strings.Join(newPlan, "\n"))
		}
	}
	if fullSubPlans != newSubPlans {
		t.Fatalf("full-claim plan SubPlans (%d) differ from new-candidate SubPlans (%d); "+
			"the lock/claim tail must add no per-row gates:\n%s",
			fullSubPlans, newSubPlans, strings.Join(fullPlan, "\n"))
	}
	if fullBaseSubPlans := countPlanToken(planCTESection(t, fullPlan, "base"), "SubPlan"); fullBaseSubPlans != 0 {
		t.Fatalf("full-claim base section still evaluates %d SubPlan nodes:\n%s",
			fullBaseSubPlans, strings.Join(fullPlan, "\n"))
	}
	swept := fullClaimUpdateRows(t, fullPlan)
	t.Logf("full-claim Update node actual rows (sweep + claims): %d (seeded stale: %d)", swept, staleRows)

	// F1/F6 funnel-vs-claim pair on the same stale-heavy fixture: the
	// funnel census must show no data-modifying or locking node, and its
	// execution time is logged next to the full claim's so the evidence
	// note can record both. Neither fixture is production scale — the
	// numbers below size the funnel against the claim on identical state,
	// they do not predict production cost (net-of-funnel production scale
	// stays NOT_CHECKED until the QA-lane sustained window).
	funnelArgs := []any{clock, nil, "n/a", clock.Add(time.Hour), true, 0, 0}
	funnelPlan := explainAnalyzeRollback(t, ctx, db, reducerClaimBatchFunnelQuery, funnelArgs)
	joinedFunnel := strings.Join(funnelPlan, "\n")
	for _, banned := range []string{"Update on", "LockRows"} {
		if strings.Contains(joinedFunnel, banned) {
			t.Fatalf("funnel plan contains %q on the stale-heavy fixture; the census must stay read-only:\n%s",
				banned, joinedFunnel)
		}
	}
	t.Logf("funnel-vs-claim: funnel execution time %s, full-claim execution time %s (stale-heavy fixture, %d stale rows; not production scale)",
		planExecutionTime(funnelPlan), planExecutionTime(fullPlan), staleRows)
	t.Logf("funnel-vs-claim buffers: funnel=[%s] full-claim=[%s]",
		strings.Join(planBufferLines(funnelPlan), " / "),
		strings.Join(planBufferLines(fullPlan), " / "))
	if swept < staleRows {
		t.Fatalf("full-claim Update node swept %d rows, want at least the %d seeded stale rows; "+
			"the sweep volume this proof must cover is missing", swept, staleRows)
	}
}

// seedStaleHeavyFixture seeds one scope whose active generation holds live
// claimable rows while three older generations hold staleRows pending rows in
// total. supply_chain_impact carries no readiness requirement, so the sweep
// terminalizes the stale rows (real UPDATE volume) instead of holding them
// out on readiness.
func seedStaleHeavyFixture(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, staleRows int) {
	t.Helper()
	seedGateScope(t, ctx, db, "scope-stale-heavy", "gen-active", now.Add(3*time.Hour))
	for i := 0; i < 3; i++ {
		gen := fmt.Sprintf("gen-stale-%d", i)
		mustExecGateArgs(t, ctx, db, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ($1, 'scope-stale-heavy', 'snapshot', 'fair', $2, $2, 'superseded', $2, $2, '{}'::jsonb)`,
			gen, now.Add(time.Duration(i)*time.Hour))
	}
	insertChunk := func(gen string, start, count int) {
		t.Helper()
		var sb strings.Builder
		sb.WriteString(`INSERT INTO fact_work_items (
		    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
		    conflict_key, status, attempt_count, payload, created_at, updated_at
		) VALUES `)
		args := make([]any, 0, count*3)
		for i := 0; i < count; i++ {
			if i > 0 {
				sb.WriteString(", ")
			}
			base := len(args) + 1
			fmt.Fprintf(&sb, "($%d, 'scope-stale-heavy', '%s', 'reducer', 'supply_chain_impact', 'scope', $%d, 'pending', 0, jsonb_build_object('entity_key', $%d::text), $%d, $%d)",
				base, gen, base+1, base, base+2, base+2)
			id := fmt.Sprintf("stale-%s-%05d", gen, start+i)
			key := fmt.Sprintf("stale-key-%s-%05d", gen, start+i)
			ts := now.Add(time.Duration(start+i) * time.Second)
			args = append(args, id, key, ts)
		}
		mustExecGateArgs(t, ctx, db, sb.String(), args...)
	}
	perGen := staleRows / 3
	for i := 0; i < 3; i++ {
		insertChunk(fmt.Sprintf("gen-stale-%d", i), i*perGen, perGen)
	}
	insertChunk("gen-active", staleRows, 150)
}

// explainAnalyzeRollback runs EXPLAIN (ANALYZE, BUFFERS) for query+args
// inside a transaction that always rolls back, and returns the plan lines.
// The rollback keeps the sweep UPDATE the claim plans execute from
// committing, so repeated explains observe identical state.
func explainAnalyzeRollback(t *testing.T, ctx context.Context, db *sql.DB, query string, args []any) []string {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin explain tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+query, args...)
	if err != nil {
		t.Fatalf("explain analyze: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain line: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate explain lines: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("explain returned no plan lines")
	}
	return lines
}

var planExecutionTimeRe = regexp.MustCompile(`Execution Time: ([\d.]+) ms`)

func planExecutionTime(plan []string) string {
	for _, line := range plan {
		if m := planExecutionTimeRe.FindStringSubmatch(line); m != nil {
			return m[1] + " ms"
		}
	}
	return "unknown"
}

func planBufferLines(plan []string) []string {
	var out []string
	for _, line := range plan {
		if strings.Contains(line, "Buffers:") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

func countPlanToken(plan []string, token string) int {
	n := 0
	for _, line := range plan {
		n += strings.Count(line, token)
	}
	return n
}

// planCTESection returns the EXPLAIN lines of one top-level CTE section:
// from the "CTE <name>" header to (excluding) the next top-level "CTE ..."
// header. "CTE Scan on ..." probe lines inside the section do not end it.
func planCTESection(t *testing.T, plan []string, name string) []string {
	t.Helper()
	start := -1
	for i, line := range plan {
		if strings.TrimSpace(line) == "CTE "+name {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("explain plan has no CTE %q section:\n%s", name, strings.Join(plan, "\n"))
	}
	end := len(plan)
	for i := start + 1; i < len(plan); i++ {
		trimmed := strings.TrimSpace(plan[i])
		if strings.HasPrefix(trimmed, "CTE ") && !strings.HasPrefix(trimmed, "CTE Scan") {
			end = i
			break
		}
	}
	return plan[start:end]
}

var fullClaimUpdateRowsRe = regexp.MustCompile(`Update on fact_work_items.*rows=([\d.]+) loops=`)

// fullClaimUpdateRows sums actual rows across every Update on fact_work_items
// node in the full-claim plan: the sweep UPDATE plus the claimed rows. It
// proves the explained full claim really executed the sweep volume instead
// of planning it away. Summing (not first-match) keeps the count independent
// of CTE plan-print order.
func fullClaimUpdateRows(t *testing.T, plan []string) int {
	t.Helper()
	total := 0
	for _, line := range plan {
		if m := fullClaimUpdateRowsRe.FindStringSubmatch(line); m != nil {
			f, err := strconv.ParseFloat(m[1], 64)
			if err != nil {
				t.Fatalf("parse Update actual rows %q: %v", m[1], err)
			}
			total += int(f)
		}
	}
	if total == 0 {
		t.Fatalf("full-claim plan has no Update on fact_work_items rows:\n%s", strings.Join(plan, "\n"))
	}
	return total
}
