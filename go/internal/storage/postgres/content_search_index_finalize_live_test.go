// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestContentSearchIndexColdLifecycleLive(t *testing.T) {
	ctx, db := openContentSearchIndexLiveDB(t)

	exec := SQLDB{DB: db}
	if err := ApplyBootstrapWithoutContentSearchIndexes(ctx, exec); err != nil {
		t.Fatalf("ApplyBootstrapWithoutContentSearchIndexes() error = %v", err)
	}
	assertContentSearchIndexState(t, db, "not_built")
	if _, err := db.ExecContext(ctx, "SELECT eshu_require_content_substring_indexes_ready()"); err == nil {
		t.Fatal("unready content substring guard error = nil, want fail-closed")
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO content_files (
  repo_id, relative_path, content, content_hash, line_count, indexed_at
) VALUES ('repo-proof', 'src/proof.go', repeat('x', 5000) || 'FarTailNeedle', 'hash-file', 1, clock_timestamp());
INSERT INTO content_entities (
  entity_id, repo_id, relative_path, entity_type, entity_name,
  start_line, end_line, source_cache, indexed_at
) VALUES ('entity-proof', 'repo-proof', 'src/proof.go', 'Function', 'Proof',
          1, 1, repeat('y', 5000) || 'FarTailNeedle', clock_timestamp());
`); err != nil {
		t.Fatalf("seed exact-search proof rows: %v", err)
	}
	if err := EnsureContentSearchIndexes(ctx, exec); err != nil {
		t.Fatalf("EnsureContentSearchIndexes() error = %v", err)
	}
	assertContentSearchIndexState(t, db, "ready")

	var fileCount, entityCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM content_files WHERE eshu_require_content_substring_indexes_ready() AND content ILIKE '%fartailneedle%'").Scan(&fileCount); err != nil {
		t.Fatalf("guarded file substring read: %v", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM content_entities WHERE eshu_require_content_substring_indexes_ready() AND source_cache ILIKE '%fartailneedle%'").Scan(&entityCount); err != nil {
		t.Fatalf("guarded entity substring read: %v", err)
	}
	if fileCount != 1 || entityCount != 1 {
		t.Fatalf("guarded exact read counts = files:%d entities:%d, want 1/1", fileCount, entityCount)
	}

	var firstCompletedAt time.Time
	if err := db.QueryRowContext(ctx, "SELECT build_completed_at FROM content_substring_index_state WHERE singleton").Scan(&firstCompletedAt); err != nil {
		t.Fatalf("read first completion timestamp: %v", err)
	}
	if err := EnsureContentSearchIndexes(ctx, exec); err != nil {
		t.Fatalf("idempotent EnsureContentSearchIndexes() error = %v", err)
	}
	var secondCompletedAt time.Time
	if err := db.QueryRowContext(ctx, "SELECT build_completed_at FROM content_substring_index_state WHERE singleton").Scan(&secondCompletedAt); err != nil {
		t.Fatalf("read second completion timestamp: %v", err)
	}
	if !secondCompletedAt.Equal(firstCompletedAt) {
		t.Fatalf("ready rerun rebuilt indexes: first=%s second=%s", firstCompletedAt, secondCompletedAt)
	}
}

func TestContentSearchIndexRestartAndFailedBuildLive(t *testing.T) {
	ctx, db := openContentSearchIndexLiveDB(t)
	exec := SQLDB{DB: db}

	if err := ApplyBootstrapWithoutContentSearchIndexes(ctx, exec); err != nil {
		t.Fatalf("ApplyBootstrapWithoutContentSearchIndexes() error = %v", err)
	}
	claimed, err := claimContentSearchIndexBuild(ctx, exec)
	if err != nil {
		t.Fatalf("claimContentSearchIndexBuild() error = %v", err)
	}
	if !claimed {
		t.Fatal("claimContentSearchIndexBuild() claimed = false, want true")
	}
	assertContentSearchIndexState(t, db, "building")
	if err := EnsureContentSearchIndexes(ctx, exec); err != nil {
		t.Fatalf("restart EnsureContentSearchIndexes() error = %v", err)
	}
	assertContentSearchIndexState(t, db, "ready")

	if _, err := db.ExecContext(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatalf("reset disposable proof schema for failure case: %v", err)
	}
	if err := ApplyBootstrapWithoutContentSearchIndexes(ctx, exec); err != nil {
		t.Fatalf("reapply deferred bootstrap: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
CREATE INDEX content_files_content_trgm_idx ON content_files (content);
CREATE INDEX content_entities_source_trgm_idx ON content_entities (source_cache);
`); err != nil {
		t.Fatalf("seed wrong same-name indexes: %v", err)
	}
	if err := EnsureContentSearchIndexes(ctx, exec); err == nil {
		t.Fatal("EnsureContentSearchIndexes() error = nil, want exact-index validation failure")
	}
	assertContentSearchIndexState(t, db, "failed")

	if _, err := db.ExecContext(ctx, `
DROP INDEX content_files_content_trgm_idx;
DROP INDEX content_entities_source_trgm_idx;
CREATE INDEX content_files_content_trgm_idx
  ON content_files USING gin (content gin_trgm_ops) WHERE content <> '';
CREATE INDEX content_entities_source_trgm_idx
  ON content_entities USING gin (source_cache gin_trgm_ops) WHERE source_cache <> '';
`); err != nil {
		t.Fatalf("seed partial same-name indexes: %v", err)
	}
	if err := EnsureContentSearchIndexes(ctx, exec); err == nil {
		t.Fatal("EnsureContentSearchIndexes() error = nil, want partial-index validation failure")
	}
	assertContentSearchIndexState(t, db, "failed")

	if _, err := db.ExecContext(ctx, `
DROP INDEX content_files_content_trgm_idx;
DROP INDEX content_entities_source_trgm_idx;
`); err != nil {
		t.Fatalf("drop invalid indexes before retry: %v", err)
	}
	if err := EnsureContentSearchIndexes(ctx, exec); err != nil {
		t.Fatalf("EnsureContentSearchIndexes() recovery error = %v", err)
	}
	assertContentSearchIndexState(t, db, "ready")
}

