// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	contentFilesLanguageTable = "content_files"
	contentFilesLanguageIndex = "content_files_language_repo_idx"
	// contentFilesLanguageFirstMigration is the first #7728 migration. Every
	// definition sorting before it is what an install of the prior release
	// has already applied and recorded.
	contentFilesLanguageFirstMigration = "go/internal/storage/postgres/migrations/171_drop_content_files_language_repo_idx.sql"
)

// contentFilesLanguageIndexSnapshot is what an operator would check to tell
// a no-op replay from a rebuild: the definition, the index OID, and the
// relfilenode (a REINDEX or drop-and-create changes the last two).
type contentFilesLanguageIndexSnapshot struct {
	Definition  string
	IndexOID    int64
	RelFileNode int64
	IdxScan     int64
}

// TestContentFilesLanguageCoveringIndexReplayConvergesLive proves the #7728
// migrations (171-173) against real Postgres through the production
// bootstrap:
//
//   - fresh database: ApplyBootstrap leaves the language index on
//     (language, repo_id) INCLUDE (indexed_at);
//   - upgrading database: an install holding the prior release's schema and
//     seeded content rows converges, and the by-language COUNT(*)/MAX rollup
//     reads exact counts and maxima afterwards;
//   - neither a second tracked ApplyBootstrap nor an untracked full-tree
//     replay (a database without eshu_schema_migrations receipts) drops,
//     rebuilds, or fails on the index.
//
// It runs on demand against a scratch Postgres:
//
//	ESHU_POSTGRES_TEST_DSN=postgresql://user:pass@localhost:<port>/eshu \
//	go test ./internal/storage/postgres \
//	  -run TestContentFilesLanguageCoveringIndexReplayConvergesLive -count=1 -v
func TestContentFilesLanguageCoveringIndexReplayConvergesLive(t *testing.T) {
	// Bridge the live-postgres-readiness runner's family DSN onto the shared
	// helper's generic variable. Local runs keep using ESHU_POSTGRES_TEST_DSN
	// directly.
	if dsn := strings.TrimSpace(os.Getenv("ESHU_CONTENT_FILES_LANGUAGE_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_CONTENT_FILES_LANGUAGE_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_CONTENT_FILES_LANGUAGE_PROOF_DSN is set without ESHU_CONTENT_FILES_LANGUAGE_PROOF_DISPOSABLE=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()

	t.Run("fresh", func(t *testing.T) {
		db, schema := openContentFilesLanguageSchemaLive(ctx, t, "eshu_7728_covering_fresh")
		if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
			t.Fatalf("first ApplyBootstrap on a fresh schema: %v", err)
		}
		first := contentFilesLanguageIndexLive(ctx, t, db, schema)
		assertContentFilesLanguageIndexCovering(t, first)
		assertContentFilesLanguageReplayIsNoOp(ctx, t, db, schema, first)
	})

	t.Run("upgrade", func(t *testing.T) {
		db, schema := openContentFilesLanguageSchemaLive(ctx, t, "eshu_7728_covering_upgrade")
		prior, rest := splitContentFilesLanguageDefinitions(t)
		if len(rest) < 3 {
			t.Fatalf("found %d definition(s) at or after %s, want at least the three #7728 migrations", len(rest), contentFilesLanguageFirstMigration)
		}
		if err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: db}, prior, slog.Default(), schemaBootstrapCoordination{}); err != nil {
			t.Fatalf("apply the prior release's definitions: %v", err)
		}
		seedContentFilesLanguageRowsLive(ctx, t, db)

		// The pre-#7728 definition is the defect: no indexed_at on the
		// index, so the rollup visits the heap once per file row.
		before := contentFilesLanguageIndexLive(ctx, t, db, schema)
		if strings.Contains(before.Definition, "indexed_at") {
			t.Fatalf("prior release's index already carries indexed_at: %q; the fixture does not stand where upgrading installs stand", before.Definition)
		}

		if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
			t.Fatalf("ApplyBootstrap upgrading a populated prior-release schema: %v", err)
		}
		converged := contentFilesLanguageIndexLive(ctx, t, db, schema)
		assertContentFilesLanguageIndexCovering(t, converged)
		assertContentFilesLanguageRollupExact(ctx, t, db)

		assertContentFilesLanguageReplayIsNoOp(ctx, t, db, schema, converged)
	})
}

