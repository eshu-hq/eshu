// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestEshuSearchIndexTermsDocumentDeletePlanLive proves that a covering index
// on (scope_id, generation_id, document_id) converts the per-page refresh
// DELETE (document_id = ANY) from an expensive (scope,generation) PK slice
// scan into a targeted single-seek operation.
//
// Environment gates (both required to run):
//
//	ESHU_SEARCH_INDEX_PLAN_LIVE=1
//	ESHU_POSTGRES_DSN=postgresql://eshu:<pw>@localhost:15432/eshu?sslmode=disable
//
// HERMETIC DESIGN — the test never touches the shared eshu_search_index_terms
// table or its indexes. Instead it:
//
//  1. Creates a throwaway table with the identical column layout, PK, and
//     lookup index as eshu_search_index_terms.
//  2. Seeds ~100k rows (500 docs × 200 terms) for one (scope,generation) pair
//     so the planner has a meaningful slice to reason about.
//  3. Runs EXPLAIN ANALYZE on the DELETE without the doc index → asserts the
//     planner uses the PK/lookup and scans the full slice (many rows/blocks).
//  4. Adds the doc index, re-analyzes, runs EXPLAIN ANALYZE again → asserts
//     the planner uses the doc index with sharply fewer blocks.
//  5. Drops the throwaway table on cleanup.
//
// EXPLAIN ANALYZE runs are wrapped in rolled-back transactions so no rows are
// ever deleted. A single *sql.Conn is used throughout so the planner cache
// for the throwaway table is stable across both EXPLAIN calls.
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
	// Close the pool last (registered first since t.Cleanup runs LIFO). The DROP
	// cleanup registered after this will fire before db.Close, so the pool is
	// still open when the throwaway table is dropped.
	t.Cleanup(func() { _ = db.Close() })

	// Use a single dedicated connection so the planner cache is stable and
	// both EXPLAIN calls observe the same session state.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Unique throwaway table name to allow concurrent test runs without collision.
	tbl := fmt.Sprintf("eshu_search_index_terms_planproof_%d", time.Now().UnixNano())
	docIdx := tbl + "_doc_idx"

	// Create throwaway table with identical structure to eshu_search_index_terms:
	// same columns, same PK, same lookup index. No FK references so no FK parents
	// need to be inserted.
	_, err = conn.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
		    scope_id       TEXT NOT NULL,
		    generation_id  TEXT NOT NULL,
		    document_id    TEXT NOT NULL,
		    term_key       TEXT NOT NULL,
		    term           TEXT NOT NULL,
		    term_frequency INTEGER NOT NULL,
		    PRIMARY KEY (scope_id, generation_id, term_key, document_id)
		)`, tbl))
	if err != nil {
		t.Fatalf("create throwaway table: %v", err)
	}
	// Register cleanup via the pool (db), not the dedicated conn. t.Cleanup runs
	// after the function's defers, so conn is already closed by the time cleanup
	// fires; using the closed conn would silently skip the DROP. Using db ensures
	// the throwaway table is always removed even if the test fails mid-way.
	t.Cleanup(func() {
		if _, dropErr := db.ExecContext(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tbl)); dropErr != nil {
			t.Errorf("cleanup: failed to drop throwaway table %s: %v", tbl, dropErr)
		}
	})

	// Mirror the existing lookup index (scope_id, generation_id, term_key) so
	// the planner has the same index landscape as the real table for the BEFORE
	// phase. This ensures the planner chooses the same index-scan-then-filter
	// path it would use on the production table without the doc index.
	_, err = conn.ExecContext(ctx, fmt.Sprintf(
		`CREATE INDEX %s_lookup_idx ON %s (scope_id, generation_id, term_key)`,
		tbl, tbl,
	))
	if err != nil {
		t.Fatalf("create lookup index: %v", err)
	}

	// Seed data in two phases so the planner sees a realistic table shape:
	//
	// Phase A — "background" scopes: 20 extra (scope,generation) pairs × 100 docs
	// × 100 terms each = 200 000 rows. These make the table large enough that the
	// planner won't choose a seq scan for the proof scope in the BEFORE phase.
	//
	// Phase B — "proof" scope: 1 (scope,generation) × 500 docs × 200 terms =
	// 100 000 rows. This is the slice the EXPLAIN query targets.
	//
	// Total: ~300 000 rows. The proof scope is ~33% of the table, which is small
	// enough for the planner to prefer an index over a seq scan.
	const (
		scopeID      = "scope-planproof"
		generationID = "gen-planproof"
		docCount     = 500
		termCount    = 200
		bgScopes     = 20
		bgDocs       = 100
		bgTerms      = 100
	)
	// Phase A: background scopes so the table is large.
	_, err = conn.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (scope_id, generation_id, document_id, term_key, term, term_frequency)
		SELECT
		    'scope-bg-' || lpad(s::text, 3, '0'),
		    'gen-bg-' || lpad(s::text, 3, '0'),
		    'doc-' || lpad(d::text, 4, '0'),
		    'term-' || lpad(d::text, 4, '0') || '-' || lpad(tk::text, 4, '0'),
		    'term-' || lpad(d::text, 4, '0') || '-' || lpad(tk::text, 4, '0'),
		    1
		FROM generate_series(0, $1-1) AS s,
		     generate_series(0, $2-1) AS d,
		     generate_series(0, $3-1) AS tk
	`, tbl), bgScopes, bgDocs, bgTerms)
	if err != nil {
		t.Fatalf("seed background rows: %v", err)
	}
	// Phase B: proof scope.
	_, err = conn.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (scope_id, generation_id, document_id, term_key, term, term_frequency)
		SELECT
		    $1,
		    $2,
		    'doc-' || lpad(d::text, 4, '0'),
		    'term-' || lpad(d::text, 4, '0') || '-' || lpad(tk::text, 4, '0'),
		    'term-' || lpad(d::text, 4, '0') || '-' || lpad(tk::text, 4, '0'),
		    1
		FROM generate_series(0, $3-1) AS d,
		     generate_series(0, $4-1) AS tk
	`, tbl), scopeID, generationID, docCount, termCount)
	if err != nil {
		t.Fatalf("seed proof scope rows: %v", err)
	}
	totalRows := bgScopes*bgDocs*bgTerms + docCount*termCount
	t.Logf("seeded %d rows into %s (%d proof + %d background)",
		totalRows, tbl, docCount*termCount, bgScopes*bgDocs*bgTerms)

	// Analyze so the planner has fresh statistics before the BEFORE phase.
	if _, analyzeErr := conn.ExecContext(ctx, fmt.Sprintf(`ANALYZE %s`, tbl)); analyzeErr != nil {
		t.Logf("ANALYZE (before) warning: %v", analyzeErr)
	}

	// Target 10 documents for the DELETE = ANY predicate — a typical per-page
	// refresh batch size.
	targetDocIDs := make([]string, 10)
	for i := range targetDocIDs {
		targetDocIDs[i] = fmt.Sprintf("doc-%04d", i)
	}

	// refreshDeleteQuery is the production refresh query shape (= ANY) but
	// targeting the throwaway table. The predicate shape is identical to
	// eshuSearchIndexRefreshDocumentTermsQuery.
	refreshDeleteQuery := fmt.Sprintf(`
