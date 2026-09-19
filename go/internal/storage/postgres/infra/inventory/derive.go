// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// mirrorPathChunkSize bounds how many paths one derive transaction covers, so
// a repo-scale Write holds the per-repo lock for one short statement pair at a
// time instead of for the whole repository.
const mirrorPathChunkSize = 500

// repoLockSQL serializes derive work per repository. The lock is taken FIRST in
// each transaction so the INSERT ... SELECT below reads content_entities under
// a statement snapshot that postdates any other deriver's commit for the same
// repo (Read Committed). Without it, a backfill that read content_entities
// before a projector commit but wrote after it would resurrect stale rows.
const repoLockSQL = `SELECT pg_advisory_xact_lock(hashtextextended('infra_resource_entities:' || $1, 0))`

const mirrorPathsDeleteSQL = `
DELETE FROM infra_resource_entities
WHERE repo_id = $1
  AND relative_path = ANY($2::text[])
`

const mirrorRepoDeleteSQL = `
DELETE FROM infra_resource_entities
WHERE repo_id = $1
`

// insertSelectColumns maps one content_entities row onto the table. Every
// dimension is trimmed and a missing or blank value becomes the empty string.
// That mirrors the canonical node writer, which trims promoted metadata strings
// and drops empty ones, so a node property absent in the graph is empty here.
const insertSelectColumns = `ce.entity_id, ce.repo_id, $3, $4, ce.relative_path, ce.entity_type, ce.entity_name,
       COALESCE(btrim(ce.metadata->>'kind'), ''),
       COALESCE(btrim(ce.metadata->>'resource_type'), ''),
       COALESCE(btrim(ce.metadata->>'data_type'), ''),
       COALESCE(btrim(ce.metadata->>'provider'), ''),
       COALESCE(btrim(ce.metadata->>'environment'), ''),
       COALESCE(btrim(ce.metadata->>'resource_service'), ''),
       COALESCE(btrim(ce.metadata->>'resource_category'), ''),
       COALESCE(btrim(ce.metadata->>'service_kind'), ''),
       now()`

const insertColumns = `entity_id, repo_id, scope_id, generation_id, relative_path, label, entity_name,
    kind, resource_type, data_type, provider, environment,
    resource_service, resource_category, service_kind, updated_at`

// insertConflictClause converges an entity whose content row moved to a path
// outside this chunk: entity_id is globally unique in content_entities, so the
// table row follows it rather than failing the whole derive.
const insertConflictClause = `
ON CONFLICT (entity_id) DO UPDATE SET
    repo_id = EXCLUDED.repo_id,
    scope_id = EXCLUDED.scope_id,
    generation_id = EXCLUDED.generation_id,
    relative_path = EXCLUDED.relative_path,
    label = EXCLUDED.label,
    entity_name = EXCLUDED.entity_name,
    kind = EXCLUDED.kind,
    resource_type = EXCLUDED.resource_type,
    data_type = EXCLUDED.data_type,
    provider = EXCLUDED.provider,
    environment = EXCLUDED.environment,
    resource_service = EXCLUDED.resource_service,
    resource_category = EXCLUDED.resource_category,
    service_kind = EXCLUDED.service_kind,
    updated_at = EXCLUDED.updated_at
`

// $1 repo_id, $2 paths, $3 scope_id, $4 generation_id, $5 labels.
const mirrorPathsInsertSQL = `
INSERT INTO infra_resource_entities (` + insertColumns + `)
SELECT ` + insertSelectColumns + `
FROM content_entities AS ce
WHERE ce.repo_id = $1
  AND ce.relative_path = ANY($2::text[])
  AND ce.entity_type = ANY($5::text[])
ORDER BY ce.entity_id` + insertConflictClause

// $1 repo_id, $2 labels, $3 scope_id, $4 generation_id.
const mirrorRepoInsertSQL = `
INSERT INTO infra_resource_entities (` + insertColumns + `)
SELECT ` + insertSelectColumns + `
FROM content_entities AS ce
WHERE ce.repo_id = $1
  AND ce.entity_type = ANY($2::text[])
ORDER BY ce.entity_id` + insertConflictClause

// Target identifies the repository a derive writes and the scope and
// generation recorded on its rows. ScopeID and GenerationID are provenance
// only: they name the generation that last projected the path, and the
// aggregate readers do not filter on them.
type Target struct {
	RepoID       string
	ScopeID      string
	GenerationID string
}

