// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// refDeltaSchemaSQL is the idempotent DDL for the #5393 Phase 1 content
// overlay. It mirrors migrations/197_content_file_ref_deltas.sql name for
// name; TestDDLMigrationParity fails the build if either side drifts.
const refDeltaSchemaSQL = `CREATE TABLE IF NOT EXISTS content_file_ref_deltas (
    repo_id TEXT NOT NULL,
    ref TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    content TEXT NULL,
    content_hash TEXT NOT NULL,
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    indexed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repo_id, ref, relative_path),
    CHECK ((is_deleted AND content IS NULL) OR (NOT is_deleted AND content IS NOT NULL))
);
CREATE TABLE IF NOT EXISTS content_file_ref_delta_bases (
    repo_id TEXT NOT NULL,
    ref TEXT NOT NULL,
    base_default_sha TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repo_id, ref)
);
CREATE INDEX IF NOT EXISTS content_file_ref_deltas_content_trgm_idx
    ON content_file_ref_deltas USING gin (content gin_trgm_ops);
`

// upsertRefDeltaQuery persists one overlay row: a diverged file or a
// branch-deletion tombstone (content NULL, is_deleted true). One statement
// serves both shapes so the conflict target cannot drift between them.
const upsertRefDeltaQuery = `
INSERT INTO content_file_ref_deltas (
    repo_id, ref, relative_path, content, content_hash,
    is_deleted, indexed_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (repo_id, ref, relative_path) DO UPDATE
SET content = EXCLUDED.content,
    content_hash = EXCLUDED.content_hash,
    is_deleted = EXCLUDED.is_deleted,
    indexed_at = EXCLUDED.indexed_at
`

// deleteRefDeltaQuery converges one path back to no-row when its branch
// content matches the default again.
const deleteRefDeltaQuery = `
DELETE FROM content_file_ref_deltas
WHERE repo_id = $1
  AND ref = $2
  AND relative_path = $3
`

// deleteRefDeltasQuery retracts a whole ref overlay. The ref stays in the
// key (#5393 invariant 2): a retraction can never spill past its ref.
const deleteRefDeltasQuery = `
DELETE FROM content_file_ref_deltas
WHERE repo_id = $1
  AND ref = $2
`

// upsertRefDeltaBaseQuery records the default-branch SHA a ref's deltas
// were diffed against.
const upsertRefDeltaBaseQuery = `
INSERT INTO content_file_ref_delta_bases (
    repo_id, ref, base_default_sha, updated_at
) VALUES (
    $1, $2, $3, $4
)
ON CONFLICT (repo_id, ref) DO UPDATE
SET base_default_sha = EXCLUDED.base_default_sha,
    updated_at = EXCLUDED.updated_at
`

// selectRefDeltaBaseQuery is the bounded re-diff probe: one primary-key
// lookup, no scan.
const selectRefDeltaBaseQuery = `
SELECT base_default_sha
FROM content_file_ref_delta_bases
WHERE repo_id = $1
  AND ref = $2
`

// deleteRefDeltaBaseQuery drops a ref's base record when its overlay is
// retired.
const deleteRefDeltaBaseQuery = `
DELETE FROM content_file_ref_delta_bases
WHERE repo_id = $1
  AND ref = $2
`

// RefFileDelta is one branch-observed file: either diverged content or a
// branch deletion (Deleted). Content is empty for tombstones; ContentHash
// carries the branch content hash for live rows and the default content
// hash observed at delete time for tombstones.
type RefFileDelta struct {
	RepoID       string
	Ref          string
	RelativePath string
	Content      string
	ContentHash  string
	Deleted      bool
}

// RefDeltaStore owns the #5393 Phase 1 content overlay: hash-gated delta
// rows, branch-deletion tombstones, and the per-ref base_default_sha ledger
// behind the bounded re-diff trigger.
//
// Isolation contract: every statement here targets only the overlay tables.
// Nothing in this store reads or writes content_files, canonical nodes, or
// edges; TestRefDeltaStatementsNeverWriteCanonical guards that
// structurally. This store also performs no extraction, projection, or
// serving reads -- it is the persistence seam future ref writers and the
// ref-scoped read path build on.
type RefDeltaStore struct {
	database db.ExecQueryer
	Now      func() time.Time
}

// NewRefDeltaStore constructs an overlay store over the given database.
func NewRefDeltaStore(database db.ExecQueryer) RefDeltaStore {
	return RefDeltaStore{database: database}
}

