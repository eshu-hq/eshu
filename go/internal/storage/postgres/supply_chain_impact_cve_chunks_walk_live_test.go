// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9994 disposable PostgreSQL 18 proof that every key probe on the CVE index stays on its anchor up to the statement bound on a fixture that reproduces the advisory-index walk above it.
//eshu:readiness:env ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DISPOSABLE ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
)

// cveChunkWalkCorrelation is the correlation the walk fixture stores for the
// advisory index's expression. It is not a QA value. At 0.8 the first walk is at 154
// ids, below 172, QA's measured flip, and above the bound, so a bound raised to QA's
// flip fails through the plan. At 0.6 the first walk is at 177 and such a bound passes.
// The cost uses the square of this value, so -0.6 behaves as 0.6.
const cveChunkWalkCorrelation = "0.8"

// cveChunkWalkFallsAt is the list length where scalararraysel's sum of the
// default 1/200 per element passes 1, so the estimate falls and the walk stops.
const cveChunkWalkFallsAt = 200

// TestSupplyChainImpactCVEBoundStaysBelowTheWalkLive plants the failure the bound
// exists for: the real catalog statement, on the real schema, reading the advisory
// index end to end. The other plan test proves the anchor on a fixture that never
// walks, so a bound above the walk passes there.
func TestSupplyChainImpactCVEBoundStaysBelowTheWalkLive(t *testing.T) {
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
	const pageCost, mode = "1.1", "force_custom_plan"
	set("random_page_cost = " + pageCost)
	set("plan_cache_mode = " + mode)
	for _, probe := range cveKeyProbes(t) {
		if _, err := conn.ExecContext(ctx, "PREPARE cve_chunk(text[]) AS "+probe.SQL); err != nil {
			t.Fatalf("prepare %s: %v", probe.Name, err)
		}
		for length := 1; length <= factimpact.MaxCVEIDsPerStatement; length++ {
			plan := cveChunkPlan(t, ctx, conn, length)
			if problem := supplyChainImpactAnchorProblem(t, plan, factimpact.IndexVulnerabilityCVEV2); problem != "" {
				t.Errorf("%s %d ids %s random_page_cost=%s: %s\n%s", probe.Name, length, mode, pageCost, problem, supplyChainImpactPlanSummary(t, plan))
			}
		}
		// The control: without a walk above the bound this test proves nothing.
		firstWalk := 0
		for length := factimpact.MaxCVEIDsPerStatement + 1; length < cveChunkWalkFallsAt && firstWalk == 0; length++ {
			if cveChunkPlanWalks(t, cveChunkPlan(t, ctx, conn, length)) {
				firstWalk = length
			}
		}
		if firstWalk == 0 {
			t.Errorf("%s: no walk from %d to %d ids, so the fixture no longer reproduces the failure", probe.Name, factimpact.MaxCVEIDsPerStatement+1, cveChunkWalkFallsAt-1)
		}
		t.Logf("%s first walk at %d ids, bound %d", probe.Name, firstWalk, factimpact.MaxCVEIDsPerStatement)
		if _, err := conn.ExecContext(ctx, "DEALLOCATE cve_chunk"); err != nil {
			t.Fatalf("deallocate: %v", err)
		}
	}
}

func cveChunkPlanWalks(t *testing.T, plan string) bool {
	t.Helper()
	advisory := factimpact.IndexVulnerabilityAdvisoryV2
	advisory = advisory[:min(len(advisory), 63)]
	for _, node := range supplyChainImpactPlanNodes(t, plan) {
		if node.IndexName == advisory {
			return true
		}
	}
	return false
}

// seedCVEChunkWalkFixture builds the shape that walks: wide pages, no index on
// fact_records but the two in question and the primary key, and the given
// correlation stored for the advisory index's expression. Heap order does not decide
// the walk; the seed only happens to shuffle. Autovacuum is off
// because an automatic ANALYZE would overwrite that stored value during the test.
// pg_restore_attribute_stats needs PostgreSQL 18.
func seedCVEChunkWalkFixture(t *testing.T, ctx context.Context, database *sql.DB, correlation string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, "ALTER TABLE fact_records SET (fillfactor = 10, autovacuum_enabled = false)"); err != nil {
		t.Fatalf("set table options: %v", err)
	}
	supplyChainImpactFixtureScope(t, ctx, database, "vuln:chunks", "vuln:chunks", `{}`, "gen:chunks:active",
		map[string]string{"gen:chunks:old": "superseded"})
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload
)
SELECT format('walk:%s', i), 'vuln:chunks', 'gen:chunks:active',
  CASE WHEN i % 12 = 0
    THEN (ARRAY['vulnerability.cve', 'vulnerability.affected_package', 'vulnerability.affected_product',
                'vulnerability.epss_score', 'vulnerability.known_exploited'])[1 + (i / 12) % 5]
    ELSE 'chunk.filler' END,
  format('stable:walk:%s', i), '1.0.0', 'synthetic', 1, 'reported', 'synthetic', format('source:walk:%s', i),
  '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z', false,
  CASE WHEN i % 12 = 0
    THEN jsonb_build_object('cve_id', format('CVE-2024-%s', lpad(((i / 12) % 1200)::text, 5, '0')),
                            'advisory_id', format('ADV-%s', (i / 12) % 1200))
    ELSE '{"x":"y"}'::jsonb END
FROM generate_series(1, 150000) AS i
ORDER BY md5(i::text)`); err != nil {
		t.Fatalf("seed walk fixture: %v", err)
	}
	dropCVEChunkCompetingIndexes(t, ctx, database)
	supplyChainImpactFixtureAnalyze(t, ctx, database)
	advisory := factimpact.IndexVulnerabilityAdvisoryV2
	advisory = advisory[:min(len(advisory), 63)]
	if _, err := database.ExecContext(ctx, fmt.Sprintf(`
SELECT pg_restore_attribute_stats('schemaname', current_schema()::text, 'relname', '%s',
  'attname', 'expr', 'inherited', false, 'correlation', %s::real)`, advisory, correlation)); err != nil {
		t.Fatalf("store correlation %s: %v", correlation, err)
	}
}

// dropCVEChunkCompetingIndexes keeps the CVE and advisory indexes and whatever backs
// a constraint. With the full set the plan leaves the anchor through another index
// instead, which hides the walk this fixture plants.
func dropCVEChunkCompetingIndexes(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	cve := factimpact.IndexVulnerabilityCVEV2
	advisory := factimpact.IndexVulnerabilityAdvisoryV2
	rows, err := database.QueryContext(ctx, `
SELECT format('%I', i.indexname)
FROM pg_indexes AS i
WHERE i.schemaname = current_schema() AND i.tablename = 'fact_records' AND i.indexname NOT IN ($1, $2)
  AND NOT EXISTS (SELECT 1 FROM pg_constraint AS c WHERE c.conname = i.indexname)`,
		cve[:min(len(cve), 63)], advisory[:min(len(advisory), 63)])
	if err != nil {
		t.Fatalf("list competing indexes: %v", err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			t.Fatalf("scan index name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read competing indexes: %v", err)
	}
	_ = rows.Close()
	for _, name := range names {
		if _, err := database.ExecContext(ctx, "DROP INDEX "+name); err != nil {
			t.Fatalf("drop %s: %v", name, err)
		}
	}
}
