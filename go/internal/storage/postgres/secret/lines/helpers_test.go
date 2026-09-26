// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// openDatabase returns a disposable database with the full bootstrap schema
// (migration 131 included) and the connection config of that database, so a test can open a
// second pool whose sessions carry DeferredSessionSQL. Set
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN (an administrative postgres-database DSN)
// and ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 to run these proofs.
func openDatabase(t *testing.T) (context.Context, *sql.DB, *pgx.ConnConfig) {
	t.Helper()
	adminDSN := os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN")
	ctx, db := postgresproof.OpenDisposableDatabase(t, adminDSN,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"), 10*time.Minute)
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	var name string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	config, err := pgx.ParseConfig(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	config.Database = name
	return ctx, db, config
}

// openDeferredPool opens a pool the way bootstrap-index does: every connection
// carries the writer marker and DeferredSessionSQL.
func openDeferredPool(t *testing.T, config *pgx.ConnConfig) *sql.DB {
	t.Helper()
	pool := stdlib.OpenDB(*config, inventory.WriterConnectOption(DeferredSessionSQL))
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

// writeRecords writes one repository's records through the real ContentWriter.
func writeRecords(ctx context.Context, t *testing.T, pool *sql.DB, repoID string, records []content.Record) {
	t.Helper()
	writer := postgres.NewContentWriter(postgres.SQLDB{DB: pool})
	if _, err := writer.Write(ctx, content.Materialization{
		RepoID: repoID, ScopeID: "scope-" + repoID, GenerationID: "gen-1", SourceSystem: "git", Records: records,
	}); err != nil {
		t.Fatalf("write %s: %v", repoID, err)
	}
}

func secretRecord(path, body, language string) content.Record {
	return content.Record{Path: path, Body: body, Metadata: map[string]string{"language": language}}
}

// corpus returns records with findings, clean files, and a suppressed path.
func corpus(prefix string, files int) []content.Record {
	records := make([]content.Record, 0, files)
	for i := 0; i < files; i++ {
		path := fmt.Sprintf("%s/f_%04d.go", prefix, i)
		switch i % 4 {
		case 0:
			records = append(records, secretRecord(path, fmt.Sprintf("package p\npassword = \"hunter%08d\"\n", i), "go"))
		case 1:
			records = append(records, secretRecord(path, "package clean\n", "go"))
		case 2:
			records = append(records, secretRecord(path, fmt.Sprintf("token: abcdef%08d\nAKIA%016d\n", i, i), "yaml"))
		default:
			records = append(records, secretRecord(prefix+"/testdata/"+path, "secret = \"fixture12345\"\n", "go"))
		}
	}
	return records
}

func sideRowCount(ctx context.Context, t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM content_file_secret_lines`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func stateRow(ctx context.Context, t *testing.T, db *sql.DB) (state string, epoch int64) {
	t.Helper()
	if err := db.QueryRowContext(ctx, `SELECT state, epoch FROM content_file_secret_lines_state WHERE singleton`).Scan(&state, &epoch); err != nil {
		t.Fatal(err)
	}
	return state, epoch
}

// requireParity compares the side table with the derivation recomputed from
// content_files, EXCEPT ALL in both directions.
func requireParity(ctx context.Context, t *testing.T, db *sql.DB, label string) {
	t.Helper()
	var missing, extra int64
	if err := db.QueryRowContext(ctx, `
WITH derived AS (
  SELECT f.repo_id, f.relative_path, d.line_number, coalesce(f.language, '') AS language, d.finding_kind, d.line_text
  FROM content_files f CROSS JOIN LATERAL eshu_secret_line_findings(f.content) d
), side AS (
  SELECT repo_id, relative_path, line_number, language, finding_kind, line_text FROM content_file_secret_lines
)
SELECT (SELECT count(*) FROM (SELECT * FROM derived EXCEPT ALL SELECT * FROM side) a),
       (SELECT count(*) FROM (SELECT * FROM side EXCEPT ALL SELECT * FROM derived) b)`).Scan(&missing, &extra); err != nil {
		t.Fatalf("%s: parity query: %v", label, err)
	}
	if missing != 0 || extra != 0 {
		t.Fatalf("%s: side table vs derivation EXCEPT ALL = missing %d, extra %d, want 0/0", label, missing, extra)
	}
}