// RefDeltaSchemaSQL returns the idempotent DDL for the overlay tables,
// mirroring migrations/197_content_file_ref_deltas.sql.
func RefDeltaSchemaSQL() string {
	return refDeltaSchemaSQL
}

// UpsertRefDelta writes one overlay row, hash-gated against the default
// branch: a live row whose content hash equals defaultContentHash converges
// to a delete (no delta row shadows identical canonical content), a
// diverged row upserts, and a Deleted row upserts a tombstone. A blank
// defaultContentHash fails closed -- without it the gate cannot decide, so
// no write happens.
func (s RefDeltaStore) UpsertRefDelta(
	ctx context.Context,
	delta RefFileDelta,
	defaultContentHash string,
) error {
	if s.database == nil {
		return fmt.Errorf("ref delta store database is required")
	}
	if strings.TrimSpace(delta.RepoID) == "" {
		return fmt.Errorf("ref delta repo_id is required")
	}
	if strings.TrimSpace(delta.Ref) == "" {
		return fmt.Errorf("ref delta ref is required")
	}
	if strings.TrimSpace(delta.RelativePath) == "" {
		return fmt.Errorf("ref delta relative_path is required")
	}
	if strings.TrimSpace(delta.ContentHash) == "" {
		return fmt.Errorf("ref delta content_hash is required")
	}
	if strings.TrimSpace(defaultContentHash) == "" {
		return fmt.Errorf("ref delta default content hash is required for hash-gating")
	}

	if !delta.Deleted && delta.ContentHash == defaultContentHash {
		return s.DeleteRefDelta(ctx, delta.RepoID, delta.Ref, delta.RelativePath)
	}

	var content any = delta.Content
	if delta.Deleted {
		content = nil
	}
	if _, err := s.database.ExecContext(
		ctx,
		upsertRefDeltaQuery,
		delta.RepoID,
		delta.Ref,
		delta.RelativePath,
		content,
		delta.ContentHash,
		delta.Deleted,
		s.now(),
	); err != nil {
		return fmt.Errorf("upsert content_file_ref_deltas for %q: %w", delta.RelativePath, err)
	}
	return nil
}

// WriteRefTombstone records that relativePath is deleted on ref.
// defaultContentHash is the default-branch hash observed at delete time --
// it documents what the tombstone suppresses. Tombstone rows carry NULL
// content so a future delta-over-default read suppresses the path instead
// of falling back to the default copy.
func (s RefDeltaStore) WriteRefTombstone(
	ctx context.Context,
	repoID string,
	ref string,
	relativePath string,
	defaultContentHash string,
) error {
	return s.UpsertRefDelta(ctx, RefFileDelta{
		RepoID:       repoID,
		Ref:          ref,
		RelativePath: relativePath,
		ContentHash:  defaultContentHash,
		Deleted:      true,
	}, defaultContentHash)
}

// DeleteRefDelta removes one path's overlay row.
func (s RefDeltaStore) DeleteRefDelta(
	ctx context.Context,
	repoID string,
	ref string,
	relativePath string,
) error {
	if s.database == nil {
		return fmt.Errorf("ref delta store database is required")
	}
	if _, err := s.database.ExecContext(ctx, deleteRefDeltaQuery, repoID, ref, relativePath); err != nil {
		return fmt.Errorf("delete content_file_ref_deltas for %q: %w", relativePath, err)
	}
	return nil
}

// DeleteRefDeltas retracts a whole ref overlay for one repo. Callers
// retracting overlay data keep the ref in the key; this is the statement
// they use.
func (s RefDeltaStore) DeleteRefDeltas(
	ctx context.Context,
	repoID string,
	ref string,
) error {
	if s.database == nil {
		return fmt.Errorf("ref delta store database is required")
	}
	if _, err := s.database.ExecContext(ctx, deleteRefDeltasQuery, repoID, ref); err != nil {
		return fmt.Errorf("delete content_file_ref_deltas for ref %q: %w", ref, err)
	}
	return nil
}

// RecordDeltaBase stores the default-branch SHA a ref's deltas were diffed
// against. Writers call it once per diff batch, not per row.
func (s RefDeltaStore) RecordDeltaBase(
	ctx context.Context,
	repoID string,
	ref string,
	baseDefaultSHA string,
) error {
	if s.database == nil {
		return fmt.Errorf("ref delta store database is required")
	}
	if strings.TrimSpace(repoID) == "" || strings.TrimSpace(ref) == "" || strings.TrimSpace(baseDefaultSHA) == "" {
		return fmt.Errorf("ref delta base requires repo_id, ref, and base_default_sha")
	}
	if _, err := s.database.ExecContext(ctx, upsertRefDeltaBaseQuery, repoID, ref, baseDefaultSHA, s.now()); err != nil {
		return fmt.Errorf("record content_file_ref_delta_bases for ref %q: %w", ref, err)
	}
	return nil
}

