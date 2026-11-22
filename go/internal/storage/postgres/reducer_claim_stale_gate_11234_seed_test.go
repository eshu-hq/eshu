// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// staleGate11234Shape sizes the #11234 QA-shaped seed: every scope has an
// active generation, some scopes carry older generations, and the live
// reducer backlog sits on active generations unless staleRows plants rows
// on older ones.
type staleGate11234Shape struct {
	scopes          int // scopes with an active generation
	olderGens       int // older (superseded) generations, spread over scopes
	liveRows        int // pending/retrying reducer rows on active generations
	retryingEvery   int // every Nth live row is retrying
	liveScopes      int // scopes the live rows spread over
	terminalPerGen  int // succeeded reducer rows per older generation
	staleRows       int // pending/retrying reducer rows on older generations
	staleRetryEvery int // every Nth stale row is retrying
	// physicalQA pads scope and generation rows to about one per heap page
	// and dirties their visibility map after VACUUM, matching the QA read
	// (13,064 buffers for 12,236 scopes; 12,307 heap fetches per claim).
	physicalQA bool
}

// staleGate11234QAShape is the measured QA shape (shim-11234 plan read,
// 2026-11-22): 12,236 active scopes, 13,366 older generations probed, about
// 10,000 pending/retrying reducer rows, no stale row.
func staleGate11234QAShape() staleGate11234Shape {
	return staleGate11234Shape{
		scopes:          12236,
		olderGens:       13366,
		liveRows:        10000,
		retryingEvery:   4,
		liveScopes:      2000,
		terminalPerGen:  20,
		staleRetryEvery: 2,
	}
}

// staleGate11234Domains are the reducer domains the seeded rows rotate over,
// taken from the QA waiting mix (#10862 replay) plus the two domains the
// gate treats specially (supply-chain fan-out spare, Crossplane carrier).
var staleGate11234Domains = []string{
	"supply_chain_impact",
	"advisory_group_projection",
	"workload_materialization",
	"deployment_mapping",
	"deployable_unit_correlation",
	"container_image_identity",
	"platform_infra_materialization",
	"crossplane_satisfied_by_materialization",
}

