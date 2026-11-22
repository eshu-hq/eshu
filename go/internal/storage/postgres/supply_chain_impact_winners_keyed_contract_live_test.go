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

// TestSupplyChainImpactWinnersResweepVolatileDigestExcludedFieldsChangeNothingLive
// proves the #10880 arbiter ruling (2026-11-21, decision 3) on PostgreSQL:
// rewriting every volatile digest-excluded payload key leaves the stored
// winner payload byte-for-byte unchanged, so a full resweep writes nothing.
// evidence_fact_ids is the one excluded key the stored payload serves; its
// refresh is the companion proof below.
func TestSupplyChainImpactWinnersResweepVolatileDigestExcludedFieldsChangeNothingLive(t *testing.T) {
	h := newKeyedHarness(t, "#10880 volatile digest-excluded fields proof")
	scopeA, _ := keyedSeedSettled(h)
	before := keyedStoredWinnerPayloads(t, h)
	for _, key := range supplychaincore.DigestExcludedPayloadKeys() {
		if key == "evidence_fact_ids" {
			continue
		}
		h.exec(`UPDATE fact_records SET payload = jsonb_set(payload, ARRAY[$2]::text[], to_jsonb('rewritten-'||$2::text), true) WHERE scope_id = $1`, scopeA, key)
	}
	h.exec(`UPDATE fact_records SET payload = jsonb_set(payload, '{provenance,content_digest}', '"another"', true) WHERE scope_id = $1`, scopeA)
	got, err := h.store.ResweepWinners(h.ctx)
	if err != nil || got.Inserted != 0 || got.Updated != 0 || got.Deleted != 0 {
		t.Fatalf("full resweep after rewriting volatile digest-excluded fields = %+v, %v; want no winner written", got, err)
	}
	if after := keyedStoredWinnerPayloads(t, h); after != before {
		t.Fatalf("stored winner payloads moved on a volatile-only rewrite\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestSupplyChainImpactWinnersResweepEvidenceFactIDsRefreshWinnerPayloadLive
// proves the served half of the #10880 arbiter ruling (2026-11-21): an
// evidence-only rewrite moves the stored winner payload even though the
// content digest excludes evidence_fact_ids, so exactly one winner updates
// and its stored evidence set equals the new set.
func TestSupplyChainImpactWinnersResweepEvidenceFactIDsRefreshWinnerPayloadLive(t *testing.T) {
	h := newKeyedHarness(t, "#10880 evidence refresh proof")
	scopeA, _ := keyedSeedSettled(h)
	h.exec(`UPDATE fact_records SET payload = jsonb_set(payload, '{evidence_fact_ids}', '["ev-a","ev-b"]'::jsonb, true)
WHERE scope_id = $1 AND payload->>'cve_id' = 'CVE-2026-2002'`, scopeA)
	got, err := h.store.ResweepWinners(h.ctx)
	if err != nil || got.Inserted != 0 || got.Updated != 1 || got.Deleted != 0 {
		t.Fatalf("full resweep after an evidence-only rewrite = %+v, %v; want exactly one winner updated", got, err)
	}
	var stored string
	if err := h.db.QueryRowContext(h.ctx, `
SELECT (payload->'evidence_fact_ids')::text FROM supply_chain_impact_canonical_winners WHERE cve_id = 'CVE-2026-2002'`).Scan(&stored); err != nil {
		t.Fatalf("read stored evidence set: %v", err)
	}
	var ids []string
	if err := json.Unmarshal([]byte(stored), &ids); err != nil {
		t.Fatalf("decode stored evidence set %q: %v", stored, err)
	}
	if len(ids) != 2 || ids[0] != "ev-a" || ids[1] != "ev-b" {
		t.Fatalf("stored evidence set = %v, want [ev-a ev-b]", ids)
	}
}

// TestSupplyChainImpactWinnersResweepKeyedEvidenceOnlyRefreshLive proves the
// capture half of the #10880 arbiter ruling (2026-11-21, decision 4) through
// the production writer: an evidence-only writer pass (content digest
// unchanged) stages the key, the keyed resweep refreshes the stored evidence
// set, and the audit then finds nothing to repair. An intent-only rerun —
// the volatile keys the winner select strips — stages nothing, writes
// nothing on the keyed tick, and also audits clean, so steady state keeps
// Repairs=0 without staging every finding every pass.
func TestSupplyChainImpactWinnersResweepKeyedEvidenceOnlyRefreshLive(t *testing.T) {
	h := newKeyedHarness(t, "#10880 keyed evidence capture proof")
	scopeA, _ := keyedSeedSettled(h)

	changed := keyedFinding("CVE-2026-2002", "pkg:npm/y", 5)
	changed.EvidenceFactIDs = []string{"cve-evidence", "ev-new"}
	h.write(scopeA, scopeA+"/g1", keyedFinding("CVE-2026-2001", "pkg:npm/x", 5), changed)
	if depth := h.pendingDepth(); depth == 0 {
		t.Fatal("evidence-only writer pass staged no capture; the keyed tick would never refresh the stored evidence set")
	}
	if got := h.keyed(); got.Updated != 1 || got.Inserted != 0 || got.Deleted != 0 {
		t.Fatalf("keyed tick after the evidence-only pass = %+v, want exactly one winner updated", got)
	}
	if ids := keyedStoredEvidence(t, h, "CVE-2026-2002"); len(ids) != 2 || ids[0] != "cve-evidence" || ids[1] != "ev-new" {
		t.Fatalf("stored evidence set after the keyed tick = %v, want [cve-evidence ev-new]", ids)
	}
	if audit, err := h.store.AuditWinners(h.ctx); err != nil || audit.Skipped || audit.Repairs != 0 {
		t.Fatalf("audit after the evidence refresh = %+v, %v; want zero repairs", audit, err)
	}

	// Intent-only rerun: same findings, new intent id and cause.
	if _, err := h.writer.WriteSupplyChainImpactFindings(h.ctx, supplychaincore.SupplyChainImpactWrite{
		IntentID: "intent-10880-rerun", ScopeID: scopeA, GenerationID: scopeA + "/g1",
		SourceSystem: "vulnerability_intelligence", Cause: "10880 intent-only rerun",
		Findings:     []supplychaincore.SupplyChainImpactFinding{keyedFinding("CVE-2026-2001", "pkg:npm/x", 5), changed},
		FencingToken: keyedFenceTokens.Add(1),
	}); err != nil {
		t.Fatalf("intent-only writer pass: %v", err)
	}
	if depth := h.pendingDepth(); depth != 0 {
		t.Fatalf("intent-only rerun staged %d captures, want none: volatile keys must not stage", depth)
	}
	if got := h.keyed(); got.Inserted != 0 || got.Updated != 0 || got.Deleted != 0 {
		t.Fatalf("keyed tick after the intent-only rerun = %+v, want no winner written", got)
	}
	if audit, err := h.store.AuditWinners(h.ctx); err != nil || audit.Skipped || audit.Repairs != 0 {
		t.Fatalf("audit after the intent-only rerun = %+v, %v; want zero repairs", audit, err)
	}
}

// keyedStoredEvidence reads the stored winner payload's evidence fact id
// set for one CVE.
func keyedStoredEvidence(t *testing.T, h *keyedHarness, cve string) []string {
	t.Helper()
	var stored string
	if err := h.db.QueryRowContext(h.ctx, `
SELECT (payload->'evidence_fact_ids')::text FROM supply_chain_impact_canonical_winners WHERE cve_id = $1`, cve).Scan(&stored); err != nil {
		t.Fatalf("read stored evidence set for %s: %v", cve, err)
	}
	var ids []string
	if err := json.Unmarshal([]byte(stored), &ids); err != nil {
		t.Fatalf("decode stored evidence set %q: %v", stored, err)
	}
	return ids
}

// keyedStoredWinnerPayloads reads every stored winner payload in canonical
// key order as one string, so a byte comparison covers the whole table.
func keyedStoredWinnerPayloads(t *testing.T, h *keyedHarness) string {
	t.Helper()
	var all string
	if err := h.db.QueryRowContext(h.ctx, `
SELECT COALESCE(string_agg(payload::text, E'\n' ORDER BY canonical_key), '')
FROM supply_chain_impact_canonical_winners`).Scan(&all); err != nil {
		t.Fatalf("read stored winner payloads: %v", err)
	}
	return all
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
	got, err := h.store.ResweepWinners(h.ctx)
	if err != nil || got.Inserted != 0 || got.Updated != 0 || got.Deleted != 0 {
		t.Fatalf("full resweep after deleting a superseded generation = %+v, %v; want no winner written", got, err)
	}
}

// TestSupplyChainImpactWinnersResweepKeyedProbePlanLive asserts the plan shape the
// #9975 ruling requires at scale: after the identifier temp table is analyzed,
// the active_facts build probes the keyed winners index (the package triple
// plus the coalesced c0 and advisory expressions) once per identifier, so the
// heap fetches only the facts a dirty key keeps. The earlier per-triple probe
// fetched every candidate of a touched triple as wide rows and discarded most
// after the heap fetch: on QA that was 15,888 fetched rows (160 MB of payload)
// for 2,720 kept on a 200-identifier sparse tick. The probe never scans
// fact_records.
func TestSupplyChainImpactWinnersResweepKeyedProbePlanLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 keyed plan proof")
	// #10544 retirement parity: a fully bootstrapped database must carry the
	// 291 keyed index and must not carry the retired triple-only
	// package-lookup index, whose planner cost ties the 291 index at rows=1
	// (see (#10544)). The
	// seeded-violation arm plants the retired index and requires the same
	// census to see it, so an absence assertion that could not detect the
	// index would fail here instead of passing vacuously.
	const retiredIndex = "fact_records_supply_chain_impact_package_lookup_idx"
	if keyedIndexExists(t, h, retiredIndex) {
		t.Fatalf("retired index %s is present after bootstrap; migration 293 must drop it", retiredIndex)
	}
	if !keyedIndexExists(t, h, "fact_records_supply_chain_impact_winners_keyed_idx") {
		t.Fatal("the 291 keyed winners index is missing after bootstrap")
	}
	h.exec(`CREATE INDEX ` + retiredIndex + ` ON fact_records ((payload->>'package_id'), (payload->>'repository_id'), (payload->>'subject_digest'), fact_id ASC, generation_id) WHERE fact_kind = 'reducer_supply_chain_impact_finding' AND is_tombstone = FALSE`)
	if !keyedIndexExists(t, h, retiredIndex) {
		t.Fatal("seeded violation undetected: the index census does not see a planted " + retiredIndex)
	}
	h.exec(`DROP INDEX ` + retiredIndex)
	if keyedIndexExists(t, h, retiredIndex) {
		t.Fatalf("seeded index %s survived its drop; the plan assertions below would run against the wrong schema", retiredIndex)
	}
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
	if !strings.Contains(plan, "fact_records_supply_chain_impact_winners_keyed_idx") {
		t.Errorf("the keyed probe does not use the keyed winners index:\n%s", plan)
	}
	if regexp.MustCompile(`"Node Type":\s*"Seq Scan"[^}]*"Relation Name":\s*"fact_records"`).MatchString(plan) {
		t.Errorf("the keyed probe seq-scans fact_records:\n%s", plan)
	}

	// 21 distinct triples (20 ordinary, one hot) carry 50 identifiers. The keyed
	// index must be searched once per identifier with the c0 and advisory
	// expressions as index conditions, so the hot triple's 3,000 candidates are
	// not heap-fetched at all: the scan returns the 50 kept facts, one per
	// identifier, not 3,050 candidates. The plan must hold in both statistics
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
		loops, rows, found := keyedIndexScanStats(analyzedPlan)
		if !found {
			t.Fatalf("%s: the analyzed plan has no scan of the keyed winners index:\n%s", state, analyzed)
		}
		if loops != 50 {
			t.Errorf("%s: the keyed winners index was searched %v times, want 50 (once per identifier):\n%s", state, loops, analyzed)
		}
		if rows != 50 {
			t.Errorf("%s: the keyed winners index scan returned %v rows, want 50 kept facts (one per identifier, not the hot triple's 3,000 candidates):\n%s", state, rows, analyzed)
		}
		if !strings.Contains(string(analyzed), `"Subplan Name": "CTE ident_facts"`) {
			t.Errorf("%s: the probe is not a materialized CTE node:\n%s", state, analyzed)
		}
	}
}

// keyedIndexExists reports whether the named index exists in the harness
// database's current schema. It is the census both arms of the #10544
// retirement parity check in the plan proof above run: the clean arm
// requires the retired index absent, the seeded arm requires a planted one
// present.
func keyedIndexExists(t *testing.T, h *keyedHarness, indexName string) bool {
	t.Helper()
	var exists bool
	if err := h.db.QueryRowContext(h.ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = current_schema() AND indexname = $1)`,
		indexName).Scan(&exists); err != nil {
		t.Fatalf("index census for %s: %v", indexName, err)
	}
	return exists
}

// keyedIndexScanStats returns the "Actual Loops" and the total rows returned
// ("Actual Rows" is per loop) of the first scan of the keyed winners index in
// an EXPLAIN (ANALYZE, FORMAT JSON) document.
func keyedIndexScanStats(node any) (loops, rows float64, found bool) {
	switch v := node.(type) {
	case []any:
		for _, item := range v {
			if loops, rows, ok := keyedIndexScanStats(item); ok {
				return loops, rows, true
			}
		}
	case map[string]any:
		if name, _ := v["Index Name"].(string); name == "fact_records_supply_chain_impact_winners_keyed_idx" {
			loops, _ := v["Actual Loops"].(float64)
			perLoop, _ := v["Actual Rows"].(float64)
			return loops, loops * perLoop, true
		}
		for _, child := range v {
			if loops, rows, ok := keyedIndexScanStats(child); ok {
				return loops, rows, true
			}
		}
	}
	return 0, 0, false
}
