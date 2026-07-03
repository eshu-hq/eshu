// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type searchIndexTermBenchRow struct {
	documentID string
	termKey    string
	term       string
	frequency  int
}

func TestEshuSearchIndexTermCopyFromLive(t *testing.T) {
	if os.Getenv("ESHU_SEARCH_INDEX_TERM_COPY_LIVE") != "1" {
		t.Skip("set ESHU_SEARCH_INDEX_TERM_COPY_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const (
		scopeID      = "scope-copy-proof"
		generationID = "gen-copy-proof"
		docCount     = 2000
		termCount    = 150
	)
	rows := makeSearchIndexTermBenchRows(docCount, termCount)
	insertTable := fmt.Sprintf("eshu_search_terms_insert_%d", time.Now().UnixNano())
	copyTable := fmt.Sprintf("eshu_search_terms_copy_%d", time.Now().UnixNano())
	createSearchIndexTermBenchTable(t, ctx, db, insertTable)
	createSearchIndexTermBenchTable(t, ctx, db, copyTable)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s", insertTable))
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s", copyTable))
	})

	insertElapsed := runSearchIndexTermInsertBench(t, ctx, db, insertTable, scopeID, generationID, rows)
	copyElapsed := runSearchIndexTermCopyBench(t, ctx, db, copyTable, scopeID, generationID, rows)
	insertCount := countSearchIndexTermBenchRows(t, ctx, db, insertTable)
	copyCount := countSearchIndexTermBenchRows(t, ctx, db, copyTable)
	if insertCount != int64(len(rows)) || copyCount != int64(len(rows)) {
		t.Fatalf("row counts insert=%d copy=%d want=%d", insertCount, copyCount, len(rows))
	}
	t.Logf("insert rows=%d elapsed=%s", len(rows), insertElapsed)
	t.Logf("copy_from rows=%d elapsed=%s", len(rows), copyElapsed)
	if copyElapsed >= insertElapsed {
		t.Fatalf("copy_from elapsed %s >= insert elapsed %s", copyElapsed, insertElapsed)
	}
}

func makeSearchIndexTermBenchRows(docCount int, termCount int) []searchIndexTermBenchRow {
	rows := make([]searchIndexTermBenchRow, 0, docCount*termCount)
	for d := 1; d <= docCount; d++ {
		for term := 1; term <= termCount; term++ {
			termKey := fmt.Sprintf("term-%04d", term)
			rows = append(rows, searchIndexTermBenchRow{
				documentID: fmt.Sprintf("doc-%06d", d),
				termKey:    termKey,
				term:       termKey,
				frequency:  1,
			})
		}
	}
	sort.Slice(rows, func(i int, j int) bool {
		if rows[i].termKey != rows[j].termKey {
			return rows[i].termKey < rows[j].termKey
		}
		return rows[i].documentID < rows[j].documentID
	})
	return rows
}

func createSearchIndexTermBenchTable(t *testing.T, ctx context.Context, db *sql.DB, table string) {
	t.Helper()
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
CREATE TABLE %s (
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    term_key TEXT NOT NULL,
    term TEXT NOT NULL,
    term_frequency INTEGER NOT NULL,
    PRIMARY KEY (scope_id, generation_id, term_key, document_id)
);
CREATE INDEX %s_doc_idx ON %s (scope_id, generation_id, document_id);
`, table, table, table))
	if err != nil {
		t.Fatalf("create %s: %v", table, err)
	}
}

func runSearchIndexTermInsertBench(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	table string,
	scopeID string,
	generationID string,
	rows []searchIndexTermBenchRow,
) time.Duration {
	t.Helper()
	documentIDs := make([]string, len(rows))
	terms := make([]string, len(rows))
	termKeys := make([]string, len(rows))
	frequencies := make([]int, len(rows))
	for i, row := range rows {
		documentIDs[i] = row.documentID
		terms[i] = row.term
		termKeys[i] = row.termKey
		frequencies[i] = row.frequency
	}
	started := time.Now()
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s (scope_id, generation_id, document_id, term_key, term, term_frequency)
SELECT $1, $2, document_id, term_key, term, term_frequency
FROM unnest($3::text[], $4::text[], $5::text[], $6::int[])
     AS t(document_id, term, term_key, term_frequency)
ORDER BY term_key, document_id
`, table), scopeID, generationID, documentIDs, terms, termKeys, frequencies)
	if err != nil {
		t.Fatalf("insert bench: %v", err)
	}
	return time.Since(started)
}

func runSearchIndexTermCopyBench(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	table string,
	scopeID string,
	generationID string,
	rows []searchIndexTermBenchRow,
) time.Duration {
	t.Helper()
	started := time.Now()
	documentIDs := make([]string, len(rows))
	terms := make([]string, len(rows))
	termKeys := make([]string, len(rows))
	frequencies := make([]int, len(rows))
	for i, row := range rows {
		documentIDs[i] = row.documentID
		terms[i] = row.term
		termKeys[i] = row.termKey
		frequencies[i] = row.frequency
	}
	copied, err := SQLDB{DB: db}.copySearchIndexTermsToTable(ctx, table, scopeID, generationID, documentIDs, terms, termKeys, frequencies)
	if err != nil {
		t.Fatalf("copy bench: %v", err)
	}
	if copied != int64(len(rows)) {
		t.Fatalf("copied %d rows, want %d", copied, len(rows))
	}
	return time.Since(started)
}

func countSearchIndexTermBenchRows(t *testing.T, ctx context.Context, db *sql.DB, table string) int64 {
	t.Helper()
	var count int64
	if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}
