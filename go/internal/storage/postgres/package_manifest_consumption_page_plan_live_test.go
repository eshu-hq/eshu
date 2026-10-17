// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7663 disposable PostgreSQL 18 plan proof that the backfill page query is an index scan on migration 181's partial index with no sort, sequential scan, or residual filter, with flat buffers from the first to the last page under custom and generic plans. Asserts plan shape and buffers, never wall time; local run 1.7s on postgres 18.6 on 2026-10-15
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"encoding/json"
	"testing"
)

// TestPackageManifestConsumptionScopeFactsPageUsesBackfillIndexLive runs the
// shipped page query under both custom and generic plans on a scope whose
// manifest facts are a minority, and requires every page to be an index scan
// on migration 181's partial index with no sort or sequential scan and with
// buffer use that stays flat from the first page to the last. It asserts plan
// shape and buffers, never wall time.
func TestPackageManifestConsumptionScopeFactsPageUsesBackfillIndexLive(t *testing.T) {
	ctx, database := openPackageManifestConsumptionPageProofDatabase(t)
	seedPackageManifestConsumptionBackfillScope(t, ctx, database)
	const pages = 8
	seedPackageManifestConsumptionPageFacts(t, ctx, database, 0, pages*packageManifestConsumptionKeyBackfillBatchSize)
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'page-fact-' || lpad(i::text, 6, '0') || '-fn', 'sidecar-live-scope', 'sidecar-live-generation',
       'content_entity', 'fn-' || i, 'git', 'fn-' || i, clock_timestamp(), clock_timestamp(), FALSE,
       jsonb_build_object('repo_id', 'repository:sidecar-live', 'entity_type', 'Function', 'entity_name', 'fn' || i)
FROM generate_series(0, 15999) AS i`); err != nil {
		t.Fatalf("seed non-manifest facts: %v", err)
	}
	if _, err := database.ExecContext(ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze fact_records: %v", err)
	}

	lateCursor := packageManifestConsumptionPageFactID((pages-1)*packageManifestConsumptionKeyBackfillBatchSize - 1)
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		conn, err := database.Conn(ctx)
		if err != nil {
			t.Fatalf("open plan probe connection: %v", err)
		}
		if _, err := conn.ExecContext(ctx, `SELECT set_config('plan_cache_mode', $1, false)`, mode); err != nil {
			t.Fatalf("set plan_cache_mode %s: %v", mode, err)
		}
		if _, err := conn.ExecContext(ctx, `PREPARE page_probe(text, text, int) AS `+listPackageManifestConsumptionKeyScopeFactsSQL); err != nil {
			t.Fatalf("prepare page query: %v", err)
		}
		buffers := make([]float64, 0, 2)
		for _, cursor := range []string{"", lateCursor} {
			var raw string
			if err := conn.QueryRowContext(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE page_probe('sidecar-live-scope', '`+cursor+`', 500)`).Scan(&raw); err != nil {
				t.Fatalf("explain page (%s, cursor %q): %v", mode, cursor, err)
			}
			var explained []struct {
				Plan map[string]any `json:"Plan"`
			}
			if err := json.Unmarshal([]byte(raw), &explained); err != nil || len(explained) != 1 {
				t.Fatalf("decode page plan (%s): %v", mode, err)
			}
			plan := explained[0].Plan
			assertPackageManifestConsumptionPagePlan(t, mode, cursor, plan)
			hit, _ := plan["Shared Hit Blocks"].(float64)
			read, _ := plan["Shared Read Blocks"].(float64)
			buffers = append(buffers, hit+read)
		}
		if _, err := conn.ExecContext(ctx, `DEALLOCATE page_probe`); err != nil {
			t.Fatalf("deallocate page query: %v", err)
		}
		_ = conn.Close()
		first, late := buffers[0], buffers[1]
		if first > 1000 || late > 1000 {
			t.Fatalf("%s page buffers first=%.0f late=%.0f, want each <= 1000 for a 500-row page", mode, first, late)
		}
		if late > first*1.25+8 || first > late*1.25+8 {
			t.Fatalf("%s page buffers first=%.0f late=%.0f, want them flat across the scope", mode, first, late)
		}
		t.Logf("%s page buffers first=%.0f late=%.0f", mode, first, late)
	}
}

func assertPackageManifestConsumptionPagePlan(t *testing.T, mode, cursor string, plan map[string]any) {
	t.Helper()
	if rows, _ := plan["Actual Rows"].(float64); rows != float64(packageManifestConsumptionKeyBackfillBatchSize) {
		t.Fatalf("%s page (cursor %q) returned %.0f rows, want %d", mode, cursor, rows, packageManifestConsumptionKeyBackfillBatchSize)
	}
	usesIndex := false
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		switch node["Node Type"] {
		case "Sort", "Incremental Sort", "Seq Scan", "Bitmap Heap Scan":
			t.Fatalf("%s page (cursor %q) plan has a %v node, want an ordered partial-index scan", mode, cursor, node["Node Type"])
		case "Index Scan":
			if node["Index Name"] == packageManifestConsumptionBackfillIndexName {
				usesIndex = true
				if _, filtered := node["Filter"]; filtered {
					t.Fatalf("%s page (cursor %q) index scan applies a residual Filter %v, want the predicate implied by the partial index", mode, cursor, node["Filter"])
				}
			}
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			if childNode, ok := child.(map[string]any); ok {
				walk(childNode)
			}
		}
	}
	walk(plan)
	if !usesIndex {
		t.Fatalf("%s page (cursor %q) plan does not scan %s", mode, cursor, packageManifestConsumptionBackfillIndexName)
	}
}
