// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestSupplyChainImpactWinnersResweepDigestExcludedFieldsChangeNothingLive proves the same fact on
// PostgreSQL: rewriting every digest-excluded payload key leaves the winners
// select output unchanged, so a full resweep writes nothing.
func TestSupplyChainImpactWinnersResweepDigestExcludedFieldsChangeNothingLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 digest-excluded fields proof")
	scopeA, _ := keyedSeedSettled(h)
	for _, key := range supplychaincore.DigestExcludedPayloadKeys() {
		h.exec(`UPDATE fact_records SET payload = jsonb_set(payload, ARRAY[$2]::text[], to_jsonb('rewritten-'||$2::text), true) WHERE scope_id = $1`, scopeA, key)
	}
	h.exec(`UPDATE fact_records SET payload = jsonb_set(payload, '{provenance,content_digest}', '"another"', true) WHERE scope_id = $1`, scopeA)
	got, err := h.store.ResweepWinners(h.ctx, h.tick())
	if err != nil || got.Inserted != 0 || got.Updated != 0 || got.Deleted != 0 {
		t.Fatalf("full resweep after rewriting digest-excluded fields = %+v, %v; want no winner written", got, err)
	}
}

// TestSupplyChainImpactWinnersResweepRetentionDeleteChangesNoWinnerLive proves that a
// retention delete of a superseded generation, which needs no capture, moves no
// winner.
func TestSupplyChainImpactWinnersResweepRetentionDeleteChangesNoWinnerLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 retention proof")
	_, scopeB := keyedSeedSettled(h)
	h.addGeneration(scopeB, scopeB+"/g2", "pending")
	h.write(scopeB, scopeB+"/g2", keyedFinding("CVE-2026-2001", "pkg:npm/x", 8))
	h.activate(scopeB, scopeB+"/g1", scopeB+"/g2")
	h.keyed()
	h.assertParity("after the flip")

	h.exec(`DELETE FROM fact_records WHERE scope_id = $1 AND generation_id = $2`, scopeB, scopeB+"/g1")
	got, err := h.store.ResweepWinners(h.ctx, h.tick())
	if err != nil || got.Inserted != 0 || got.Updated != 0 || got.Deleted != 0 {
		t.Fatalf("full resweep after deleting a superseded generation = %+v, %v; want no winner written", got, err)
	}
}