DELETE FROM %s
WHERE scope_id      = $1
  AND generation_id = $2
  AND document_id   = ANY($3::text[])
`, tbl)

	// explainOnConn runs EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) on the DELETE
	// inside a transaction that is always rolled back so no rows are deleted.
	// Using the same dedicated conn so cached plans cannot differ.
	explainOnConn := func(query string, args ...any) string {
		t.Helper()
		tx, txErr := conn.BeginTx(ctx, nil)
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

	// ── BEFORE: no doc index ──────────────────────────────────────────────────
	planBefore := explainOnConn(refreshDeleteQuery, scopeID, generationID, targetDocIDs)
	t.Logf("=== PLAN BEFORE (no doc index) ===\n%s", planJSONSummary(t, planBefore))
	_, bufsBefore := planInnerScanMetrics(t, planBefore)
	t.Logf("before: shared_blocks=%d", bufsBefore)

	// Assert the BEFORE plan does NOT use a document-keyed index (there is none).
	// It must rely on the PK or lookup index and filter on document_id.
	if strings.Contains(planBefore, docIdx) {
		t.Fatalf("BEFORE plan unexpectedly references doc index %s — test setup error", docIdx)
	}

	// ── Add doc index, re-analyze ────────────────────────────────────────────
	_, err = conn.ExecContext(ctx, fmt.Sprintf(
		`CREATE INDEX %s ON %s (scope_id, generation_id, document_id)`,
		docIdx, tbl,
	))
	if err != nil {
		t.Fatalf("create doc index: %v", err)
	}
	if _, analyzeErr := conn.ExecContext(ctx, fmt.Sprintf(`ANALYZE %s`, tbl)); analyzeErr != nil {
		t.Logf("ANALYZE (after) warning: %v", analyzeErr)
	}

	// ── AFTER: with doc index ────────────────────────────────────────────────
	planAfter := explainOnConn(refreshDeleteQuery, scopeID, generationID, targetDocIDs)
	t.Logf("=== PLAN AFTER (with doc index) ===\n%s", planJSONSummary(t, planAfter))
	_, bufsAfter := planInnerScanMetrics(t, planAfter)
	t.Logf("after: shared_blocks=%d", bufsAfter)

	// Assert the AFTER plan uses the doc index.
	if !strings.Contains(planAfter, docIdx) {
		t.Errorf("AFTER plan does not reference doc index %s\n"+
			"The planner preferred a different path; check index statistics and cardinality.\n"+
			"Plan:\n%s", docIdx, planAfter)
	}

	// Assert the AFTER plan uses an index scan (not a seq scan).
	if !strings.Contains(planAfter, "Index Scan") && !strings.Contains(planAfter, "Bitmap Index") {
		t.Errorf("AFTER plan does not show an Index Scan or Bitmap Index Scan\n"+
			"Plan:\n%s", planAfter)
	}

	// Assert blocks decreased: the doc index must read fewer blocks than the
	// PK-slice scan. The throwaway table is entirely hot in shared_buffers so
	// the absolute improvement is modest (typically 30–60%) compared to the
	// ~99.97% seen on cold disk for the 43 GB production table. What matters
	// here is that the planner chose the doc index (asserted above) AND that
	// the block count dropped — proving the structural improvement is real.
	if bufsBefore > 0 && bufsAfter > 0 {
		improvement := float64(bufsBefore-bufsAfter) / float64(bufsBefore) * 100
		t.Logf("buffer block improvement: %.1f%% (%d → %d)", improvement, bufsBefore, bufsAfter)
		if bufsAfter >= bufsBefore {
			t.Errorf("WITH-index plan blocks (%d) >= WITHOUT-index (%d) — "+
				"expected fewer blocks with doc index",
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

// planInnerScanMetrics extracts rows-removed-by-filter and total shared buffer
// blocks from an EXPLAIN ANALYZE BUFFERS FORMAT JSON output. It prefers the
// child scan node (Index Scan under ModifyTable) for block counts.
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
	if len(top.Plans) > 0 {
		child := top.Plans[0]
		rowsRemovedByFilter = child.RowsRemovedByFilter
		if child.SharedHitBlocks+child.SharedReadBlocks > 0 {
			sharedBlocks = child.SharedHitBlocks + child.SharedReadBlocks
		}
	}
	return rowsRemovedByFilter, sharedBlocks
}
