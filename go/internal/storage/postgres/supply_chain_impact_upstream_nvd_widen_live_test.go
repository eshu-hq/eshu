// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9994 disposable PostgreSQL 18 proof that the #9914 widened upstream probe — the CVE key probe over all five vulnerability kinds — holds its anchor on an NVD-bearing store at the 120-id chunk bound under custom and generic plans; the no-statistics control must walk, so a hold proves the measurement and not the fixture
//eshu:readiness:env ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DISPOSABLE ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DSN

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
)

// nvdWidenScope carries the NVD-shaped product layer: per-CVE product counts
// at the public-data proxy scale from the #9994 evidence note (distro/kernel
// pool median 6, mean 10.5, p99 131 across 1,582 CVEs; resampled 961-CVE pass
// median 10,043 rows). Forty probed CVEs carry 200 products each and two carry
// 500, so the probed set averages about 80 products per CVE — several times the
// 19-per-CVE break-even the #9914 shim measured. A hold here leaves headroom at
// proxy scale; the two documented limits of the proxy (not an image's real CVE
// list, pages not a random NVD sample) still apply.
const (
	nvdWidenScope         = "vuln:nvd"
	nvdWidenActiveGen     = "gen:nvd:active"
	nvdWidenHeavyCVEs     = 40
	nvdWidenHeavyPerCVE   = 200
	nvdWidenExtremePerCVE = 500
)

// TestSupplyChainImpactUpstreamWidenedProbeHoldsNVDScaleLive is the #9994 AC3
// store measurement: the widened upstream probe (every vulnerability kind the
// CVE anchor covers) must stay on the CVE key probe at the chunk bound on a
// store that holds NVD product rows, under custom and generic plans. The
// explained candidate is the shipped vulnerability_cve_id SQL, which the
// transfer guard below proves byte-identical to the widened upstream shape;
// the catalog keeps the 2-kind fallback (TestUpstreamBranchReadsOnlySignalKinds,
// TestSupplyChainImpactUpstreamListReadsOnlySignalsLive) until the owner re-runs
// the widening decision on this evidence.
func TestSupplyChainImpactUpstreamWidenedProbeHoldsNVDScaleLive(t *testing.T) {
	ctx, database := openSupplyChainImpactEvidenceProofDB(t)
	seedCVEChunkWalkFixture(t, ctx, database, cveChunkWalkCorrelation)
	seedUpstreamNVDProductLayer(t, ctx, database)

	candidate := nvdWidenCandidateSQL(t)
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("pin a connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	set := func(setting string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, "SET "+setting); err != nil {
			t.Fatalf("set %s: %v", setting, err)
		}
	}
	probe := cveChunkIDs(factimpact.MaxCVEIDsPerStatement)
	literal := supplyChainImpactSQLLiteral(t, probe)
	if _, err := conn.ExecContext(ctx, "PREPARE nvd_widen_probe(text[]) AS "+candidate); err != nil {
		t.Fatalf("prepare widened probe: %v", err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, "DEALLOCATE nvd_widen_probe") }()
	for _, pageCost := range []string{"1.1", "4"} {
		set("random_page_cost = " + pageCost)
		for _, mode := range supplyChainImpactPlanModes {
			set("plan_cache_mode = " + mode)
			var raw []byte
			if err := conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) EXECUTE nvd_widen_probe("+literal+")").Scan(&raw); err != nil {
				t.Fatalf("explain 120 ids %s random_page_cost=%s: %v", mode, pageCost, err)
			}
			plan := string(raw)
			if cveChunkPlanWalks(t, plan) {
				t.Errorf("widened probe walked the advisory index at 120 ids %s random_page_cost=%s:\n%s",
					mode, pageCost, supplyChainImpactPlanSummary(t, plan))
			} else if problem := supplyChainImpactAnchorProblem(t, plan, factimpact.IndexVulnerabilityCVEV2); problem != "" {
				t.Errorf("widened probe at 120 ids %s random_page_cost=%s: %s\n%s",
					mode, pageCost, problem, supplyChainImpactPlanSummary(t, plan))
			}
		}
	}
	est, actual, buffers := cveStatsSpot(t, ctx, conn, candidate, factimpact.MaxCVEIDsPerStatement)
	t.Logf("nvd with-stats n=120: candidate est=%.0f actual=%.0f buffers=%d", est, actual, buffers)

	// The loader path on the NVD store: 121 upstream ids still fan out to
	// exactly two statements, one stat each, so per-pass telemetry counts
	// statements on NVD data exactly as TestUpstreamListIsChunkedOnTheKeyProbe
	// proves on the stub.
	loaded, stats, err := NewFactStore(SQLDB{DB: database}).ListActiveSupplyChainImpactFactsWithStats(
		ctx, supplychaincore.SupplyChainImpactFactFilter{UpstreamCVEIDs: cveChunkIDs(factimpact.MaxCVEIDsPerStatement + 1)})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFactsWithStats on the NVD store: %v", err)
	}
	if len(loaded) == 0 {
		t.Error("loader returned no rows on the NVD store for upstream ids that carry signal facts")
	}
	upstreamStatements := 0
	for _, branch := range stats.Branches {
		if branch.Branch == factimpact.BranchVulnerabilityUpstreamCVEID {
			upstreamStatements++
		}
	}
	if upstreamStatements != 2 {
		t.Errorf("upstream branch stats = %d, want 2 for 121 ids: one per statement", upstreamStatements)
	}

	// The control: without migration 283 the same candidate must walk, so the
	// hold above proves the statistics-backed plan and not a toothless fixture.
	nvdWidenDropStats(t, ctx, database)
	defer nvdWidenReapplyStats(t, ctx, database)
	if _, err := conn.ExecContext(ctx, "PREPARE nvd_widen_control(text[]) AS "+candidate); err != nil {
		t.Fatalf("prepare control: %v", err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, "DEALLOCATE nvd_widen_control") }()
	set("random_page_cost = 1.1")
	set("plan_cache_mode = force_custom_plan")
	firstWalk := 0
	for length := factimpact.MaxCVEIDsPerStatement + 1; length < cveChunkWalkFallsAt && firstWalk == 0; length++ {
		var raw []byte
		literal := supplyChainImpactSQLLiteral(t, cveChunkIDs(length))
		if err := conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) EXECUTE nvd_widen_control("+literal+")").Scan(&raw); err != nil {
			t.Fatalf("explain control %d ids: %v", length, err)
		}
		if cveChunkPlanWalks(t, string(raw)) {
			firstWalk = length
		}
	}
	if firstWalk == 0 {
		t.Fatalf("without-stats: no walk from %d to %d ids on the NVD store, so the fixture cannot show the failure",
			factimpact.MaxCVEIDsPerStatement+1, cveChunkWalkFallsAt-1)
	}
	t.Logf("nvd without-stats first walk at %d ids, bound %d", firstWalk, factimpact.MaxCVEIDsPerStatement)
}

