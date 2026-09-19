// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func markXmin(t *testing.T, ctx context.Context, sqlDB *sql.DB, repo string) string {
	t.Helper()
	var xmin string
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT xmin::text FROM infra_resource_entity_dirty_repos WHERE repo_id = $1`, repo).Scan(&xmin); err != nil {
		t.Fatalf("read mark xmin: %v", err)
	}
	return xmin
}

// TestWriterFenceLiveStatementShapes runs the statement shapes an older
// writer issues through an unfenced connection and asserts the dirty set
// after each. Infra inserts, updates (including a type change into a label),
// and deletes mark; non-infra inserts and updates do not; a rolled-back
// unaware insert leaves no mark; the fenced connection marks nothing for any
// of them. A second unaware write to an already-marked repository takes the
// mark's lock without rewriting it (its xmin does not move).
func TestWriterFenceLiveStatementShapes(t *testing.T) {
	awareDB, ctx := liveDB(t)
	database := postgres.SQLDB{DB: awareDB}
	legacyDB := plainDB(t)
	clear := func(repo string) {
		t.Cleanup(func() {
			_, _ = awareDB.ExecContext(context.Background(),
				`DELETE FROM infra_resource_entity_dirty_repos WHERE repo_id = $1`, repo)
		})
	}
	exec := func(db *sql.DB, query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}

	// Fenced: every shape, no mark.
	fenced := uniqueRepo(t)
	clear(fenced)
	seedDerivedRepo(t, ctx, database, fenced, contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`})
	putContent(t, ctx, awareDB, fenced, contentRow{"f", "main.go", "Function", "main", `{}`})
	exec(awareDB, `UPDATE content_entities SET start_line = 3 WHERE repo_id = $1`, fenced)
	exec(awareDB, `UPDATE content_entities SET entity_type = 'K8sResource' WHERE entity_id = $1`, fenced+"/f")
	exec(awareDB, `DELETE FROM content_entities WHERE repo_id = $1 AND relative_path = 'a.tf'`, fenced)
	if isDirty(t, ctx, awareDB, fenced) {
		t.Fatal("a fenced session marked its repository")
	}

	// Unfenced non-infra insert and update: no mark.
	plain := uniqueRepo(t)
	clear(plain)
	putContent(t, ctx, legacyDB, plain, contentRow{"f", "main.go", "Function", "main", `{}`})
	exec(legacyDB, `UPDATE content_entities SET start_line = 3 WHERE repo_id = $1`, plain)
	if isDirty(t, ctx, awareDB, plain) {
		t.Fatal("an unaware non-infra insert or update marked the repository")
	}
	// Unfenced type change into an infra label: mark.
	exec(legacyDB, `UPDATE content_entities SET entity_type = 'HelmChart' WHERE entity_id = $1`, plain+"/f")
	if !isDirty(t, ctx, awareDB, plain) {
		t.Fatal("an unaware update of a row into an infra label did not mark the repository")
	}

	// Unfenced rolled-back infra insert: no mark.
	rolled := uniqueRepo(t)
	clear(rolled)
	tx, err := legacyDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, metadata, indexed_at)
VALUES ($1, $2, 'a.tf', 'TerraformResource', 'r', 1, 2, '', '{}'::jsonb, now())`, rolled+"/a", rolled); err != nil {
		t.Fatalf("unaware insert: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if isDirty(t, ctx, awareDB, rolled) {
		t.Fatal("a rolled-back unaware insert left a mark")
	}

	// Unfenced multi-row infra insert of one repository, the shape of an old
	// writer's batch upsert: one mark, and the statement succeeds.
	batch := uniqueRepo(t)
	clear(batch)
	exec(legacyDB, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, metadata, indexed_at)
SELECT $1 || '/' || i, $1, 'm' || i || '.tf', 'TerraformResource', 'r' || i, 1, 2, '', '{}'::jsonb, now()
FROM generate_series(1, 300) AS i
ON CONFLICT (entity_id) DO UPDATE SET entity_name = EXCLUDED.entity_name`, batch)
	exec(legacyDB, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, metadata, indexed_at)
SELECT $1 || '/' || i, $1, 'm' || i || '.tf', 'TerraformResource', 'x' || i, 1, 2, '', '{}'::jsonb, now()
FROM generate_series(1, 300) AS i
ON CONFLICT (entity_id) DO UPDATE SET entity_name = EXCLUDED.entity_name`, batch)
	if !isDirty(t, ctx, awareDB, batch) {
		t.Fatal("an unaware multi-row infra upsert did not mark the repository")
	}

	// Unfenced infra update and path delete: mark, and a repeat write keeps
	// the first mark's tuple.
	legacy := uniqueRepo(t)
	clear(legacy)
	seedDerivedRepo(t, ctx, database, legacy,
		contentRow{"a", "a.tf", "TerraformResource", "r.a", `{}`},
		contentRow{"b", "b.tf", "TerraformResource", "r.b", `{}`})
	exec(legacyDB, `UPDATE content_entities SET start_line = 7 WHERE entity_id = $1`, legacy+"/a")
	if !isDirty(t, ctx, awareDB, legacy) {
		t.Fatal("an unaware infra update did not mark the repository")
	}
	first := markXmin(t, ctx, awareDB, legacy)
	exec(legacyDB, `DELETE FROM content_entities WHERE repo_id = $1 AND relative_path = 'b.tf'`, legacy)
	putContent(t, ctx, legacyDB, legacy, contentRow{"c", "c.tf", "TerraformResource", "r.c", `{}`})
	if got := markXmin(t, ctx, awareDB, legacy); got != first {
		t.Fatalf("a repeat unaware write rewrote the mark (xmin %s -> %s); the upsert must be lock-only", first, got)
	}
}