func TestContentSearchIndexConcurrentFinalizersLive(t *testing.T) {
	ctx, db := openContentSearchIndexLiveDB(t)
	exec := SQLDB{DB: db}
	if err := ApplyBootstrapWithoutContentSearchIndexes(ctx, exec); err != nil {
		t.Fatalf("ApplyBootstrapWithoutContentSearchIndexes() error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO content_files (
  repo_id, relative_path, content, content_hash, line_count, indexed_at
)
SELECT 'repo-proof', 'src/proof-' || n || '.go',
       repeat(md5(n::text), 64), md5(n::text), 1, clock_timestamp()
FROM generate_series(1, 20000) AS n;
INSERT INTO content_entities (
  entity_id, repo_id, relative_path, entity_type, entity_name,
  start_line, end_line, source_cache, indexed_at
)
SELECT 'entity-' || n, 'repo-proof', 'src/proof-' || n || '.go',
       'Function', 'Proof' || n, 1, 1, repeat(md5(n::text), 64), clock_timestamp()
FROM generate_series(1, 20000) AS n;
`); err != nil {
		t.Fatalf("seed concurrent finalizer rows: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			errs <- EnsureContentSearchIndexes(ctx, exec)
		}()
	}
	ready.Wait()
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent EnsureContentSearchIndexes() error = %v", err)
		}
	}

	assertContentSearchIndexState(t, db, "ready")
	var valid bool
	if err := db.QueryRowContext(ctx, "SELECT eshu_content_substring_indexes_valid()").Scan(&valid); err != nil {
		t.Fatalf("validate concurrent finalizer indexes: %v", err)
	}
	if !valid {
		t.Fatal("eshu_content_substring_indexes_valid() = false, want true")
	}
}

func openContentSearchIndexLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("PingContext() error = %v", err)
	}
	if _, err := db.ExecContext(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatalf("reset disposable proof schema: %v", err)
	}
	return ctx, db
}

func assertContentSearchIndexState(t *testing.T, db *sql.DB, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow("SELECT state FROM content_substring_index_state WHERE singleton").Scan(&got); err != nil {
		t.Fatalf("read content substring index state: %v", err)
	}
	if got != want {
		t.Fatalf("content substring index state = %q, want %q", got, want)
	}
}