// nvdWidenCandidateSQL returns the shipped vulnerability_cve_id SQL after
// proving it is exactly the widened upstream shape: the upstream branch with
// its 2-kind predicate replaced by the five vulnerability kinds. The catalog
// builder renders both from the same keyProbe with the same anchor, key, and
// one bind parameter, so the plans this test measures transfer to the widening
// decision byte for byte. Any catalog drift fails this guard instead of
// silently measuring a different statement.
func nvdWidenCandidateSQL(t *testing.T) string {
	t.Helper()
	var candidate, upstream string
	for _, branch := range factimpact.Branches() {
		switch branch.Name {
		case cveChunkBranch:
			candidate = branch.SQL
		case factimpact.BranchVulnerabilityUpstreamCVEID:
			upstream = branch.SQL
		}
	}
	if candidate == "" {
		t.Fatal("catalog has no vulnerability_cve_id branch")
	}
	if upstream == "" {
		t.Fatalf("catalog has no %s branch", factimpact.BranchVulnerabilityUpstreamCVEID)
	}
	const twoKind = "fact.fact_kind IN ('vulnerability.epss_score', 'vulnerability.known_exploited')"
	if strings.Count(upstream, twoKind) != 1 {
		t.Fatalf("upstream SQL does not carry the 2-kind predicate exactly once:\n%s", upstream)
	}
	const fiveKind = "fact.fact_kind IN ('vulnerability.cve', 'vulnerability.affected_package', 'vulnerability.affected_product', 'vulnerability.epss_score', 'vulnerability.known_exploited')"
	if widened := strings.Replace(upstream, twoKind, fiveKind, 1); widened != candidate {
		t.Fatalf("widened upstream SQL drifts from the CVE arm; the plan proof no longer transfers:\n%s", widened)
	}
	return candidate
}

