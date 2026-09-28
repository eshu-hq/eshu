// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

// generationRetentionCandidateQuerySeededPerRowLiveWork is a guard-sensitivity
// fixture only, never shipped: it mechanically un-materializes live_work back
// into the per-row correlated NOT EXISTS form T1 rejected (arb-7334c.md
// section 4, cold-plan buffers 230,005 -> 2,372,599 at 10x fact_work_items).
// Derived from the shipped constant by two exact substring replacements, not
// hand-copied, so it tracks the production query's other text.
var generationRetentionCandidateQuerySeededPerRowLiveWork = mustSeedPerRowLiveWork(generationRetentionCandidateQuery)

func mustSeedPerRowLiveWork(shipped string) string {
	withoutCTE := strings.Replace(shipped, `live_work AS MATERIALIZED (
    SELECT DISTINCT work.generation_id
    FROM fact_work_items AS work
    WHERE work.status IN ('claimed', 'running', 'retrying')
),
`, "", 1)
	if withoutCTE == shipped {
		panic("mustSeedPerRowLiveWork: live_work CTE text not found in the shipped query")
	}
	perRow := strings.Replace(withoutCTE, `NOT EXISTS (
          SELECT 1
          FROM live_work
          WHERE live_work.generation_id = ranked.generation_id
      )`, `NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS work
          WHERE work.generation_id = ranked.generation_id
            AND work.status IN ('claimed', 'running', 'retrying')
      )`, 1)
	if perRow == withoutCTE {
		panic("mustSeedPerRowLiveWork: live_work NOT EXISTS text not found in the shipped query")
	}
	return perRow
}

// TestGenerationRetentionCandidatePlanNeverLoopsFactWorkItemsLive is P12: the
// live_work CTE must read fact_work_items exactly once (Actual Loops = 1)
// regardless of plan state (never analyzed, forced generic cold, forced
// generic analyzed, custom analyzed). A seeded reversion to the per-row
// correlated form (mustSeedPerRowLiveWork), checked cold, must make the same
// guard fail, so the guard is proven able to see the #6809-class defect it
// exists to catch; after ANALYZE, Postgres can decorrelate either SQL text
// into the same single-scan plan on a fixture this small, so the seeded
// check only holds cold, matching T1's own finding.
func TestGenerationRetentionCandidatePlanNeverLoopsFactWorkItemsLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	cutoff := now.Add(-7 * 24 * time.Hour)
	old := now.Add(-10 * 24 * time.Hour)
	newer := now.Add(-9 * 24 * time.Hour)

	for i := 0; i < 10; i++ {
		scopeID := retentionSelectionScopeID("plan", i)
		seedRetentionSelectionScope(t, ctx, database, scopeID)
		seedRetentionSelectionSupersededGeneration(t, ctx, database, scopeID, scopeID+"-g0", old)
		seedRetentionSelectionSupersededGeneration(t, ctx, database, scopeID, scopeID+"-g1", newer)
	}
	// A handful of live fact_work_items rows, on the newer (rank 1, already
	// ineligible by rank) generation of a few scopes, so the live_work CTE
	// reads a nonzero but bounded live set through fact_work_items_status_idx
	// without changing which generations are eligible.
	for i := 0; i < 5; i++ {
		scopeID := retentionSelectionScopeID("plan", i)
		seedRetentionSelectionLiveWork(t, ctx, database, scopeID, scopeID+"-g1")
	}

	check := func(t *testing.T, query string, explain func(string) []byte) (violations []string) {
		t.Helper()
		plan := explain(query)
		return candidateFactWorkItemsPlanViolations(t, plan)
	}

	t.Run("shipped/never-analyzed-custom", func(t *testing.T) {
		if v := check(t, generationRetentionCandidateQuery, func(q string) []byte {
			return explainCandidateCustom(t, ctx, database, q, cutoff)
		}); len(v) > 0 {
			t.Errorf("violations: %v", v)
		}
	})
	t.Run("shipped/forced-generic-cold", func(t *testing.T) {
		if v := check(t, generationRetentionCandidateQuery, func(q string) []byte {
			return explainCandidateGeneric(t, ctx, database, q, cutoff)
		}); len(v) > 0 {
			t.Errorf("violations: %v", v)
		}
	})
	// Cold (no planner statistics): T1 found the rejected per-row form's
	// fact_work_items node looping once per ranked row specifically in the
	// unanalyzed states (arb-7334c.md section 4: 230,005 buffers cold vs.
	// 1,171 after ANALYZE). After ANALYZE, Postgres can recognize a
	// correlated NOT EXISTS as decorrelatable and hash-anti-join it into a
	// single scan regardless of which SQL text produced it, on a fixture
	// this small, so this seeded check must run before analyzeCandidateProbeTables.
	t.Run("seeded-red-per-row-live-work-cold", func(t *testing.T) {
		v := check(t, generationRetentionCandidateQuerySeededPerRowLiveWork, func(q string) []byte {
			return explainCandidateGeneric(t, ctx, database, q, cutoff)
		})
		if len(v) == 0 {
			t.Fatal("plan guard accepted the seeded per-row live_work reversion cold, want a violation (fact_work_items must loop once per ranked row)")
		}
	})
	analyzeCandidateProbeTables(t, ctx, database)
	t.Run("shipped/forced-generic-analyzed", func(t *testing.T) {
		if v := check(t, generationRetentionCandidateQuery, func(q string) []byte {
			return explainCandidateGeneric(t, ctx, database, q, cutoff)
		}); len(v) > 0 {
			t.Errorf("violations: %v", v)
		}
	})
	t.Run("shipped/custom-analyzed", func(t *testing.T) {
		if v := check(t, generationRetentionCandidateQuery, func(q string) []byte {
			return explainCandidateCustom(t, ctx, database, q, cutoff)
		}); len(v) > 0 {
			t.Errorf("violations: %v", v)
		}
	})
}

