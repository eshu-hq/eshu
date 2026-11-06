// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10050 disposable PostgreSQL 18 proof that migration 281's extended-statistics object on payload->>'cve_id' holds every key probe on the CVE index on its anchor past the advisory-index walk the fixture reproduces without it; dropping the object is the RED control
//eshu:readiness:env ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DISPOSABLE ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
)

// cveStatsMigrationFile is the #10050 statistics migration the plan proof
// applies and drops. The test reads its SQL from the embedded bootstrap
// definitions, so the proof always runs the shipped bytes.
const cveStatsMigrationFile = "281_fact_records_vulnerability_cve_id_stats.sql"

// cveStatsObjectName is the extended-statistics object migration 281 creates.
const cveStatsObjectName = "fact_records_vulnerability_cve_id_stats"

// cveStatsSweepLengths is every probed list length: the whole chunked range
// the loader can no longer emit past the bound, plus the long lists the
// re-derived bound must be measured against.
func cveStatsSweepLengths() []int {
	lengths := make([]int, 0, 402)
	for length := 1; length <= 400; length++ {
		lengths = append(lengths, length)
	}
	return append(lengths, 1000, 2000)
}

// TestSupplyChainImpactCVEStatsHoldProbePastTheWalkLive is the #10050 plan
// proof: on the walk fixture from TestSupplyChainImpactCVEBoundStaysBelow-
// TheWalkLive (which reproduces the advisory-index walk above the chunk
// bound without statistics), every key probe on the CVE index must stay off
// the advisory index at every swept length, under custom and generic plans
// at random_page_cost 1.1 and 4. The explained statements are the catalog's
// own branch SQL, never hand-copied.
func TestSupplyChainImpactCVEStatsHoldProbePastTheWalkLive(t *testing.T) {
	ctx, database := openSupplyChainImpactEvidenceProofDB(t)
	seedCVEChunkWalkFixture(t, ctx, database, cveChunkWalkCorrelation)
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
	probes := cveKeyProbes(t)
	sweep := func(label string) int {
		t.Helper()
		walks := 0
		for _, pageCost := range []string{"1.1", "4"} {
			for _, probe := range probes {
				if _, err := conn.ExecContext(ctx, "PREPARE cve_stats_probe(text[]) AS "+probe.SQL); err != nil {
					t.Fatalf("prepare %s: %v", probe.Name, err)
				}
				set("random_page_cost = " + pageCost)
				for _, mode := range supplyChainImpactPlanModes {
					set("plan_cache_mode = " + mode)
					for _, length := range cveStatsSweepLengths() {
						plan := cveStatsChunkPlan(t, ctx, conn, length)
						if cveChunkPlanWalks(t, plan) {
							walks++
							if walks <= 5 {
								t.Errorf("%s %s %d ids %s random_page_cost=%s walked the advisory index:\n%s",
									label, probe.Name, length, mode, pageCost, supplyChainImpactPlanSummary(t, plan))
							}
						} else if problem := supplyChainImpactAnchorProblem(t, plan, factimpact.IndexVulnerabilityCVEV2); problem != "" {
							t.Errorf("%s %s %d ids %s random_page_cost=%s: %s\n%s",
								label, probe.Name, length, mode, pageCost, problem, supplyChainImpactPlanSummary(t, plan))
						}
					}
				}
				if _, err := conn.ExecContext(ctx, "DEALLOCATE cve_stats_probe"); err != nil {
					t.Fatalf("deallocate: %v", err)
				}
			}
		}
		return walks
	}

	// With migration 281 applied at bootstrap, no length may walk.
	set("random_page_cost = 1.1")
	set("plan_cache_mode = force_custom_plan")
	for _, length := range []int{1, 120, 180, 400} {
		est, actual, buffers := cveStatsSpot(t, ctx, conn, probes[0].SQL, length)
		t.Logf("with-stats n=%d: candidate est=%.0f actual=%.0f buffers=%d", length, est, actual, buffers)
	}
	if walks := sweep("with-stats"); walks != 0 {
		t.Fatalf("with-stats: %d advisory walks: migration 281's object does not hold the probe (RED)", walks)
	}

	// The control: without the object the same fixture must walk, so the
	// sweep above proves the object and not the fixture.
	if _, err := database.ExecContext(ctx, "DROP STATISTICS IF EXISTS "+cveStatsObjectName); err != nil {
		t.Fatalf("drop statistics object: %v", err)
	}
	if _, err := database.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze after drop: %v", err)
	}
	// ANALYZE recomputes the advisory expression's correlation from the
	// heap and wipes the walk plant, so store it again exactly as the
	// fixture did; otherwise the control proves nothing.
	cveStatsRestoreWalkCorrelation(t, ctx, database)
	// Same session settings as the with-stats spots above: the
	// before/after table is only comparable under identical boundaries.
	set("random_page_cost = 1.1")
	set("plan_cache_mode = force_custom_plan")
	for _, length := range []int{1, 120, 180, 400} {
		est, actual, buffers := cveStatsSpot(t, ctx, conn, probes[0].SQL, length)
		t.Logf("without-stats n=%d: candidate est=%.0f actual=%.0f buffers=%d", length, est, actual, buffers)
	}
	set("random_page_cost = 1.1")
	set("plan_cache_mode = force_custom_plan")
	if _, err := conn.ExecContext(ctx, "PREPARE cve_stats_probe(text[]) AS "+probes[0].SQL); err != nil {
		t.Fatalf("prepare control: %v", err)
	}
	firstWalk := 0
	for length := factimpact.MaxCVEIDsPerStatement + 1; length < cveChunkWalkFallsAt && firstWalk == 0; length++ {
		if cveChunkPlanWalks(t, cveStatsChunkPlan(t, ctx, conn, length)) {
			firstWalk = length
		}
	}
	if _, err := conn.ExecContext(ctx, "DEALLOCATE cve_stats_probe"); err != nil {
		t.Fatalf("deallocate control: %v", err)
	}
	if firstWalk == 0 {
		t.Fatalf("without-stats: no walk from %d to %d ids, so the fixture no longer reproduces the failure",
			factimpact.MaxCVEIDsPerStatement+1, cveChunkWalkFallsAt-1)
	}
	t.Logf("without-stats first walk at %d ids", firstWalk)

	// Re-apply the shipped migration bytes and prove the sweep is clean again.
	applyCVEStatsMigration(t, ctx, database)
	if _, err := database.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze after re-apply: %v", err)
	}
	if walks := sweep("re-applied"); walks != 0 {
		t.Fatalf("re-applied: %d advisory walks", walks)
	}
}