// seedUpstreamNVDProductLayer adds the NVD-shaped rows the walk fixture lacks:
// per-CVE product fan-out at several times the #9914 break-even, plus the
// cve, affected_package, EPSS and KEV rows that make each probed CVE return
// the full widened row set. Rows land in their own scope but the probe joins
// every active generation the same way, so the plan shape is the production
// one. ANALYZE refreshes the migration 283 expression statistics over the new
// rows; the advisory correlation the walk needs is stored again afterwards
// because ANALYZE recomputes it from the heap.
func seedUpstreamNVDProductLayer(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	supplyChainImpactFixtureScope(t, ctx, database, nvdWidenScope, nvdWidenScope, `{}`, nvdWidenActiveGen, nil)
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload
)
SELECT format('nvd:prod:%s:%s', cve, n), 'vuln:nvd', 'gen:nvd:active',
  'vulnerability.affected_product', format('stable:nvd:prod:%s:%s', cve, n),
  '1.0.0', 'synthetic', 1, 'reported', 'synthetic',
  format('source:nvd:prod:%s:%s', cve, n),
  '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z', false,
  jsonb_build_object('cve_id', cve, 'criteria', format('cpe:2.3:a:vendor:product:%s:*:*:*:*:*:*:*', n))
FROM (SELECT 'CVE-2024-' || lpad(g::text, 5, '0') AS cve FROM generate_series(0, 39) AS g) AS heavy,
  generate_series(1, 200) AS n`); err != nil {
		t.Fatalf("seed heavy product rows: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload
)
SELECT format('nvd:prod:x:%s:%s', cve, n), 'vuln:nvd', 'gen:nvd:active',
  'vulnerability.affected_product', format('stable:nvd:prod:x:%s:%s', cve, n),
  '1.0.0', 'synthetic', 1, 'reported', 'synthetic',
  format('source:nvd:prod:x:%s:%s', cve, n),
  '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z', false,
  jsonb_build_object('cve_id', cve, 'criteria', format('cpe:2.3:a:vendor:extreme:%s:*:*:*:*:*:*:*', n))
FROM (VALUES ('CVE-2024-00100'), ('CVE-2024-00101')) AS extreme(cve),
  generate_series(1, 500) AS n`); err != nil {
		t.Fatalf("seed extreme product rows: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload
)
SELECT format('nvd:%s:%s', kind, cve), 'vuln:nvd', 'gen:nvd:active', kind,
  format('stable:nvd:%s:%s', kind, cve),
  '1.0.0', 'synthetic', 1, 'reported', 'synthetic',
  format('source:nvd:%s:%s', kind, cve),
  '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z', false,
  jsonb_build_object('cve_id', cve,
    'criteria', 'cpe:2.3:a:vendor:product:1:*:*:*:*:*:*:*',
    'package_id', 'pkg:npm/vendor-product')
FROM (SELECT 'CVE-2024-' || lpad(g::text, 5, '0') AS cve FROM generate_series(0, 119) AS g) AS ids,
  (VALUES
    ('vulnerability.cve'),
    ('vulnerability.affected_package'),
    ('vulnerability.affected_product'),
    ('vulnerability.epss_score'),
    ('vulnerability.known_exploited')) AS kinds(kind)`); err != nil {
		t.Fatalf("seed widened row set: %v", err)
	}
	supplyChainImpactFixtureAnalyze(t, ctx, database)
	cveStatsRestoreWalkCorrelation(t, ctx, database)
}

// nvdWidenDropStats removes migration 283's statistics object and refreshes
// the table statistics for the no-statistics control arm.
func nvdWidenDropStats(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	if _, err := database.ExecContext(ctx, "DROP STATISTICS IF EXISTS "+cveStatsObjectName); err != nil {
		t.Fatalf("drop cve_id statistics object: %v", err)
	}
	if _, err := database.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze after drop: %v", err)
	}
	cveStatsRestoreWalkCorrelation(t, ctx, database)
}

// nvdWidenReapplyStats restores migration 283's shipped bytes after the
// control arm, so the disposable database is left in its bootstrapped shape.
func nvdWidenReapplyStats(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	applyCVEStatsMigration(t, ctx, database)
	if _, err := database.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze after re-apply: %v", err)
	}
	cveStatsRestoreWalkCorrelation(t, ctx, database)
}
