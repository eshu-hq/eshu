// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Fan-out keyed proof (#10839). The hermetic keyed fixtures hold a handful
// of copies per identifier, but QA shows hundreds of active copies behind
// one five-tuple (50,123 copies over 200 oldest dirty identifiers collapsing
// to 236 distinct live pairs on the 2026-11-24 replica). These tests seed
// that shape through the production writer path: the same finding in every
// generation of many scopes, retired generations, a scope/generation
// mismatch row, and an operator suppression. The independent oracle is a
// fresh full recompute, which shares no probe code with the keyed build.

const (
	fanoutScopes = 30
	fanoutKeyCVE = "CVE-2026-FANOUT-1"
	fanoutKeyPkg = "pkg:npm/fanout-key"
)

// seedFanout writes the same keyed finding into three generations of every
// scope (two retired, one active). It returns the live copy count and the
// scanned copy count the tick must report.
func seedFanout(h *keyedHarness) (live, scanned int) {
	h.t.Helper()
	// Bulk background corpus first: 20,000 unrelated findings so the table
	// is big enough for the planner's index choice to mean something (a
	// 91-row table seq-scans no matter what). Raw inserts bypass the writer
	// but still queue dirty rows, so drain them before seeding the measured
	// fan-out below.
	for _, bg := range []string{"vuln-intel://10839/bg-a", "vuln-intel://10839/bg-b"} {
		h.seedScope(bg, bg+"/g0")
		h.exec(`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'fanout-bg-' || $1 || '-' || g, $1, $1 || '/g0',
  'reducer_supply_chain_impact_finding', 'fanout-bg-' || $1 || '-' || g, '1.0.0', 'synthetic',
  'fanout-bg-' || $1 || '-' || g, now(), now(), false,
  jsonb_build_object('package_id', 'pkg:npm/bg-' || (g % 500), 'repository_id', 'repo-bg',
    'subject_digest', '', 'cve_id', 'CVE-2026-BG-' || g, 'advisory_id', '',
    'impact_status', 'affected_exact')
FROM generate_series(1, 10000) AS g`, bg)
	}
	for i := 0; ; i++ {
		if h.dirtyDepth()+h.stagingDepth() == 0 {
			break
		}
		if i == 50 {
			h.t.Fatalf("bulk backlog did not drain: dirty=%d staging=%d", h.dirtyDepth(), h.stagingDepth())
		}
		if _, err := h.store.ResweepWinnersKeyed(h.ctx); err != nil {
			h.t.Fatalf("drain bulk backlog: %v", err)
		}
	}
	for i := 0; i < fanoutScopes; i++ {
		scope := fmt.Sprintf("vuln-intel://10839/fanout/%02d", i)
		h.seedScope(scope, scope+"/g0")
		h.write(scope, scope+"/g0", keyedFinding(fanoutKeyCVE, fanoutKeyPkg, i))
		h.addGeneration(scope, scope+"/g1", "pending")
		h.activate(scope, scope+"/g0", scope+"/g1")
		h.write(scope, scope+"/g1", keyedFinding(fanoutKeyCVE, fanoutKeyPkg, i))
		h.addGeneration(scope, scope+"/g2", "pending")
		h.activate(scope, scope+"/g1", scope+"/g2")
		h.write(scope, scope+"/g2", keyedFinding(fanoutKeyCVE, fanoutKeyPkg, i))
	}
	// One tombstoned copy in a retired generation: excluded from the scan and
	// the kept set by the probe's tombstone filter.
	h.exec(`UPDATE fact_records SET is_tombstone = TRUE
WHERE scope_id = 'vuln-intel://10839/fanout/00'
  AND generation_id = 'vuln-intel://10839/fanout/00/g0'
  AND fact_kind = 'reducer_supply_chain_impact_finding'
  AND payload->>'cve_id' = '` + fanoutKeyCVE + `'`)
	// One scope/generation mismatch row: the five-tuple matches, but the
	// generation belongs to another scope, so no liveness row covers it.
	h.dropForeignKeysOf("fact_records")
	h.exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'fanout-mismatch/00', 'vuln-intel://10839/fanout/00', 'vuln-intel://10839/fanout/01/g2',
  fact_kind, 'fanout-mismatch/00', schema_version, source_system, source_fact_key,
  observed_at, ingested_at, FALSE, payload
FROM fact_records
WHERE scope_id = 'vuln-intel://10839/fanout/01' AND payload->>'cve_id' = '` + fanoutKeyCVE + `'
LIMIT 1`)
	h.seedOperator("fanout-op-g0", "", map[string]map[string]any{
		"fanout-op-1": {"action": "suppress", "cve_id": fanoutKeyCVE},
	})
	h.exec(`ANALYZE fact_records`)
	// Live copies: the 30 active generations. Scanned copies add the two
	// retired gens per scope (60, minus the tombstoned retired copy) plus
	// the mismatch row.
	return 30, 90 - 1 + 1
}

// TestSupplyChainImpactWinnersKeyedFanoutMatchesRecomputeLive runs a keyed tick over the
// fan-out fixture and requires byte parity with a fresh full recompute, plus
// the exact scanned/kept counts. The recompute shares no probe code with the
// keyed build, so it is an independent oracle for the restructure.
func TestSupplyChainImpactWinnersKeyedFanoutMatchesRecomputeLive(t *testing.T) {
	h := newKeyedHarness(t, "#10839 fan-out parity")
	wantKept, wantScanned := seedFanout(h)
	result := h.keyed()
	h.assertParity("#10839 fan-out")
	if result.ChunkKeptRows != int64(wantKept) {
		t.Errorf("kept rows = %d, want %d (live copies changed: the probe admits or drops rows)", result.ChunkKeptRows, wantKept)
	}
	if result.ChunkScannedCopies != int64(wantScanned) {
		t.Errorf("scanned copies = %d, want %d (probe fan-out changed: copies added or missed)", result.ChunkScannedCopies, wantScanned)
	}
}

// walkPlan visits every node of a JSON EXPLAIN tree.
func walkPlan(t *testing.T, planJSON []byte, visit func(node map[string]any)) {
	t.Helper()
	var plans []any
	if err := json.Unmarshal(planJSON, &plans); err != nil {
		t.Fatalf("plan is not JSON: %v", err)
	}
	var walk func(node any)
	walk = func(node any) {
		m, ok := node.(map[string]any)
		if !ok {
			return
		}
		visit(m)
		if kids, _ := m["Plans"].([]any); kids != nil {
			for _, k := range kids {
				walk(k)
			}
		}
	}
	for _, p := range plans {
		if m, ok := p.(map[string]any); ok {
			if inner, ok := m["Plan"]; ok {
				walk(inner)
			} else {
				walk(m)
			}
		}
	}
}

// maxLivenessLoops returns the largest Actual Loops over the liveness tables.
func maxLivenessLoops(t *testing.T, planJSON []byte) float64 {
	t.Helper()
	maxLoops := 0.0
	walkPlan(t, planJSON, func(m map[string]any) {
		if rel, _ := m["Relation Name"].(string); rel == "ingestion_scopes" || rel == "scope_generations" {
			if loops, _ := m["Actual Loops"].(float64); loops > maxLoops {
				maxLoops = loops
			}
		}
	})
	return maxLoops
}

// hasSeqScanOn reports whether any Seq Scan node reads table.
func hasSeqScanOn(t *testing.T, planJSON []byte, table string) bool {
	t.Helper()
	found := false
	walkPlan(t, planJSON, func(m map[string]any) {
		if typ, _ := m["Node Type"].(string); typ == "Seq Scan" {
			if rel, _ := m["Relation Name"].(string); rel == table {
				found = true
			}
		}
	})
	return found
}

// TestSupplyChainImpactWinnersKeyedFanoutLivenessLoopsBoundedLive explains the production
// build statement over the fan-out fixture and requires the liveness tables
// to be visited at most once per distinct live pair. The shipped shape
// nested-loops them once per candidate copy (91 loops here); the fixed shape
// resolves the live set once per tick.
func TestSupplyChainImpactWinnersKeyedFanoutLivenessLoopsBoundedLive(t *testing.T) {
	h := newKeyedHarness(t, "#10839 fan-out plan shape")
	seedFanout(h)
	conn, err := h.db.Conn(h.ctx)
	if err != nil {
		t.Fatalf("connection: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	for _, stmt := range []string{
		`CREATE TEMP TABLE ` + postgres.WinnersKeyedIdentsTableForTest + ` (package_id text, repository_id text, subject_digest text, c0 text, advisory_id text)`,
		`INSERT INTO ` + postgres.WinnersKeyedIdentsTableForTest + `
SELECT DISTINCT package_id, repository_id, subject_digest, c0, advisory_id FROM (
  SELECT (payload->>'package_id') AS package_id, (payload->>'repository_id') AS repository_id,
    (payload->>'subject_digest') AS subject_digest,
    COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), '') AS c0,
    COALESCE(payload->>'advisory_id', '') AS advisory_id
  FROM fact_records
  WHERE fact_kind = 'reducer_supply_chain_impact_finding' AND NOT is_tombstone
    AND (payload->>'cve_id') = '` + fanoutKeyCVE + `') AS keyed`,
		`ANALYZE ` + postgres.WinnersKeyedIdentsTableForTest,
	} {
		if _, err := conn.ExecContext(h.ctx, stmt); err != nil {
			t.Fatalf("prepare identifiers: %v", err)
		}
	}
	var planJSON []byte
	if err := conn.QueryRowContext(h.ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+postgres.WinnersKeyedActiveFactsSQLForTest).Scan(&planJSON); err != nil {
		t.Fatalf("explain probe: %v", err)
	}
	plan := string(planJSON)
	if !strings.Contains(plan, "fact_records_supply_chain_impact_winners_keyed_idx") {
		t.Errorf("the keyed probe does not use the keyed winners index:\n%s", plan)
	}
	if hasSeqScanOn(t, planJSON, "fact_records") {
		t.Errorf("the keyed probe seq-scans fact_records:\n%s", plan)
	}
	var livePairs int
	if err := conn.QueryRowContext(h.ctx, `SELECT count(*) FROM (
SELECT DISTINCT f.scope_id, f.generation_id FROM fact_records AS f
JOIN ingestion_scopes AS s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
JOIN scope_generations AS g ON g.scope_id = f.scope_id AND g.generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
  AND (f.payload->>'cve_id') = '`+fanoutKeyCVE+`' AND g.status = 'active') p`).Scan(&livePairs); err != nil {
		t.Fatalf("count live pairs: %v", err)
	}
	bound := float64(livePairs + 5)
	if got := maxLivenessLoops(t, planJSON); got > bound {
		t.Errorf("liveness loops = %v, want <= %v (live pairs + slack): per-copy liveness probes are back", got, bound)
	}
}