// DeltaBaseStale is the bounded re-diff trigger: one primary-key lookup
// comparing the recorded base against the current default SHA. It reports
// true when the default advanced past the base, and also when no base was
// ever recorded -- an undiffed ref must be diffed, never trusted.
func (s RefDeltaStore) DeltaBaseStale(
	ctx context.Context,
	repoID string,
	ref string,
	currentDefaultSHA string,
) (bool, error) {
	if s.database == nil {
		return false, fmt.Errorf("ref delta store database is required")
	}

	rows, err := s.database.QueryContext(ctx, selectRefDeltaBaseQuery, repoID, ref)
	if err != nil {
		return false, fmt.Errorf("read delta base for ref %q: %w", ref, err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("read delta base for ref %q: %w", ref, err)
		}
		return true, nil
	}
	var base string
	if err := rows.Scan(&base); err != nil {
		return false, fmt.Errorf("scan delta base for ref %q: %w", ref, err)
	}
	return base != currentDefaultSHA, nil
}

// ClearDeltaBase drops a ref's base record when its overlay is retired.
func (s RefDeltaStore) ClearDeltaBase(
	ctx context.Context,
	repoID string,
	ref string,
) error {
	if s.database == nil {
		return fmt.Errorf("ref delta store database is required")
	}
	if _, err := s.database.ExecContext(ctx, deleteRefDeltaBaseQuery, repoID, ref); err != nil {
		return fmt.Errorf("clear content_file_ref_delta_bases for ref %q: %w", ref, err)
	}
	return nil
}

func (s RefDeltaStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// refDeltaWriteToCanonical matches a SQL write verb targeting the canonical
// content_files table: INSERT INTO, UPDATE, DELETE FROM, TRUNCATE, or MERGE.
// Comment text is stripped before matching, so prose mentioning canonical
// tables cannot trip the guard. The overlay's own table
// (content_file_ref_deltas) never matches: the pattern requires the exact
// canonical name at a word boundary.
var refDeltaWriteToCanonical = regexp.MustCompile(
	`(?i)\b(INSERT\s+INTO|UPDATE|DELETE\s+FROM|TRUNCATE(\s+TABLE)?|MERGE(\s+INTO)?)\s+"?content_files\b`,
)

// refDeltaStatementInventory returns every SQL statement this store can
// emit, so the isolation guard test asserts over the real write surface
// instead of a copy.
func refDeltaStatementInventory() []string {
	return []string{
		refDeltaSchemaSQL,
		upsertRefDeltaQuery,
		deleteRefDeltaQuery,
		deleteRefDeltasQuery,
		upsertRefDeltaBaseQuery,
		selectRefDeltaBaseQuery,
		deleteRefDeltaBaseQuery,
	}
}

// stripRefDeltaSQLComments removes -- line comments and /* */ block
// comments so the canonical-write guard reads statements, not prose.
func stripRefDeltaSQLComments(statement string) string {
	var kept []string
	inBlock := false
	for _, line := range strings.Split(statement, "\n") {
		if inBlock {
			if end := strings.Index(line, "*/"); end >= 0 {
				line = line[end+2:]
				inBlock = false
			} else {
				continue
			}
		}
		for {
			start := strings.Index(line, "/*")
			if start < 0 {
				break
			}
			if end := strings.Index(line[start+2:], "*/"); end >= 0 {
				line = line[:start] + line[start+2+end+2:]
				continue
			}
			line = line[:start]
			inBlock = true
			break
		}
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// assertRefDeltaStatementsNeverWriteCanonical fails when any statement
// writes the canonical content_files table. It is the structural half of
// the #5393 Phase 1 key invariant: a delta row never shadows canonical
// content, because the overlay store has no statement that can write it.
func assertRefDeltaStatementsNeverWriteCanonical(statements []string) error {
	for _, statement := range statements {
		if refDeltaWriteToCanonical.MatchString(stripRefDeltaSQLComments(statement)) {
			return fmt.Errorf("ref-delta overlay statement writes canonical content_files: %q",
				firstLine(statement))
		}
	}
	return nil
}

func firstLine(statement string) string {
	if idx := strings.Index(statement, "\n"); idx >= 0 {
		return strings.TrimSpace(statement[:idx])
	}
	return strings.TrimSpace(statement)
}