// TestSupplyChainImpactWinnersResweepKeyedProbePlanLive asserts the plan shape the
// ruling requires at scale: after the identifier temp table is analyzed, the
// active_facts build probes the existing package-lookup index once per DISTINCT
// triple (not once per identifier: a hot triple's fifty dirty keys must not
// fetch its whole fact set fifty times) and never scans fact_records.
func TestSupplyChainImpactWinnersResweepKeyedProbePlanLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 keyed plan proof")
	h.seedScope("vuln-intel://9805/plan", "vuln-intel://9805/plan/g1")
	h.exec(`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'plan-' || g, 'vuln-intel://9805/plan', 'vuln-intel://9805/plan/g1',
       'reducer_supply_chain_impact_finding', 'plan-' || g, '1.0.0', 'synthetic', 'plan-' || g, now(), now(), false,
       jsonb_build_object('package_id', 'pkg:npm/p' || (g % 4000), 'repository_id', 'repo-' || (g % 50),
                          'subject_digest', '', 'cve_id', 'CVE-2026-' || g, 'advisory_id', '', 'impact_status', 'affected_exact')
FROM generate_series(1, 40000) AS g`)
	// A hot triple: 3,000 candidates behind one (package, repository, digest).
	h.exec(`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'hot-' || g, 'vuln-intel://9805/plan', 'vuln-intel://9805/plan/g1',
       'reducer_supply_chain_impact_finding', 'hot-' || g, '1.0.0', 'synthetic', 'hot-' || g, now(), now(), false,
       jsonb_build_object('package_id', 'pkg:npm/hot', 'repository_id', 'repo-hot',
                          'subject_digest', '', 'cve_id', 'CVE-2026-H' || g, 'advisory_id', '', 'impact_status', 'affected_exact')
FROM generate_series(1, 3000) AS g`)
	h.exec(`ANALYZE fact_records`)

	conn, err := h.db.Conn(h.ctx)
	if err != nil {
		t.Fatalf("connection: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	for _, stmt := range []string{
		`CREATE TEMP TABLE ` + postgres.WinnersKeyedIdentsTableForTest + ` (package_id text, repository_id text, subject_digest text, c0 text, advisory_id text)`,
		`INSERT INTO ` + postgres.WinnersKeyedIdentsTableForTest + ` SELECT 'pkg:npm/p' || g, 'repo-' || (g % 50), '', 'CVE-2026-' || g, '' FROM generate_series(1, 20) g`,
		`INSERT INTO ` + postgres.WinnersKeyedIdentsTableForTest + ` SELECT 'pkg:npm/hot', 'repo-hot', '', 'CVE-2026-H' || g, '' FROM generate_series(1, 30) g`,
		`ANALYZE ` + postgres.WinnersKeyedIdentsTableForTest,
	} {
		if _, err := conn.ExecContext(h.ctx, stmt); err != nil {
			t.Fatalf("prepare identifiers %q: %v", stmt, err)
		}
	}
	var planJSON []byte
	if err := conn.QueryRowContext(h.ctx, `EXPLAIN (FORMAT JSON) `+postgres.WinnersKeyedActiveFactsSQLForTest).Scan(&planJSON); err != nil {
		t.Fatalf("explain probe: %v", err)
	}
	plan := string(planJSON)
	var parsed any
	if err := json.Unmarshal(planJSON, &parsed); err != nil {
		t.Fatalf("plan is not JSON: %v", err)
	}
	if !strings.Contains(plan, "fact_records_supply_chain_impact_package_lookup_idx") {
		t.Errorf("the keyed probe does not use the package-lookup index:\n%s", plan)
	}
	if regexp.MustCompile(`"Node Type":\s*"Seq Scan"[^}]*"Relation Name":\s*"fact_records"`).MatchString(plan) {
		t.Errorf("the keyed probe seq-scans fact_records:\n%s", plan)
	}

	// 21 distinct triples (20 ordinary, one hot) carry 50 identifiers. The lookup
	// index must be searched once per triple, so the hot triple's 3,000 candidates
	// are fetched once, not thirty times. The plan must hold in both statistics
	// states that mattered: the scope tables as the isolated schema leaves them
	// (default estimates, which flipped the per-identifier form into a scope loop
	// with 100 searches), and after ANALYZE of the scope tables. The probe sits in
	// a MATERIALIZED CTE, so the planner has no join order to choose for it.
	for _, state := range []string{"default scope-table statistics", "analyzed scope tables"} {
		if state == "analyzed scope tables" {
			if _, err := conn.ExecContext(h.ctx, `ANALYZE ingestion_scopes`); err != nil {
				t.Fatalf("analyze ingestion_scopes: %v", err)
			}
			if _, err := conn.ExecContext(h.ctx, `ANALYZE scope_generations`); err != nil {
				t.Fatalf("analyze scope_generations: %v", err)
			}
		}
		if _, err := conn.ExecContext(h.ctx, `DROP TABLE IF EXISTS `+postgres.WinnersKeyedActiveFactsTableForTest); err != nil {
			t.Fatalf("drop the previous explain output: %v", err)
		}
		var analyzed []byte
		if err := conn.QueryRowContext(h.ctx, `EXPLAIN (ANALYZE, FORMAT JSON) `+postgres.WinnersKeyedActiveFactsSQLForTest).Scan(&analyzed); err != nil {
			t.Fatalf("%s: explain analyze probe: %v", state, err)
		}
		var analyzedPlan any
		if err := json.Unmarshal(analyzed, &analyzedPlan); err != nil {
			t.Fatalf("%s: analyzed plan is not JSON: %v", state, err)
		}
		loops, found := lookupIndexLoops(analyzedPlan)
		if !found {
			t.Fatalf("%s: the analyzed plan has no scan of the package-lookup index:\n%s", state, analyzed)
		}
		if loops != 21 {
			t.Errorf("%s: the package-lookup index was searched %v times, want 21 (once per distinct triple, not once per identifier):\n%s", state, loops, analyzed)
		}
		if !strings.Contains(string(analyzed), `"Subplan Name": "CTE triple_facts"`) {
			t.Errorf("%s: the probe is not a materialized CTE node:\n%s", state, analyzed)
		}
	}
}

// lookupIndexLoops returns the "Actual Loops" of the first scan of the
// package-lookup index in an EXPLAIN (ANALYZE, FORMAT JSON) document.
func lookupIndexLoops(node any) (float64, bool) {
	switch v := node.(type) {
	case []any:
		for _, item := range v {
			if loops, ok := lookupIndexLoops(item); ok {
				return loops, true
			}
		}
	case map[string]any:
		if name, _ := v["Index Name"].(string); name == "fact_records_supply_chain_impact_package_lookup_idx" {
			loops, _ := v["Actual Loops"].(float64)
			return loops, true
		}
		for _, child := range v {
			if loops, ok := lookupIndexLoops(child); ok {
				return loops, true
			}
		}
	}
	return 0, false
}