// cveStatsChunkPlan explains one prepared chunk in the session's settings.
func cveStatsChunkPlan(t *testing.T, ctx context.Context, conn *sql.Conn, length int) string {
	t.Helper()
	var raw []byte
	literal := supplyChainImpactSQLLiteral(t, cveChunkIDs(length))
	if err := conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) EXECUTE cve_stats_probe("+literal+")").Scan(&raw); err != nil {
		t.Fatalf("explain %d ids: %v", length, err)
	}
	return string(raw)
}

// cveStatsSpot runs EXPLAIN (ANALYZE, BUFFERS) for one list length and
// reports the candidate node's estimated rows, actual rows, and the root
// node's shared buffers, for the before/after evidence table.
func cveStatsSpot(t *testing.T, ctx context.Context, conn *sql.Conn, statement string, length int) (est, actual float64, buffers int64) {
	t.Helper()
	if _, err := conn.ExecContext(ctx, "PREPARE cve_stats_spot(text[]) AS "+statement); err != nil {
		t.Fatalf("prepare spot: %v", err)
	}
	defer func() {
		_, _ = conn.ExecContext(ctx, "DEALLOCATE cve_stats_spot")
	}()
	var raw []byte
	literal := supplyChainImpactSQLLiteral(t, cveChunkIDs(length))
	if err := conn.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE cve_stats_spot("+literal+")").Scan(&raw); err != nil {
		t.Fatalf("explain analyze %d ids: %v", length, err)
	}
	nodes := supplyChainImpactPlanNodes(t, string(raw))
	if len(nodes) == 0 {
		t.Fatalf("spot %d: empty plan", length)
	}
	buffers = nodes[0].SharedHit + nodes[0].SharedRead + nodes[0].SharedDirtied + nodes[0].SharedWritten
	cve := factimpact.IndexVulnerabilityCVEV2[:min(len(factimpact.IndexVulnerabilityCVEV2), 63)]
	for _, node := range nodes {
		if node.IndexName == cve {
			return node.PlanRows * max(node.ActualLoops, 1), node.ActualRows * node.ActualLoops, buffers
		}
	}
	t.Logf("spot %d: no CVE-index node in plan:\n%s", length, supplyChainImpactPlanSummary(t, string(raw)))
	return 0, 0, buffers
}

// cveStatsRestoreWalkCorrelation stores the walk fixture's advisory
// correlation again. It duplicates the pg_restore_attribute_stats step of
// seedCVEChunkWalkFixture (which this test seeds through): any ANALYZE
// after the seed recomputes the correlation from the heap and unplants
// the walk the control needs.
func cveStatsRestoreWalkCorrelation(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	advisory := factimpact.IndexVulnerabilityAdvisoryV2
	advisory = advisory[:min(len(advisory), 63)]
	if _, err := database.ExecContext(ctx, fmt.Sprintf(`
SELECT pg_restore_attribute_stats('schemaname', current_schema()::text, 'relname', '%s',
  'attname', 'expr', 'inherited', false, 'correlation', %s::real)`, advisory, cveChunkWalkCorrelation)); err != nil {
		t.Fatalf("store correlation %s: %v", cveChunkWalkCorrelation, err)
	}
}

