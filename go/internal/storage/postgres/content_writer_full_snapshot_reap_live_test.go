// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// These live tests prove #7447 item 5 against real Postgres 18 with the real
// content_store definitions: a full snapshot must not leave a content row for a
// path an earlier, never-activated generation wrote. They reuse the isolated
// schema opener from the fingerprint reap proof and skip without
// ESHU_POSTGRES_TEST_DSN.

const fullSnapshotReapRepo = "github.com/acme/app"

func fullSnapshotFile(path string) content.Record {
	return content.Record{Path: path, Body: "package p // " + path + "\n"}
}

func fullSnapshotEntity(path string) content.EntityRecord {
	return content.EntityRecord{
		EntityID:   "content-entity:" + fullSnapshotReapRepo + ":" + path,
		Path:       path,
		EntityType: "Function",
		EntityName: "F",
		StartLine:  1,
		EndLine:    2,
	}
}

func fullSnapshotMaterialization(repoID, generation string, full bool, paths ...string) content.Materialization {
	mat := content.Materialization{
		RepoID:       repoID,
		ScopeID:      "scope-" + repoID,
		GenerationID: generation,
		FullSnapshot: full,
	}
	for _, path := range paths {
		mat.Records = append(mat.Records, fullSnapshotFile(path))
		mat.Entities = append(mat.Entities, fullSnapshotEntity(path))
	}
	return mat
}

func liveStringColumn(ctx context.Context, t *testing.T, database *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query %.80q: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	sort.Strings(out)
	return out
}

func contentFilePaths(ctx context.Context, t *testing.T, database *sql.DB, repoID string) []string {
	t.Helper()
	return liveStringColumn(ctx, t, database,
		"SELECT relative_path FROM content_files WHERE repo_id = $1 ORDER BY 1", repoID)
}

func contentEntityPaths(ctx context.Context, t *testing.T, database *sql.DB, repoID string) []string {
	t.Helper()
	return liveStringColumn(ctx, t, database,
		"SELECT DISTINCT relative_path FROM content_entities WHERE repo_id = $1 ORDER BY 1", repoID)
}

func contentReferencePaths(ctx context.Context, t *testing.T, database *sql.DB, repoID string) []string {
	t.Helper()
	return liveStringColumn(ctx, t, database,
		"SELECT DISTINCT relative_path FROM content_file_references WHERE repo_id = $1 ORDER BY 1", repoID)
}

func seedContentReference(ctx context.Context, t *testing.T, database *sql.DB, repoID, path string) {
	t.Helper()
	mustExecLive(ctx, t, database, `
INSERT INTO content_file_references (repo_id, relative_path, reference_kind, reference_value, indexed_at)
VALUES ($1, $2, 'import', 'fmt', now())`, repoID, path)
}

func requirePaths(t *testing.T, label string, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if want == nil {
		want = []string{}
	}
	if got == nil {
		got = []string{}
	}
	if reflect.DeepEqual(got, want) {
		return
	}
	// A large mismatch (the 1,200-path chunk test) would print every path, so
	// summarize past a short list.
	const shown = 12
	if len(got) > shown || len(want) > shown {
		t.Fatalf("%s: got %d paths, want %d; first got %v, first want %v",
			label, len(got), len(want), got[:min(len(got), shown)], want[:min(len(want), shown)])
	}
	t.Fatalf("%s = %v, want %v", label, got, want)
}

func mustWriteContent(ctx context.Context, t *testing.T, writer ContentWriter, mat content.Materialization) {
	t.Helper()
	if _, err := writer.Write(ctx, mat); err != nil {
		t.Fatalf("Write(%s) error = %v", mat.GenerationID, err)
	}
}

// TestContentWriterFullSnapshotReapsPathsAWriterLeftBehind is the #7389
// reproduction shape at the content store. Generation A wrote keep/w/y. The
// delta B wrote x.go (new) and deleted w.go, then was superseded and never
// activated. The full snapshot D at the tree {keep, w, y} must leave exactly
// that tree in content_files, content_entities and content_file_references,
// where before it left x.go in all three.
func TestContentWriterFullSnapshotReapsPathsAWriterLeftBehind(t *testing.T) {
	ctx, database := openFingerprintReapLiveDB(t)
	writer := NewContentWriter(SQLDB{DB: database})

	mustWriteContent(ctx, t, writer, fullSnapshotMaterialization(fullSnapshotReapRepo, "gen-a", true, "keep.go", "w.go", "y.go"))

	deltaB := fullSnapshotMaterialization(fullSnapshotReapRepo, "gen-b", false, "keep.go", "x.go")
	deltaB.Records = append(deltaB.Records, content.Record{Path: "w.go", Deleted: true})
	mustWriteContent(ctx, t, writer, deltaB)
	seedContentReference(ctx, t, database, fullSnapshotReapRepo, "x.go")
	requirePaths(t, "after superseded delta B, content_files", contentFilePaths(ctx, t, database, fullSnapshotReapRepo), "keep.go", "x.go", "y.go")

	mustWriteContent(ctx, t, writer, fullSnapshotMaterialization(fullSnapshotReapRepo, "gen-d", true, "keep.go", "w.go", "y.go"))

	requirePaths(t, "after full snapshot D, content_files", contentFilePaths(ctx, t, database, fullSnapshotReapRepo), "keep.go", "w.go", "y.go")
	requirePaths(t, "after full snapshot D, content_entities paths", contentEntityPaths(ctx, t, database, fullSnapshotReapRepo), "keep.go", "w.go", "y.go")
	requirePaths(t, "after full snapshot D, content_file_references paths", contentReferencePaths(ctx, t, database, fullSnapshotReapRepo))
}

