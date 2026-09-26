// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const secretLinesMigrationName = "content_file_secret_lines"

// openSecretLinesDatabase returns a disposable database with the full
// bootstrap schema applied. Set ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN (an
// administrative postgres-database DSN) and
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 to run these proofs.
func openSecretLinesDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableDatabase(t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		10*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	return ctx, db
}

// secretLinesParity compares the side table with the derivation recomputed
// from content_files, EXCEPT ALL in both directions; both must be zero.
func secretLinesParity(ctx context.Context, db *sql.DB) (missing, extra int64, err error) {
	err = db.QueryRowContext(ctx, `
WITH derived AS (
  SELECT f.repo_id, f.relative_path, d.line_number, coalesce(f.language, '') AS language, d.finding_kind, d.line_text
  FROM content_files f CROSS JOIN LATERAL eshu_secret_line_findings(f.content) d
), side AS (
  SELECT repo_id, relative_path, line_number, language, finding_kind, line_text FROM content_file_secret_lines
)
SELECT (SELECT count(*) FROM (SELECT * FROM derived EXCEPT ALL SELECT * FROM side) a),
       (SELECT count(*) FROM (SELECT * FROM side EXCEPT ALL SELECT * FROM derived) b)`).Scan(&missing, &extra)
	return missing, extra, err
}

func requireSecretLinesParity(t *testing.T, ctx context.Context, db *sql.DB, label string) {
	t.Helper()
	missing, extra, err := secretLinesParity(ctx, db)
	if err != nil {
		t.Fatalf("%s: parity query: %v", label, err)
	}
	if missing != 0 || extra != 0 {
		t.Fatalf("%s: side table vs derivation EXCEPT ALL = missing %d, extra %d, want 0/0", label, missing, extra)
	}
}

func secretLinesSideRows(t *testing.T, ctx context.Context, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT repo_id || '|' || relative_path || '|' || line_number || '|' || language || '|' || finding_kind || '|' || suppressed
FROM content_file_secret_lines ORDER BY repo_id, relative_path, line_number`)
	if err != nil {
		t.Fatalf("read side rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestContentFileSecretLinesMigrationBackfillsAndReappliesLive applies
// migration 131 to a content_files table that already holds rows (the upgrade
// path): the backfill must produce exactly the expected findings, and applying
// the file a second time must be a no-op.
func TestContentFileSecretLinesMigrationBackfillsAndReappliesLive(t *testing.T) {
	ctx, db := openSecretLinesDatabase(t)

	// Return the database to its pre-131 state, then load rows the migration
	// must backfill.
	for _, stmt := range []string{
		`DROP TRIGGER content_files_secret_lines_insert ON content_files`,
		`DROP TRIGGER content_files_secret_lines_update ON content_files`,
		`DROP TABLE content_file_secret_lines`,
		`DROP TABLE content_file_secret_lines_state`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	_, err := db.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at) VALUES
 ('repo-a', 'src/a.go', E'package a\npassword = "hunter2hunter2"\nAKIAABCDEFGHIJKLMNOP\n', 'h1', 3, 'go', now()),
 ('repo-a', 'src/b_test.go', 'token = "abcdefgh"', 'h2', 1, NULL, now()),
 ('repo-a', 'src/clean.go', E'package clean\nfunc Add() {}\n', 'h3', 2, 'go', now()),
 ('repo-b', 'src/stripe.go', 'stripe = "sk_live_ABCDEFGH12"', 'h4', 1, 'go', now()),
 ('repo-b', 'src/crlf.py', E'x = 1\r\ntoken=abcdefgh1\r\n', 'h5', 2, 'python', now())`)
	if err != nil {
		t.Fatalf("seed pre-131 content_files: %v", err)
	}

	want := []string{
		"repo-a|src/a.go|2|go|password_literal|false",
		"repo-a|src/a.go|3|go|aws_access_key|false",
		"repo-a|src/b_test.go|1||api_token|true",
		"repo-b|src/crlf.py|2|python|api_token|false",
	}
	for pass := 1; pass <= 2; pass++ {
		if _, err := db.ExecContext(ctx, MigrationSQL(secretLinesMigrationName)); err != nil {
			t.Fatalf("apply migration 131 (pass %d): %v", pass, err)
		}
		if got := secretLinesSideRows(t, ctx, db); !slices.Equal(got, want) {
			t.Fatalf("pass %d side rows = %v, want %v", pass, got, want)
		}
		requireSecretLinesParity(t, ctx, db, fmt.Sprintf("pass %d", pass))
		// The migration's backfill makes the table complete, so readiness starts
		// ready; a re-apply must not reset a bulk load's state.
		var state string
		var epoch int64
		if err := db.QueryRowContext(ctx, `SELECT state, epoch FROM content_file_secret_lines_state WHERE singleton`).Scan(&state, &epoch); err != nil {
			t.Fatalf("pass %d read state: %v", pass, err)
		}
		if pass == 1 {
			if state != "ready" {
				t.Fatalf("pass 1 state = %q, want ready", state)
			}
			if _, err := db.ExecContext(ctx, `UPDATE content_file_secret_lines_state SET state = 'not_built', epoch = 7`); err != nil {
				t.Fatal(err)
			}
		} else if state != "not_built" || epoch != 7 {
			t.Fatalf("re-applying the migration reset the bulk load state to %q epoch %d, want not_built epoch 7", state, epoch)
		}
	}

	var triggers int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_trigger
