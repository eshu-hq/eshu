// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestContentFilesRelativePathIndexMigrationRecoversInvalidConcurrentBuildLive(t *testing.T) {
	ctx, database := openContentSearchIndexLiveDB(t)
	exec := SQLDB{DB: database}
	migration, preMigration := contentFilesRelativePathIndexMigration(t)
	if err := applyBootstrapDefinitionsWith(ctx, exec, preMigration, slog.Default(), schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("apply pre-126 schema: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
VALUES
  ('repo-a', 'shared/path.go', 'a', 'hash-a', 1, clock_timestamp()),
	  ('repo-b', 'shared/path.go', 'b', 'hash-b', 1, clock_timestamp())
`); err != nil {
		t.Fatalf("seed duplicate relative paths: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
CREATE UNIQUE INDEX CONCURRENTLY content_files_relative_path_trgm_idx
  ON content_files (relative_path)
`); err == nil {
		t.Fatal("seed invalid same-name concurrent index error = nil, want duplicate-key failure")
	}
	if valid := contentFilesRelativePathIndexValid(t, ctx, database); valid {
		t.Fatal("failed same-name concurrent index is valid, want invalid")
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	apply := func() error {
		return applyBootstrapDefinitionsWith(ctx, exec, []Definition{migration}, slog.Default(), schemaBootstrapCoordination{})
	}
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			errs <- apply()
		}()
	}
	ready.Wait()
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent migration recovery: %v", err)
		}
	}
	assertContentFilesRelativePathIndexDefinition(t, ctx, database)

	if err := apply(); err != nil {
		t.Fatalf("reapply recovered migration: %v", err)
	}
	var receipts int
	if err := database.QueryRowContext(ctx, `
SELECT count(*)
FROM eshu_schema_migrations
WHERE path = $1 AND variant = 'full'`, migration.Path).Scan(&receipts); err != nil {
		t.Fatalf("count migration receipts: %v", err)
	}
	if receipts != 1 {
		t.Fatalf("migration receipts = %d, want 1", receipts)
	}
}

func contentFilesRelativePathIndexMigration(t *testing.T) (Definition, []Definition) {
	t.Helper()
	definitions := BootstrapDefinitions()
	preMigration := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		if definition.Name == "content_files_relative_path_trgm_index" {
			return definition, preMigration
		}
		preMigration = append(preMigration, definition)
	}
	t.Fatal("content_files_relative_path_trgm_index definition not found")
	return Definition{}, nil
}

func assertContentFilesRelativePathIndexDefinition(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	if !contentFilesRelativePathIndexValid(t, ctx, database) {
		t.Fatal("content_files relative-path index is invalid, want valid")
	}
	var definition string
	if err := database.QueryRowContext(
		ctx,
		"SELECT pg_get_indexdef('public.content_files_relative_path_trgm_idx'::regclass)",
	).Scan(&definition); err != nil {
		t.Fatalf("read relative-path index definition: %v", err)
	}
	for _, fragment := range []string{"USING gin", "(relative_path gin_trgm_ops)"} {
		if !strings.Contains(definition, fragment) {
			t.Fatalf("relative-path index definition = %q, missing %q", definition, fragment)
		}
	}
}

func contentFilesRelativePathIndexValid(t *testing.T, ctx context.Context, database *sql.DB) bool {
	t.Helper()
	var valid bool
	if err := database.QueryRowContext(ctx, `
SELECT i.indisvalid AND i.indisready
FROM pg_index AS i
JOIN pg_class AS c ON c.oid = i.indexrelid
WHERE c.relname = 'content_files_relative_path_trgm_idx'`).Scan(&valid); err != nil {
		t.Fatalf("read relative-path index validity: %v", err)
	}
	return valid
}
