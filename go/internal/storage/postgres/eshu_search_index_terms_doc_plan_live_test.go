// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestEshuSearchIndexTermsDocumentDeletePlanLive proves — against a live
// Postgres instance with real corpus data — that the document-keyed index
// eshu_search_index_terms_doc_idx changes the query plan for the hot
// per-page refresh DELETE (document_id = ANY) from a full (scope,generation)
// PK slice scan to a targeted index seek on document_id.
//
// Environment gates (both required to run):
//
//	ESHU_SEARCH_INDEX_PLAN_LIVE=1
//	ESHU_POSTGRES_DSN=postgresql://eshu:<pw>@localhost:15432/eshu?sslmode=disable
//
// The test selects a real (scope_id, generation_id) pair that has at least
// 10 000 term rows so the planner has a meaningful slice to optimize. EXPLAIN
// runs are wrapped in transactions that are always rolled back so no rows are
// ever deleted. The doc index is dropped before the "before" plan and
// recreated before the "after" plan; both operations use IF NOT EXISTS /
// IF EXISTS so they are safe on an already-bootstrapped DB.
//
// Note: ApplyBootstrap is intentionally NOT called here. The schema must
// already exist on the target DB. Calling ApplyBootstrap on a populated
// 73 M-row DB applies all DDL including slow CREATE INDEX operations under
// lock, which is unacceptable in a test context.
func TestEshuSearchIndexTermsDocumentDeletePlanLive(t *testing.T) {
	if os.Getenv("ESHU_SEARCH_INDEX_PLAN_LIVE") != "1" {
		t.Skip("set ESHU_SEARCH_INDEX_PLAN_LIVE=1 and ESHU_POSTGRES_DSN to run live plan proof")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run live plan proof")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Pick a real (scope_id, generation_id) pair with a large term slice so the
	// planner has meaningful statistics. We need at least 10 000 rows to make
	// the document-keyed vs. full-slice difference observable in the plan.
	var scopeID, generationID string
	var termCount int64
	err = db.QueryRowContext(ctx, `
		SELECT scope_id, generation_id, count(*) AS cnt
		FROM eshu_search_index_terms
		GROUP BY scope_id, generation_id
		ORDER BY cnt DESC
		LIMIT 1
	`).Scan(&scopeID, &generationID, &termCount)
	if err != nil {
		t.Fatalf("select large scope: %v", err)
	}
	if termCount < 10000 {
		t.Skipf("largest (scope,gen) slice has only %d term rows — need ≥10000 for meaningful plan comparison", termCount)
	}
	t.Logf("proof scope=%s generation=%s term_rows=%d", scopeID, generationID, termCount)

	// Pick a small sample of document_ids from the chosen scope to use as the
	// = ANY target (mimicking a per-page refresh of 10 documents).
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT document_id
		FROM eshu_search_index_terms
		WHERE scope_id = $1 AND generation_id = $2
		LIMIT 10
	`, scopeID, generationID)
	if err != nil {
		t.Fatalf("select sample document_ids: %v", err)
	}
	var targetDocIDs []string
	for rows.Next() {
		var docID string
		if scanErr := rows.Scan(&docID); scanErr == nil {
			targetDocIDs = append(targetDocIDs, docID)
		}
	}
	_ = rows.Close()
	if len(targetDocIDs) == 0 {
		t.Fatal("no document_ids found for chosen scope")
	}
	t.Logf("targeting %d document_ids for EXPLAIN", len(targetDocIDs))

	// refreshDeleteQuery is the production refresh query shape (= ANY) used by
	// eshuSearchIndexRefreshDocumentTermsQuery. We EXPLAIN this without
	// actually deleting anything — the transaction is always rolled back.
	const refreshDeleteQuery = `
DELETE FROM eshu_search_index_terms
WHERE scope_id      = $1
  AND generation_id = $2
  AND document_id   = ANY($3::text[])
`

	// explainJSON runs EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) on the DELETE
	// inside a transaction that is always rolled back.
	explainJSON := func(query string, args ...any) string {
		t.Helper()
		tx, txErr := db.BeginTx(ctx, nil)
		if txErr != nil {
			t.Fatalf("begin explain tx: %v", txErr)
		}
		defer func() { _ = tx.Rollback() }()

		explain := "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) " + query
		var raw []byte
		if scanErr := tx.QueryRowContext(ctx, explain, args...).Scan(&raw); scanErr != nil {
			t.Fatalf("EXPLAIN query error: %v", scanErr)
		}
		return string(raw)
	}

	// --- Phase 1: ensure the doc index is absent, capture baseline plan ---
	if _, dropErr := db.ExecContext(ctx, `DROP INDEX IF EXISTS eshu_search_index_terms_doc_idx`); dropErr != nil {
		t.Fatalf("drop doc index: %v", dropErr)
	}

	planBefore := explainJSON(refreshDeleteQuery, scopeID, generationID, targetDocIDs)
	t.Logf("=== PLAN BEFORE (no doc index) ===\n%s", planJSONSummary(t, planBefore))
	rowsBefore, bufsBefore := planInnerScanMetrics(t, planBefore)
	t.Logf("before: inner_rows_removed_by_filter=%d shared_blocks=%d", rowsBefore, bufsBefore)

	// --- Phase 2: create the doc index, re-analyze, capture improved plan ---
	if _, createErr := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS eshu_search_index_terms_doc_idx
		    ON eshu_search_index_terms (scope_id, generation_id, document_id)
	`); createErr != nil {
		t.Fatalf("create doc index: %v", createErr)
	}
	// ANALYZE so the planner picks up the new index statistics.
	if _, analyzeErr := db.ExecContext(ctx, `ANALYZE eshu_search_index_terms`); analyzeErr != nil {
		t.Logf("ANALYZE warning (non-fatal): %v", analyzeErr)
	}

	planAfter := explainJSON(refreshDeleteQuery, scopeID, generationID, targetDocIDs)
	t.Logf("=== PLAN AFTER (with doc index) ===\n%s", planJSONSummary(t, planAfter))
	rowsAfter, bufsAfter := planInnerScanMetrics(t, planAfter)
	t.Logf("after: inner_rows_removed_by_filter=%d shared_blocks=%d", rowsAfter, bufsAfter)

	// Assert: with-index plan must reference the document-keyed index.
	if !strings.Contains(planAfter, "eshu_search_index_terms_doc_idx") {
		t.Errorf("WITH-index plan does not reference eshu_search_index_terms_doc_idx\n"+
			"The planner preferred a different path; check index statistics and cardinality.\n"+
			"Plan:\n%s", planAfter)
	}

	// Assert: with-index plan uses an index scan.
	if !strings.Contains(planAfter, "Index Scan") && !strings.Contains(planAfter, "Bitmap Index") {
		t.Errorf("WITH-index plan does not show an Index Scan or Bitmap Index Scan\n"+
			"Plan:\n%s", planAfter)
	}

	// Assert: the with-index plan scans fewer blocks than the without-index plan.
	// On a large table with real data the doc index should cut buffer reads
	// dramatically (from O(scope_slice) to O(target_docs × terms_per_doc)).
	if bufsAfter > 0 && bufsBefore > 0 {
		improvement := float64(bufsBefore-bufsAfter) / float64(bufsBefore) * 100
		t.Logf("buffer block improvement: %.1f%% (%d → %d)", improvement, bufsBefore, bufsAfter)
		if bufsAfter >= bufsBefore {
			t.Errorf("WITH-index plan read %d blocks vs WITHOUT-index %d blocks — expected fewer blocks with doc index",
				bufsAfter, bufsBefore)
		}
	}
}