// seedStaleGate11234 plants the shape on an empty benchmark schema
// (createReducerClaimBenchmarkSchema). Scope i's active generation is
// gen-a-i, ingested at base+1h; older generation k of scope s is
// gen-o-s-k, ingested at base+k minutes, so it is always older than active.
func seedStaleGate11234(ctx context.Context, t testing.TB, conn db.Executor, shape staleGate11234Shape) {
	t.Helper()
	base := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	active := base.Add(time.Hour)
	exec := func(label, query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %s: %v", label, err)
		}
	}
	exec("scopes", `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'scope-' || i, 'repository', 'git', 'repo-' || i, NULL, 'git', 'repo-' || i,
       $1::timestamptz, $1::timestamptz, 'active', 'gen-a-' || i, '{}'::jsonb
FROM generate_series(1, $2::int) AS s(i)`, active, shape.scopes)
	exec("active generations", `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint,
    observed_at, ingested_at, status, activated_at, superseded_at, payload)
SELECT 'gen-a-' || i, 'scope-' || i, 'snapshot', 'seed', $1::timestamptz, $1::timestamptz,
       'active', $1::timestamptz, NULL, '{}'::jsonb
FROM generate_series(1, $2::int) AS s(i)`, active, shape.scopes)
	// Older generation j (1-based) lands on scope ((j-1) % scopes)+1 as its
	// k-th older generation, k = ((j-1) / scopes)+1.
	exec("older generations", `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint,
    observed_at, ingested_at, status, activated_at, superseded_at, payload)
SELECT 'gen-o-' || sc || '-' || k, 'scope-' || sc, 'snapshot', 'seed',
       $1::timestamptz + make_interval(mins => k), $1::timestamptz + make_interval(mins => k),
       'superseded', $1::timestamptz + make_interval(mins => k), $2::timestamptz, '{}'::jsonb
FROM (SELECT ((j - 1) % $3::int) + 1 AS sc, ((j - 1) / $3::int) + 1 AS k
      FROM generate_series(1, $4::int) AS s(j)) AS o`, base, active, shape.scopes, shape.olderGens)
	exec("terminal rows", `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, payload, created_at, updated_at)
SELECT 'term-' || g.generation_id || '-' || n, g.scope_id, g.generation_id, 'reducer',
       ($2::text[])[1 + (n % 8)], 'scope', NULL, 'succeeded', 1,
       jsonb_build_object('source_system', 'seed'), $1::timestamptz, $1::timestamptz
FROM scope_generations AS g
CROSS JOIN generate_series(1, $3::int) AS s(n)
WHERE g.status = 'superseded'`, base, staleGate11234Domains, shape.terminalPerGen)
	exec("live rows", staleGate11234RowInsert("'live-' || i", "'scope-' || sc", "'gen-a-' || sc",
		"(SELECT i, ((i - 1) % $5::int) + 1 AS sc FROM generate_series(1, $3::int) AS s(i)) AS l"),
		active, staleGate11234Domains, shape.liveRows, shape.retryingEvery, shape.liveScopes)
	if shape.staleRows > 0 {
		// Stale row i sits on scope i's first older generation (scope i
		// has one whenever olderGens >= scopes).
		exec("stale rows", staleGate11234RowInsert("'stale-' || i", "'scope-' || i", "'gen-o-' || i || '-1'",
			"(SELECT i, $5::int AS sc FROM generate_series(1, $3::int) AS s(i)) AS l"),
			base, staleGate11234Domains, shape.staleRows, shape.staleRetryEvery, 0)
	}
	if shape.physicalQA {
		// About one row per page: QA's seq scans read ~1 buffer per row.
		exec("pad scopes", `UPDATE ingestion_scopes SET payload = jsonb_build_object('pad', repeat(md5(scope_id), 150))`)
		exec("pad generations", `UPDATE scope_generations SET payload = jsonb_build_object('pad', repeat(md5(generation_id), 150))`)
		exec("vacuum", `VACUUM (ANALYZE) ingestion_scopes, scope_generations, fact_work_items`)
		// Churn after VACUUM clears all-visible bits, as QA's live status
		// updates do, so index-only scans pay heap fetches.
		exec("churn generations", `UPDATE scope_generations SET freshness_hint = 'churn'`)
		exec("churn live rows", `UPDATE fact_work_items SET updated_at = updated_at WHERE status IN ('pending', 'retrying')`)
		// Terminal rows walked pending, retrying, pending, succeeded; with
		// autovacuum behind (QA), their older index versions stay behind
		// for the pending/retrying probes to visit.
		exec("no autovacuum", `ALTER TABLE fact_work_items SET (autovacuum_enabled = false)`)
		for _, status := range []string{"pending", "retrying", "pending", "succeeded"} {
			exec("terminal walk "+status, `UPDATE fact_work_items SET status = $1 WHERE work_item_id LIKE 'term-%'`, status)
		}
	}
	if err := analyzeReducerClaimBenchmarkTables(ctx, conn); err != nil {
		t.Fatalf("analyze seed: %v", err)
	}
}

// staleGate11234RowInsert renders a pending/retrying reducer-row insert over
// the given source: $1 timestamp, $2 domains, $3 count, $4 retry modulus.
func staleGate11234RowInsert(idExpr, scopeExpr, generationExpr, source string) string {
	return `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, visible_at, next_attempt_at, failure_class, payload,
    created_at, updated_at)
SELECT ` + idExpr + `, ` + scopeExpr + `, ` + generationExpr + `, 'reducer', ($2::text[])[1 + (i % 8)],
       'scope', NULL,
       CASE WHEN i % $4::int = 0 THEN 'retrying' ELSE 'pending' END,
       CASE WHEN i % $4::int = 0 THEN 1 ELSE 0 END,
       CASE WHEN i % $4::int = 0 THEN $1::timestamptz ELSE NULL END,
       CASE WHEN i % $4::int = 0 THEN $1::timestamptz ELSE NULL END,
       CASE WHEN i % $4::int = 0 THEN 'reducer_retryable' ELSE NULL END,
       jsonb_build_object('source_system', 'seed', 'entity_key', 'e-' || i),
       $1::timestamptz + make_interval(secs => i), $1::timestamptz + make_interval(secs => i)
FROM ` + source
}

// staleGate11234ChainCut is the shipped WITH list cut after the stale gate,
// counting the stale set: the gate chain alone, derived from the constants.
// Only $2 (the domain filter) is referenced by the gate; the read-only tail
// types $1 so the claim's placeholder numbering stays verbatim (42P18).
func staleGate11234ChainCut() string {
	return "\nWITH " + reducerClaimReadinessRequirementsCTE() + ",\n" + reducerClaimStaleGateCTEs() +
		"\nSELECT count(*) FROM stale_reducer_candidates WHERE ($1::timestamptz IS NULL OR $1::timestamptz IS NOT NULL)"
}
