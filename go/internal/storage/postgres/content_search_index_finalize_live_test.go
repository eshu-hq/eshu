// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
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

func TestContentEntityNameIndexBootstrapModesLive(t *testing.T) {
	ctx, db := openContentSearchIndexLiveDB(t)
	exec := SQLDB{DB: db}

	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("ApplyBootstrap() error = %v", err)
	}
	assertContentSearchIndexState(t, db, "ready")
	assertContentEntityNameIndex(t, db, true)
	if _, err := db.ExecContext(ctx, MigrationSQL("content_entity_name_trgm_index")); err != nil {
		t.Fatalf("retry migration 062: %v", err)
	}
	assertContentEntityNameGuardedRead(t, ctx, db)

	if _, err := db.ExecContext(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatalf("reset disposable proof schema for upgrade: %v", err)
	}
	definitions := BootstrapDefinitions()
	upgradeDefinitions := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		if definition.Name == "content_entity_name_trgm_index" {
			break
		}
		upgradeDefinitions = append(upgradeDefinitions, definition)
	}
	if err := ApplyDefinitions(ctx, exec, upgradeDefinitions); err != nil {
		t.Fatalf("apply pre-062 two-index schema: %v", err)
	}
	assertContentSearchIndexState(t, db, "ready")
	assertContentEntityNameIndex(t, db, false)
	if _, err := db.ExecContext(ctx, MigrationSQL("content_entity_name_trgm_index")); err != nil {
		t.Fatalf("apply upgrade migration 062: %v", err)
	}
	assertContentSearchIndexState(t, db, "ready")
	assertContentEntityNameIndex(t, db, true)
	assertContentEntityNameGuardedRead(t, ctx, db)

	if _, err := db.ExecContext(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatalf("reset disposable proof schema for deferred bootstrap: %v", err)
	}
	if err := ApplyBootstrapWithoutContentSearchIndexes(ctx, exec); err != nil {
		t.Fatalf("ApplyBootstrapWithoutContentSearchIndexes() error = %v", err)
	}
	assertContentSearchIndexState(t, db, "not_built")
	assertContentEntityNameIndex(t, db, false)
	if _, err := db.ExecContext(ctx, "SELECT eshu_require_content_substring_indexes_ready()"); err == nil {
		t.Fatal("deferred guarded read error = nil, want fail-closed")
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
CREATE INDEX content_entities_name_trgm_idx ON content_entities (entity_name);
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
DROP INDEX content_entities_name_trgm_idx;
CREATE INDEX content_files_content_trgm_idx
  ON content_files USING gin (content gin_trgm_ops) WHERE content <> '';
CREATE INDEX content_entities_source_trgm_idx
  ON content_entities USING gin (source_cache gin_trgm_ops) WHERE source_cache <> '';
CREATE INDEX content_entities_name_trgm_idx
  ON content_entities USING gin (entity_name gin_trgm_ops) WHERE entity_name <> '';
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
DROP INDEX content_entities_name_trgm_idx;
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

func TestContentEntityNameMigrationConcurrencyAndInterruptionLive(t *testing.T) {
	ctx, db := openContentSearchIndexLiveDB(t)
	exec := SQLDB{DB: db}
	definitions := BootstrapDefinitions()
	preMigration := make([]Definition, 0, len(definitions))
	var migration Definition
	for _, definition := range definitions {
		if definition.Name == "content_entity_name_trgm_index" {
			migration = definition
			break
		}
		preMigration = append(preMigration, definition)
	}
	if migration.Name == "" {
		t.Fatal("content_entity_name_trgm_index definition not found")
	}
	if err := ApplyDefinitions(ctx, exec, preMigration); err != nil {
		t.Fatalf("apply populated pre-062 schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO content_entities (
  entity_id, repo_id, relative_path, entity_type, entity_name,
  start_line, end_line, source_cache, indexed_at
)
SELECT 'migration-proof-' || n, 'repo-proof', 'src/proof-' || n || '.go',
       'Function', 'Proof' || n, 1, 1, '', clock_timestamp()
FROM generate_series(1, 20000) AS n;
ANALYZE content_entities;
`); err != nil {
		t.Fatalf("seed populated migration table: %v", err)
	}

	blocker, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin migration blocker: %v", err)
	}
	if _, err := blocker.ExecContext(ctx, "LOCK TABLE content_entities IN ACCESS EXCLUSIVE MODE"); err != nil {
		_ = blocker.Rollback()
		t.Fatalf("lock content_entities: %v", err)
	}
	if err := ApplyDefinitionsWithLockTimeout(
		ctx,
		exec,
		[]Definition{migration},
		100*time.Millisecond,
	); err == nil || !strings.Contains(strings.ToLower(err.Error()), "lock timeout") {
		_ = blocker.Rollback()
		t.Fatalf("blocked migration error = %v, want bounded lock timeout", err)
	}
	interruptedCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := ApplyDefinitionsWithLockTimeout(
		interruptedCtx,
		exec,
		[]Definition{migration},
		5*time.Second,
	); err == nil || (!errors.Is(err, context.DeadlineExceeded) &&
		!strings.Contains(strings.ToLower(err.Error()), "context deadline")) {
		_ = blocker.Rollback()
		t.Fatalf("interrupted migration error = %v, want context deadline", err)
	}
	if err := blocker.Rollback(); err != nil {
		t.Fatalf("release migration blocker: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			errs <- ApplyDefinitionsWithLockTimeout(ctx, exec, []Definition{migration}, 5*time.Second)
		}()
	}
	ready.Wait()
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent migration retry error = %v", err)
		}
	}
	assertContentEntityNameIndexDefinition(t, ctx, db)

	ready.Add(2)
	start = make(chan struct{})
	for range 2 {
		go func() {
			ready.Done()
			<-start
			errs <- ApplyBootstrap(ctx, exec)
		}()
	}
	ready.Wait()
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent ApplyBootstrap() error = %v", err)
		}
	}
	assertContentEntityNameIndexDefinition(t, ctx, db)
}

func assertContentEntityNameIndexDefinition(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var valid bool
	if err := db.QueryRowContext(ctx, "SELECT eshu_content_substring_indexes_valid()").Scan(&valid); err != nil {
		t.Fatalf("validate exact content search indexes: %v", err)
	}
	if !valid {
		t.Fatal("eshu_content_substring_indexes_valid() = false, want exact three-index contract")
	}
	var definition string
	if err := db.QueryRowContext(
		ctx,
		"SELECT pg_get_indexdef('public.content_entities_name_trgm_idx'::regclass)",
	).Scan(&definition); err != nil {
		t.Fatalf("read content entity name index definition: %v", err)
	}
	for _, fragment := range []string{"USING gin", "(entity_name gin_trgm_ops)"} {
		if !strings.Contains(definition, fragment) {
			t.Fatalf("content entity name index definition = %q, missing %q", definition, fragment)
		}
	}
}

func openContentSearchIndexLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	return postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		2*time.Minute,
	)
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

func assertContentEntityNameIndex(t *testing.T, db *sql.DB, want bool) {
	t.Helper()
	var got bool
	if err := db.QueryRow("SELECT to_regclass('public.content_entities_name_trgm_idx') IS NOT NULL").Scan(&got); err != nil {
		t.Fatalf("read content entity name index presence: %v", err)
	}
	if got != want {
		t.Fatalf("content entity name index present = %t, want %t", got, want)
	}
}

func assertContentEntityNameGuardedRead(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO content_entities (
  entity_id, repo_id, relative_path, entity_type, entity_name,
  start_line, end_line, source_cache, indexed_at
) VALUES ('entity-name-guard-proof', 'repo-proof', 'proof.go', 'Function',
          'ExactProof', 1, 1, '', clock_timestamp())
ON CONFLICT (entity_id) DO NOTHING;
`); err != nil {
		t.Fatalf("seed guarded entity-name read: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM content_entities
WHERE eshu_require_content_substring_indexes_ready()
  AND entity_name = 'ExactProof'
`).Scan(&count); err != nil {
		t.Fatalf("guarded entity-name read: %v", err)
	}
	if count != 1 {
		t.Fatalf("guarded entity-name count = %d, want 1", count)
	}
}