// assertContentFilesLanguageReplayIsNoOp runs a second tracked
// ApplyBootstrap and then an untracked replay of the whole tree through the
// per-statement recorder, and requires the language index to be untouched by
// both.
func assertContentFilesLanguageReplayIsNoOp(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	schema string,
	before contentFilesLanguageIndexSnapshot,
) {
	t.Helper()
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("second (tracked) ApplyBootstrap: %v", err)
	}
	afterTracked := contentFilesLanguageIndexLive(ctx, t, db, schema)
	t.Logf("language index before replay: %+v", before)
	t.Logf("language index after tracked ApplyBootstrap: %+v", afterTracked)

	recorder := &migrationIndexReplayRecorder{db: SQLDB{DB: db}, t: t, schema: schema, table: contentFilesLanguageTable}
	if err := ApplyDefinitions(ctx, recorder, BootstrapDefinitions()); err != nil {
		t.Fatalf("untracked full-tree replay: %v", err)
	}
	for _, change := range recorder.changes {
		t.Errorf("the untracked replay changed an index on %s: %s", contentFilesLanguageTable, change)
	}
	afterUntracked := contentFilesLanguageIndexLive(ctx, t, db, schema)
	t.Logf("language index after untracked full-tree replay: %+v", afterUntracked)

	for label, after := range map[string]contentFilesLanguageIndexSnapshot{
		"tracked ApplyBootstrap":     afterTracked,
		"untracked full-tree replay": afterUntracked,
	} {
		if after.Definition != before.Definition || after.IndexOID != before.IndexOID || after.RelFileNode != before.RelFileNode {
			t.Errorf("%s changed %s: before %+v, after %+v", label, contentFilesLanguageIndex, before, after)
		}
	}
}

func assertContentFilesLanguageIndexCovering(t *testing.T, snapshot contentFilesLanguageIndexSnapshot) {
	t.Helper()
	if !strings.Contains(snapshot.Definition, "(language, repo_id)") || !strings.Contains(snapshot.Definition, "INCLUDE (indexed_at)") {
		t.Fatalf("%s definition = %q, want (language, repo_id) INCLUDE (indexed_at)", contentFilesLanguageIndex, snapshot.Definition)
	}
}