// Stats reports how many table rows one derive removed and wrote.
type Stats struct {
	Deleted  int64
	Inserted int64
}

// MirrorPaths re-derives the infra_resource_entities rows of the given paths of
// one repository from content_entities. It must run AFTER the content writer
// has finished upserting, reaping, and deleting content_entities for those
// paths, so the table converges on exactly the committed content rows: a path
// whose entities were all removed ends with no table rows.
//
// Each chunk of paths is one transaction: repo lock, delete, insert. Replaying
// the same call is idempotent. database must implement db.Beginner; a
// non-transactional database is an error, not a silent partial write.
func MirrorPaths(ctx context.Context, database db.ExecQueryer, target Target, paths []string) (Stats, error) {
	if strings.TrimSpace(target.RepoID) == "" {
		return Stats{}, errors.New("infra inventory derive: repo_id is required")
	}
	unique := normalizedPaths(paths)
	if len(unique) == 0 {
		return Stats{}, nil
	}
	beginner, ok := database.(db.Beginner)
	if !ok {
		return Stats{}, errors.New("infra inventory derive: database must support transactions")
	}

	var total Stats
	for start := 0; start < len(unique); start += mirrorPathChunkSize {
		end := min(start+mirrorPathChunkSize, len(unique))
		chunk := pgarray.StringArray(unique[start:end])
		stats, err := deriveInTransaction(ctx, beginner, target.RepoID,
			statement{mirrorPathsDeleteSQL, []any{target.RepoID, chunk}},
			statement{mirrorPathsInsertSQL, []any{target.RepoID, chunk, target.ScopeID, target.GenerationID, pgarray.StringArray(Labels)}})
		if err != nil {
			return total, fmt.Errorf("infra inventory derive %d paths for repo %q: %w", len(chunk), target.RepoID, err)
		}
		total.Deleted += stats.Deleted
		total.Inserted += stats.Inserted
	}
	return total, nil
}

// MirrorRepo re-derives every infra_resource_entities row of one repository from
// content_entities in a single transaction. The backfill uses it. Scope and
// generation provenance is left empty because content_entities does not record
// which generation last wrote a path; the next projection of a path fills it.
func MirrorRepo(ctx context.Context, database db.ExecQueryer, repoID string) (Stats, error) {
	if strings.TrimSpace(repoID) == "" {
		return Stats{}, errors.New("infra inventory derive: repo_id is required")
	}
	beginner, ok := database.(db.Beginner)
	if !ok {
		return Stats{}, errors.New("infra inventory derive: database must support transactions")
	}
	stats, err := deriveInTransaction(ctx, beginner, repoID,
		statement{mirrorRepoDeleteSQL, []any{repoID}},
		statement{mirrorRepoInsertSQL, []any{repoID, pgarray.StringArray(Labels), "", ""}})
	if err != nil {
		return Stats{}, fmt.Errorf("infra inventory derive repo %q: %w", repoID, err)
	}
	return stats, nil
}

// statement is one SQL text with its bound arguments.
type statement struct {
	query string
	args  []any
}

// deriveInTransaction runs lock, delete, insert as one transaction and rolls
// back on any failure so a chunk is never half-derived.
func deriveInTransaction(
	ctx context.Context,
	beginner db.Beginner,
	repoID string,
	deleteStmt statement,
	insertStmt statement,
) (stats Stats, err error) {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, repoLockSQL, repoID); err != nil {
		return Stats{}, fmt.Errorf("lock repo: %w", err)
	}
	deleted, err := tx.ExecContext(ctx, deleteStmt.query, deleteStmt.args...)
	if err != nil {
		return Stats{}, fmt.Errorf("delete: %w", err)
	}
	inserted, err := tx.ExecContext(ctx, insertStmt.query, insertStmt.args...)
	if err != nil {
		return Stats{}, fmt.Errorf("insert: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return Stats{}, fmt.Errorf("commit: %w", err)
	}
	stats.Deleted, _ = deleted.RowsAffected()
	stats.Inserted, _ = inserted.RowsAffected()
	return stats, nil
}

// normalizedPaths trims, drops blanks, deduplicates, and sorts so chunk
// boundaries and lock order are reproducible.
func normalizedPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if _, dup := seen[path]; dup {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
