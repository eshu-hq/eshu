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

// TestEshuSearchIndexTermsGenerationClearPlanLive proves the current
// generation-scoped term clear can use the primary-key prefix without a
// document-keyed secondary index.
//
// Environment gates (both required to run):
//
//	ESHU_SEARCH_INDEX_PLAN_LIVE=1
//	ESHU_POSTGRES_DSN=postgresql://eshu:<pw>@localhost:15432/eshu?sslmode=disable
//
// HERMETIC DESIGN — the test never touches the shared eshu_search_index_terms
// table or its indexes. It creates a throwaway table with the production
// primary key only, seeds a selective proof generation plus background scopes,
// and runs EXPLAIN ANALYZE on the generation clear inside a rolled-back
// transaction.
func TestEshuSearchIndexTermsGenerationClearPlanLive(t *testing.T) {
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
	tbl := fmt.Sprintf("eshu_search_index_terms_clearproof_%d", time.Now().UnixNano())
	pkeyName := tbl + "_pkey"

	// Create throwaway table with identical structure to eshu_search_index_terms:
	// same columns and same PK. No FK references so no FK parents need to be
	// inserted.
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

	const (
		scopeID      = "scope-planproof"
		generationID = "gen-planproof"
		docCount     = 100
		termCount    = 100
		bgScopes     = 50
		bgDocs       = 100
		bgTerms      = 100
	)
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

	clearGenerationQuery := fmt.Sprintf(`
DELETE FROM %s
WHERE scope_id      = $1
  AND generation_id = $2
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

	plan := explainOnConn(clearGenerationQuery, scopeID, generationID)
	t.Logf("=== GENERATION CLEAR PLAN (primary-key prefix only) ===\n%s", planJSONSummary(t, plan))
	if !strings.Contains(plan, pkeyName) {
		t.Fatalf("generation clear plan does not reference primary-key index %s:\n%s", pkeyName, plan)
	}
	if strings.Contains(plan, "doc_idx") {
		t.Fatalf("generation clear plan unexpectedly references a document index:\n%s", plan)
	}
	if strings.Contains(plan, "Seq Scan") {
		t.Fatalf("generation clear plan used a sequential scan despite primary-key prefix:\n%s", plan)
	}
}

// TestEshuSearchIndexTermsBM25LookupUsesPrimaryKeyPrefixLive proves the BM25
// term-frequency query does not need the redundant
// eshu_search_index_terms_lookup_idx index. The production primary key starts
// with (scope_id, generation_id, term_key), which is the lookup prefix used by
// the active-generation BM25 joins. The throwaway table intentionally creates no
// standalone lookup index; the plan must still use the primary-key index.
func TestEshuSearchIndexTermsBM25LookupUsesPrimaryKeyPrefixLive(t *testing.T) {
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
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer func() { _ = conn.Close() }()

	tbl := fmt.Sprintf("eshu_search_index_terms_bm25proof_%d", time.Now().UnixNano())
	pkeyName := tbl + "_pkey"
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
	t.Cleanup(func() {
		if _, dropErr := db.ExecContext(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tbl)); dropErr != nil {
			t.Errorf("cleanup: failed to drop throwaway table %s: %v", tbl, dropErr)
		}
	})

	const (
		scopeID      = "scope-bm25proof"
		generationID = "gen-bm25proof"
		docCount     = 500
		termCount    = 200
	)
	_, err = conn.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (scope_id, generation_id, document_id, term_key, term, term_frequency)
		SELECT
		    $1,
		    $2,
		    'doc-' || lpad(d::text, 4, '0'),
		    'term-' || lpad(tk::text, 4, '0'),
		    'term-' || lpad(tk::text, 4, '0'),
		    1
		FROM generate_series(0, $3-1) AS d,
		     generate_series(0, $4-1) AS tk
	`, tbl), scopeID, generationID, docCount, termCount)
	if err != nil {
		t.Fatalf("seed rows: %v", err)
	}
	if _, analyzeErr := conn.ExecContext(ctx, fmt.Sprintf(`ANALYZE %s`, tbl)); analyzeErr != nil {
		t.Logf("ANALYZE warning: %v", analyzeErr)
	}

	query := fmt.Sprintf(`
WITH query_terms AS (
    SELECT term, term_key
    FROM unnest($3::text[], $4::text[]) AS q(term, term_key)
)
SELECT t.term_key, t.term, count(*)::float8 AS doc_frequency
FROM %s t
JOIN query_terms q ON q.term_key = t.term_key AND q.term = t.term
WHERE t.scope_id = $1
  AND t.generation_id = $2
GROUP BY t.term_key, t.term
`, tbl)
	var raw []byte
	err = conn.QueryRowContext(
		ctx,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query,
		scopeID,
		generationID,
		[]string{"term-0001", "term-0002", "term-0003"},
		[]string{"term-0001", "term-0002", "term-0003"},
	).Scan(&raw)
	if err != nil {
		t.Fatalf("EXPLAIN query error: %v", err)
	}
	plan := string(raw)
	t.Logf("=== BM25 LOOKUP PLAN (primary-key prefix only) ===\n%s", planJSONSummary(t, plan))
	if !strings.Contains(plan, pkeyName) {
		t.Fatalf("BM25 lookup plan does not reference primary-key index %s:\n%s", pkeyName, plan)
	}
	if strings.Contains(plan, "lookup_idx") {
		t.Fatalf("BM25 lookup plan unexpectedly references a lookup_idx:\n%s", plan)
	}
	if strings.Contains(plan, "Seq Scan") {
		t.Fatalf("BM25 lookup plan used a sequential scan despite primary-key prefix:\n%s", plan)
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