// assertContentFilesLanguageRollupExact runs the by-language rollup shape
// ReadRepositoriesByLanguage serves (per-repository COUNT(*) and
// MAX(indexed_at) for one language) and requires exact counts and maxima.
func assertContentFilesLanguageRollupExact(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT repo_id, count(*), max(indexed_at)
FROM content_files
WHERE language = 'javascript'
GROUP BY repo_id
ORDER BY repo_id`)
	if err != nil {
		t.Fatalf("by-language rollup: %v", err)
	}
	defer rows.Close()
	type rollup struct {
		count int
		max   string
	}
	got := map[string]rollup{}
	for rows.Next() {
		var repoID string
		var r rollup
		var max time.Time
		if err := rows.Scan(&repoID, &r.count, &max); err != nil {
			t.Fatalf("scan rollup row: %v", err)
		}
		r.max = max.UTC().Format(time.RFC3339)
		got[repoID] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read rollup rows: %v", err)
	}
	want := map[string]rollup{
		"repo-a": {count: 3, max: "2026-10-01T00:00:03Z"},
		"repo-b": {count: 1, max: "2026-10-02T00:00:00Z"},
	}
	if len(got) != len(want) {
		t.Fatalf("rollup repos = %v, want %v", got, want)
	}
	for repoID, w := range want {
		if g, ok := got[repoID]; !ok || g != w {
			t.Errorf("rollup for %s = %+v, want %+v", repoID, got[repoID], w)
		}
	}
}

// contentFilesLanguageIndexLive reads the language index's definition, OID,
// relfilenode, and pg_stat_user_indexes scan count. A missing or INVALID
// index fails the test.
func contentFilesLanguageIndexLive(ctx context.Context, t *testing.T, db *sql.DB, schema string) contentFilesLanguageIndexSnapshot {
	t.Helper()
	var snapshot contentFilesLanguageIndexSnapshot
	var valid bool
	if err := db.QueryRowContext(ctx, `
SELECT pg_get_indexdef(index_class.oid), index_class.oid::bigint, index_class.relfilenode::bigint,
       index_entry.indisvalid, COALESCE(stats.idx_scan, 0)
FROM pg_class AS index_class
JOIN pg_index AS index_entry ON index_entry.indexrelid = index_class.oid
JOIN pg_namespace AS namespace ON namespace.oid = index_class.relnamespace
LEFT JOIN pg_stat_user_indexes AS stats ON stats.indexrelid = index_class.oid
WHERE namespace.nspname = $1 AND index_class.relname = $2`,
		schema, contentFilesLanguageIndex,
	).Scan(&snapshot.Definition, &snapshot.IndexOID, &snapshot.RelFileNode, &valid, &snapshot.IdxScan); err != nil {
		t.Fatalf("read %s: %v", contentFilesLanguageIndex, err)
	}
	if !valid {
		t.Fatalf("%s is INVALID: %+v", contentFilesLanguageIndex, snapshot)
	}
	return snapshot
}

// splitContentFilesLanguageDefinitions returns the shipped definitions
// before the first #7728 migration and the ones from it on.
func splitContentFilesLanguageDefinitions(t *testing.T) (prior, rest []Definition) {
	t.Helper()
	for _, definition := range BootstrapDefinitions() {
		if definition.Path < contentFilesLanguageFirstMigration {
			prior = append(prior, definition)
			continue
		}
		rest = append(rest, definition)
	}
	return prior, rest
}

// seedContentFilesLanguageRowsLive writes the file rows a prior release left
// behind: three javascript rows in repo-a with distinct indexed_at maxima
// and one javascript row plus one python row in repo-b.
func seedContentFilesLanguageRowsLive(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO content_files
  (repo_id, relative_path, commit_sha, content, content_hash, line_count, language, indexed_at, artifact_type)
VALUES
  ('repo-a', 'src/a1.js', 'sha', 'const a = 1;', 'h1', 1, 'javascript', '2026-10-01T00:00:01Z', 'source'),
  ('repo-a', 'src/a2.js', 'sha', 'const a = 2;', 'h2', 1, 'javascript', '2026-10-01T00:00:02Z', 'source'),
  ('repo-a', 'src/a3.js', 'sha', 'const a = 3;', 'h3', 1, 'javascript', '2026-10-01T00:00:03Z', 'source'),
  ('repo-b', 'src/b1.js', 'sha', 'const b = 1;', 'h4', 1, 'javascript', '2026-10-02T00:00:00Z', 'source'),
  ('repo-b', 'src/b1.py', 'sha', 'b = 1', 'h5', 1, 'python', '2026-10-02T00:00:00Z', 'source');
ANALYZE content_files;
`); err != nil {
		t.Fatalf("seed prior-release content files: %v", err)
	}
}

// openContentFilesLanguageSchemaLive opens a connection pool whose
// search_path is a fresh isolated schema (then public), dropped when the
// test ends. The DSN is ESHU_POSTGRES_TEST_DSN or ESHU_POSTGRES_DSN; with
// neither set the test skips.
func openContentFilesLanguageSchemaLive(ctx context.Context, t *testing.T, prefix string) (*sql.DB, string) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN or ESHU_POSTGRES_DSN to run the #7728 replay proof")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if _, err := adminDB.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public"); err != nil {
		t.Fatalf("install pg_trgm in public: %v", err)
	}
	schema := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE"); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("open isolated schema: %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	return db, schema
}