// candidateFactWorkItemsPlanViolations reports any fact_work_items access
// node whose Actual Loops is not 1: at any table size the materialized
// live_work CTE reads fact_work_items exactly once, while the rejected
// per-row correlated NOT EXISTS form re-executes it once per outer ranked
// row it examines. This is the discriminating invariant, not the chosen
// access method (Seq Scan vs. index): on a small fixture the planner
// correctly picks a Seq Scan for either shape, so asserting "no Seq Scan"
// here would not distinguish them; T1's own proof (arb-7334c.md section 4)
// establishes the Seq-Scan/buffers-growth claim at production scale. It also
// requires exactly one WindowAgg node (the single ROW_NUMBER pass).
func candidateFactWorkItemsPlanViolations(t *testing.T, plan []byte) []string {
	t.Helper()
	var root []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(plan, &root); err != nil || len(root) != 1 {
		t.Fatalf("decode plan: %v\n%s", err, plan)
	}
	var violations []string
	var windowAggs int
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		nodeType, _ := node["Node Type"].(string)
		relation, _ := node["Relation Name"].(string)
		if nodeType == "WindowAgg" {
			windowAggs++
		}
		if relation == "fact_work_items" {
			if loops, ok := node["Actual Loops"].(float64); ok && loops != 1 {
				violations = append(violations, "fact_work_items node executed with Actual Loops="+jsonNumber(loops)+", want 1")
			}
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			if childNode, ok := child.(map[string]any); ok {
				walk(childNode)
			}
		}
	}
	walk(root[0].Plan)
	if windowAggs != 1 {
		violations = append(violations, "WindowAgg count = "+jsonNumber(float64(windowAggs))+", want 1")
	}
	return violations
}

func jsonNumber(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// explainCandidateCustom plans query with cutoff, minSuperseded 0, limit 100
// bound directly, the shape a one-off (non-prepared) execution settles on. It
// runs inside a transaction that is always rolled back, so the row locks the
// query's own FOR UPDATE clauses take are released.
func explainCandidateCustom(t *testing.T, ctx context.Context, database *sql.DB, query string, cutoff time.Time) []byte {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	var plan []byte
	if err := tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, FORMAT JSON, BUFFERS) "+query, cutoff, 0, 100).Scan(&plan); err != nil {
		t.Fatalf("explain custom plan: %v", err)
	}
	return plan
}

// explainCandidateGeneric plans query as a prepared statement under
// plan_cache_mode=force_generic_plan, the plan a reused prepared statement
// settles on, which cannot see the bound cutoff value.
func explainCandidateGeneric(t *testing.T, ctx context.Context, database *sql.DB, query string, cutoff time.Time) []byte {
	t.Helper()
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "BEGIN; SET LOCAL plan_cache_mode = force_generic_plan; "+
		"PREPARE retention_candidate_probe(timestamptz, int, int) AS "+query); err != nil {
		t.Fatalf("prepare generic plan: %v", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "DEALLOCATE retention_candidate_probe; ROLLBACK")
	}()
	// A literal, not a bound arg: EXECUTE's arguments are plain SQL text, and
	// the outer EXPLAIN call must issue no bind parameters of its own so pgx
	// does not try to reconcile them against the already-prepared statement.
	literal := "'" + cutoff.UTC().Format(time.RFC3339Nano) + "'::timestamptz"
	var plan []byte
	if err := conn.QueryRowContext(ctx,
		"EXPLAIN (ANALYZE, FORMAT JSON, BUFFERS) EXECUTE retention_candidate_probe("+literal+", 0, 100)",
	).Scan(&plan); err != nil {
		t.Fatalf("explain generic plan: %v", err)
	}
	return plan
}

// analyzeCandidateProbeTables refreshes planner statistics for the tables the
// candidate query reads, matching an operated database rather than a
// freshly bulk-loaded one.
func analyzeCandidateProbeTables(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for _, table := range []string{"scope_generations", "ingestion_scopes", "fact_work_items"} {
		if _, err := database.ExecContext(ctx, "ANALYZE "+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
}