// TestContentWriterDeltaNeverReapsUnlistedPaths pins that only a full snapshot
// reaps: a delta names the paths it touched and must leave every other path.
func TestContentWriterDeltaNeverReapsUnlistedPaths(t *testing.T) {
	ctx, database := openFingerprintReapLiveDB(t)
	writer := NewContentWriter(SQLDB{DB: database})

	mustWriteContent(ctx, t, writer, fullSnapshotMaterialization(fullSnapshotReapRepo, "gen-a", true, "a.go", "b.go", "c.go"))
	mustWriteContent(ctx, t, writer, fullSnapshotMaterialization(fullSnapshotReapRepo, "gen-b", false, "a.go"))

	requirePaths(t, "content_files after a delta touching only a.go", contentFilePaths(ctx, t, database, fullSnapshotReapRepo), "a.go", "b.go", "c.go")
	requirePaths(t, "content_entities after a delta", contentEntityPaths(ctx, t, database, fullSnapshotReapRepo), "a.go", "b.go", "c.go")
}

// TestContentWriterFullSnapshotReapIsScopedToItsRepository proves the reap is
// keyed by repo_id: another repository's rows are never touched.
func TestContentWriterFullSnapshotReapIsScopedToItsRepository(t *testing.T) {
	ctx, database := openFingerprintReapLiveDB(t)
	writer := NewContentWriter(SQLDB{DB: database})
	const other = "github.com/acme/other"

	mustWriteContent(ctx, t, writer, fullSnapshotMaterialization(other, "gen-o", true, "o1.go", "o2.go"))
	mustWriteContent(ctx, t, writer, fullSnapshotMaterialization(fullSnapshotReapRepo, "gen-a", true, "keep.go", "stale.go"))
	mustWriteContent(ctx, t, writer, fullSnapshotMaterialization(fullSnapshotReapRepo, "gen-d", true, "keep.go"))

	requirePaths(t, "reaped repo content_files", contentFilePaths(ctx, t, database, fullSnapshotReapRepo), "keep.go")
	requirePaths(t, "other repo content_files", contentFilePaths(ctx, t, database, other), "o1.go", "o2.go")
	requirePaths(t, "other repo content_entities", contentEntityPaths(ctx, t, database, other), "o1.go", "o2.go")
}

// TestContentWriterFullSnapshotReapsAcrossDeleteChunks drives more stale paths
// than one 500-row delete batch carries, so a chunking slip cannot hide.
func TestContentWriterFullSnapshotReapsAcrossDeleteChunks(t *testing.T) {
	ctx, database := openFingerprintReapLiveDB(t)
	writer := NewContentWriter(SQLDB{DB: database})

	const stale = 1200
	mustExecLive(ctx, t, database, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
SELECT $1, 'old/f' || g || '.go', 'x', md5(g::text), 1, now() FROM generate_series(1, $2::int) g`,
		fullSnapshotReapRepo, stale)
	mustExecLive(ctx, t, database, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line, source_cache, indexed_at)
SELECT 'content-entity:old:' || g, $1, 'old/f' || g || '.go', 'Function', 'F', 1, 2, 'x', now() FROM generate_series(1, $2::int) g`,
		fullSnapshotReapRepo, stale)

	mustWriteContent(ctx, t, writer, fullSnapshotMaterialization(fullSnapshotReapRepo, "gen-d", true, "keep.go"))

	requirePaths(t, "content_files after reaping 1200 stale paths", contentFilePaths(ctx, t, database, fullSnapshotReapRepo), "keep.go")
	requirePaths(t, "content_entities after reaping 1200 stale paths", contentEntityPaths(ctx, t, database, fullSnapshotReapRepo), "keep.go")
}

// TestContentWriterFullSnapshotOfEmptyRepositoryReapsItsContent mirrors the
// canonical graph writer, whose full retract removes every file of a repository
// that is now empty. The other repository stays.
func TestContentWriterFullSnapshotOfEmptyRepositoryReapsItsContent(t *testing.T) {
	ctx, database := openFingerprintReapLiveDB(t)
	writer := NewContentWriter(SQLDB{DB: database})
	const other = "github.com/acme/other"

	mustWriteContent(ctx, t, writer, fullSnapshotMaterialization(other, "gen-o", true, "o.go"))
	mustWriteContent(ctx, t, writer, fullSnapshotMaterialization(fullSnapshotReapRepo, "gen-a", true, "a.go", "b.go"))
	mustWriteContent(ctx, t, writer, fullSnapshotMaterialization(fullSnapshotReapRepo, "gen-d", true))

	requirePaths(t, "content_files of the emptied repository", contentFilePaths(ctx, t, database, fullSnapshotReapRepo))
	requirePaths(t, "content_entities of the emptied repository", contentEntityPaths(ctx, t, database, fullSnapshotReapRepo))
	requirePaths(t, "other repository content_files", contentFilePaths(ctx, t, database, other), "o.go")
}

// TestContentWriterFullSnapshotReapIsIdempotent replays the same full snapshot,
// which is what a retried projection does, and expects the same tree.
func TestContentWriterFullSnapshotReapIsIdempotent(t *testing.T) {
	ctx, database := openFingerprintReapLiveDB(t)
	writer := NewContentWriter(SQLDB{DB: database})

	mustWriteContent(ctx, t, writer, fullSnapshotMaterialization(fullSnapshotReapRepo, "gen-a", true, "keep.go", "stale.go"))
	for i := 0; i < 2; i++ {
		mustWriteContent(ctx, t, writer, fullSnapshotMaterialization(fullSnapshotReapRepo, fmt.Sprintf("gen-d-%d", i), true, "keep.go"))
		requirePaths(t, fmt.Sprintf("content_files after replay %d", i), contentFilePaths(ctx, t, database, fullSnapshotReapRepo), "keep.go")
	}
}