WHERE tgrelid = 'content_files'::regclass AND tgname IN
  ('content_files_secret_lines_insert', 'content_files_secret_lines_update')`).Scan(&triggers); err != nil {
		t.Fatal(err)
	}
	if triggers != 2 {
		t.Fatalf("content_files has %d secret-line triggers after re-apply, want exactly 2", triggers)
	}
}

// TestContentFileSecretLinesRetentionPruneCascadesLive runs the real
// pruneContentFilesForGenerationsQuery and proves the foreign key cascade
// removes exactly the pruned files' side rows and keeps every other file's.
func TestContentFileSecretLinesRetentionPruneCascadesLive(t *testing.T) {
	ctx, db := openSecretLinesDatabase(t)
	_, err := db.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at) VALUES
 ('repo-a', 'src/pruned1.go', E'password = "prunedsecret1"\n', 'p1', 1, 'go', now()),
 ('repo-a', 'src/pruned2.go', E'token = "prunedsecret22"\n', 'p2', 1, 'go', now()),
 ('repo-a', 'src/kept_retained.go', E'secret = "keptretained1"\n', 'k1', 1, 'go', now()),
 ('repo-a', 'src/kept_other.go', E'password = "keptother123"\n', 'k2', 1, 'go', now()),
 ('repo-b', 'src/pruned1.go', E'password = "sameprunedpath"\n', 'p3', 1, 'go', now())`)
	if err != nil {
		t.Fatalf("seed content_files: %v", err)
	}
	if got := len(secretLinesSideRows(t, ctx, db)); got != 5 {
		t.Fatalf("side rows after seed = %d, want 5", got)
	}
	for _, stmt := range []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
		 VALUES ('scope-ret', 'repository', 'git', 'scope-ret', 'git', 'scope-ret', now(), now(), 'active')`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
		 VALUES ('gen-old', 'scope-ret', 'snapshot', now() - interval '3 days', now() - interval '3 days', 'superseded', now() - interval '2 days'),
		        ('gen-new', 'scope-ret', 'snapshot', now(), now(), 'active', NULL)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed scope: %v", err)
		}
	}
	// Every path has a file fact in the old generation; kept_retained also has
	// one in the retained generation, so the prune must leave it alone.
	for i, fact := range []struct{ gen, repo, path string }{
		{"gen-old", "repo-a", "src/pruned1.go"},
		{"gen-old", "repo-a", "src/pruned2.go"},
		{"gen-old", "repo-b", "src/pruned1.go"},
		{"gen-old", "repo-a", "src/kept_retained.go"},
		{"gen-new", "repo-a", "src/kept_retained.go"},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, 'scope-ret', $2, 'file', $1, 'git', $1, now(), now(), jsonb_build_object('repo_id', $3::text, 'relative_path', $4::text))`,
			fmt.Sprintf("fact-%d", i), fact.gen, fact.repo, fact.path); err != nil {
			t.Fatalf("seed fact: %v", err)
		}
	}

	pruned, err := execRowsAffected(ctx, SQLDB{DB: db}, pruneContentFilesForGenerationsQuery, array.Of([]string{"gen-old"}))
	if err != nil {
		t.Fatalf("pruneContentFilesForGenerationsQuery: %v", err)
	}
	if pruned != 3 {
		t.Fatalf("prune removed %d content_files rows, want 3", pruned)
	}
	want := []string{
		"repo-a|src/kept_other.go|1|go|password_literal|false",
		"repo-a|src/kept_retained.go|1|go|secret_literal|false",
	}
	if got := secretLinesSideRows(t, ctx, db); !slices.Equal(got, want) {
		t.Fatalf("side rows after prune = %v, want %v", got, want)
	}
	requireSecretLinesParity(t, ctx, db, "after retention prune")
}