// planJSONSummary returns a pretty-printed top-level plan node for test logging.
func planJSONSummary(t *testing.T, raw string) string {
	t.Helper()
	var result []struct {
		Plan json.RawMessage `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil || len(result) == 0 {
		return raw
	}
	pretty, err := json.MarshalIndent(result[0].Plan, "", "  ")
	if err != nil {
		return raw
	}
	return string(pretty)
}

// planInnerScanMetrics walks the first EXPLAIN plan node and its first child
// (the actual scan node) to extract rows-removed-by-filter and total shared
// buffer blocks. These are the key metrics showing how much work the planner
// avoided with the document-keyed index.
func planInnerScanMetrics(t *testing.T, raw string) (rowsRemovedByFilter, sharedBlocks int64) {
	t.Helper()
	type planNode struct {
		RowsRemovedByFilter int64      `json:"Rows Removed by Filter"`
		SharedHitBlocks     int64      `json:"Shared Hit Blocks"`
		SharedReadBlocks    int64      `json:"Shared Read Blocks"`
		Plans               []planNode `json:"Plans"`
	}
	var result []struct {
		Plan planNode `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil || len(result) == 0 {
		return 0, 0
	}
	top := result[0].Plan
	sharedBlocks = top.SharedHitBlocks + top.SharedReadBlocks
	// The scan metrics live on the child node (the Index Scan under the ModifyTable).
	if len(top.Plans) > 0 {
		child := top.Plans[0]
		rowsRemovedByFilter = child.RowsRemovedByFilter
		if child.SharedHitBlocks+child.SharedReadBlocks > 0 {
			sharedBlocks = child.SharedHitBlocks + child.SharedReadBlocks
		}
	}
	return rowsRemovedByFilter, sharedBlocks
}