// TestSupplyChainImpactSecurityAlertCVEProbeBeforeAfterStatsLive records
// the before/after plans of the sibling cve_id probe (#10050 acceptance
// item 4): security_alert_cve_id reads the same payload->>'cve_id'
// expression through its own partial index over one kind. The object feeds
// its predicate too, with a different kind fraction, so this test sweeps it
// rather than assuming benefit. It gates anchor-hold with the object and
// logs the without-object plans for the evidence table.
func TestSupplyChainImpactSecurityAlertCVEProbeBeforeAfterStatsLive(t *testing.T) {
	ctx, database := openSupplyChainImpactEvidenceProofDB(t)
	seedSecurityAlertCVEFixture(t, ctx, database)
	var statement string
	for _, branch := range factimpact.Branches() {
		if branch.Name == "security_alert_cve_id" {
			statement = branch.SQL
		}
	}
	if statement == "" {
		t.Fatal("catalog has no security_alert_cve_id branch")
	}
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
	explain := func(label string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, "PREPARE alert_probe(text[]) AS "+statement); err != nil {
			t.Fatalf("%s: prepare: %v", label, err)
		}
		defer func() {
			_, _ = conn.ExecContext(ctx, "DEALLOCATE alert_probe")
		}()
		for _, pageCost := range []string{"1.1", "4"} {
			set("random_page_cost = " + pageCost)
			for _, mode := range supplyChainImpactPlanModes {
				set("plan_cache_mode = " + mode)
				for _, length := range []int{120, 400} {
					var raw []byte
					literal := supplyChainImpactSQLLiteral(t, cveChunkIDs(length))
					if err := conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) EXECUTE alert_probe("+literal+")").Scan(&raw); err != nil {
						t.Fatalf("%s: explain %d ids: %v", label, length, err)
					}
					plan := string(raw)
					if problem := supplyChainImpactAnchorProblem(t, plan, factimpact.IndexSecurityAlertCVEID); problem != "" {
						t.Errorf("%s %d ids %s random_page_cost=%s: %s\n%s",
							label, length, mode, pageCost, problem, supplyChainImpactPlanSummary(t, plan))
					} else {
						t.Logf("%s %d ids %s random_page_cost=%s holds %s",
							label, length, mode, pageCost, factimpact.IndexSecurityAlertCVEID)
					}
				}
			}
		}
	}
	explain("with-stats")
	if _, err := database.ExecContext(ctx, "DROP STATISTICS IF EXISTS "+cveStatsObjectName); err != nil {
		t.Fatalf("drop statistics object: %v", err)
	}
	if _, err := database.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze after drop: %v", err)
	}
	explain("without-stats")
	applyCVEStatsMigration(t, ctx, database)
	if _, err := database.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze after re-apply: %v", err)
	}
}

// seedSecurityAlertCVEFixture seeds one scope with 1,500 security-alert
// rows over 1,500 distinct cve_ids on a filler heap (fillfactor 10, the
// same heap-weight trick as seedCVEChunkPlanFixture) so an index plan beats
// a sequential scan on shape, not on statistics.
func seedSecurityAlertCVEFixture(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	if _, err := database.ExecContext(ctx, "ALTER TABLE fact_records SET (fillfactor = 10)"); err != nil {
		t.Fatalf("set fillfactor: %v", err)
	}
	supplyChainImpactFixtureScope(t, ctx, database, "vuln:alerts", "vuln:alerts", `{}`, "gen:alerts:active", nil)
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload
)
SELECT format('alert:%s', n), 'vuln:alerts', 'gen:alerts:active', 'security_alert.repository_alert',
  format('stable:alert:%s', n), '1.0.0', 'synthetic', 1, 'reported', 'synthetic',
  format('source:alert:%s', n), '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z', false,
  jsonb_build_object('cve_id', format('CVE-2024-%s', lpad((n - 1)::text, 5, '0')))
FROM generate_series(1, 1500) AS n`); err != nil {
		t.Fatalf("seed alert rows: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
  observed_at, ingested_at, payload
)
SELECT format('alert:filler:%s', n), 'vuln:alerts', 'gen:alerts:active', 'chunk.filler', format('stable:alert:filler:%s', n),
  '1.0.0', 'synthetic', 1, 'reported', 'synthetic', format('source:alert:filler:%s', n),
  '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z', '{"x":"y"}'::jsonb
FROM generate_series(1, 60000) AS n`); err != nil {
		t.Fatalf("seed filler: %v", err)
	}
	supplyChainImpactFixtureAnalyze(t, ctx, database)
}

// applyCVEStatsMigration re-applies migration 281's shipped SQL bytes, so
// the re-applied arm of the proof runs the same statements bootstrap ran.
func applyCVEStatsMigration(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for _, def := range BootstrapDefinitions() {
		if strings.HasSuffix(def.Path, "/"+cveStatsMigrationFile) {
			if _, err := database.ExecContext(ctx, def.SQL); err != nil {
				t.Fatalf("re-apply %s: %v", cveStatsMigrationFile, err)
			}
			return
		}
	}
	t.Fatalf("bootstrap has no %s", cveStatsMigrationFile)
}
