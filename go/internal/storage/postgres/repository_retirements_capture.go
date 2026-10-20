// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// GraphRetractKeys are the stable graph-phase keys step 2c runs on. The
// purge deletes the live rows they are derived from, so the capture seam
// writes them to the tombstone once (migration 208) and every resume
// reads the tombstone copy, never the live rows.
type GraphRetractKeys struct {
	// ScopeIDs are the repository's scope ids (default-branch scope plus
	// @ref scopes), ordered, captured with the same predicate the purge
	// locks (partition_key = repo id).
	ScopeIDs []string
	// Root is the repository root with the precedence BuildMaterialization
	// uses: the repository fact's path in the scope's active generation,
	// else its local_path, else the scope payload's repo_path. Empty only
	// when no live row names a root, which fails the retirement closed.
	Root string
}

// retirementCaptureScopesSQL derives the repository's scope ids with the
// same predicate the purge locks.
const retirementCaptureScopesSQL = `SELECT scope_id FROM ingestion_scopes
	WHERE partition_key = $1 ORDER BY scope_id`

// retirementCaptureRootSQL derives the repository root: the newest
// repository fact's path (else local_path) in each scope's active
// generation, falling back to the scope payload's repo_path. Scopes are
// visited in scope_id order and the first non-empty root wins, so the
// capture is deterministic for a repository whose scopes agree (the fence
// guarantees they do: one checkout, one root).
const retirementCaptureRootSQL = `WITH candidate_roots AS (
	SELECT f.scope_id, f.observed_at, f.fact_id,
		COALESCE(NULLIF(f.payload->>'path', ''), NULLIF(f.payload->>'local_path', '')) AS root
	FROM fact_records f
	WHERE f.scope_id = ANY($1) AND f.fact_kind = 'repository' AND f.source_system = 'git'
	  AND f.is_tombstone = FALSE AND f.generation_id = (
		SELECT s.active_generation_id FROM ingestion_scopes s
		WHERE s.scope_id = f.scope_id)
	UNION ALL
	SELECT s.scope_id, s.observed_at, s.scope_id,
		NULLIF(s.payload->>'repo_path', '')
	FROM ingestion_scopes s
	WHERE s.scope_id = ANY($1)
)
SELECT root FROM candidate_roots
	WHERE root IS NOT NULL AND root <> ''
	ORDER BY scope_id, observed_at DESC, fact_id DESC
	LIMIT 1`

// CaptureGraphRetractKeys derives the 2c graph-phase keys for one
// repository id from the live rows. It is read-only: storing is a
// separate write-once step so a crash between derive and store re-derives
// instead of running on a half-written capture.
func CaptureGraphRetractKeys(ctx context.Context, database db.ExecQueryer, repoID string) (GraphRetractKeys, error) {
	var keys GraphRetractKeys
	if strings.TrimSpace(repoID) == "" {
		return keys, fmt.Errorf("retirement capture graph keys: empty repo id")
	}
	scopeRows, err := database.QueryContext(ctx, retirementCaptureScopesSQL, repoID)
	if err != nil {
		return keys, fmt.Errorf("retirement capture scopes: %w", err)
	}
	for scopeRows.Next() {
		var scopeID string
		if err := scopeRows.Scan(&scopeID); err != nil {
			_ = scopeRows.Close()
			return keys, fmt.Errorf("retirement capture scopes scan: %w", err)
		}
		keys.ScopeIDs = append(keys.ScopeIDs, scopeID)
	}
	if err := scopeRows.Err(); err != nil {
		_ = scopeRows.Close()
		return keys, fmt.Errorf("retirement capture scopes: %w", err)
	}
	_ = scopeRows.Close()
	if len(keys.ScopeIDs) == 0 {
		return keys, fmt.Errorf("retirement capture graph keys %s: no scopes carry its partition key", repoID)
	}
	rootRows, err := database.QueryContext(ctx, retirementCaptureRootSQL, keys.ScopeIDs)
	if err != nil {
		return keys, fmt.Errorf("retirement capture root: %w", err)
	}
	defer func() { _ = rootRows.Close() }()
	if rootRows.Next() {
		if err := rootRows.Scan(&keys.Root); err != nil {
			return keys, fmt.Errorf("retirement capture root scan: %w", err)
		}
	}
	if err := rootRows.Err(); err != nil {
		return keys, fmt.Errorf("retirement capture root: %w", err)
	}
	return keys, nil
}

// retirementStoreGraphKeysSQL writes the capture once. The
// graph_repo_root IS NULL arm makes the write idempotent: a resume that
// already captured keeps the first values, never the re-derived ones (2c
// may already have cleared Repository.path by then).
const retirementStoreGraphKeysSQL = `UPDATE repository_retirements
	SET graph_repo_root = $2, graph_scope_ids = $3, updated_at = $4
	WHERE retirement_id = $1 AND readmitted_at IS NULL AND graph_repo_root IS NULL`

// StoreGraphRetractKeys writes a capture to the tombstone once and
// reports whether it stored. False means the tombstone already holds a
// capture (resume path), is gone, or was readmitted: the caller must not
// overwrite and must not proceed to the graph phase on a readmitted row.
func StoreGraphRetractKeys(ctx context.Context, database db.ExecQueryer, retirementID string, keys GraphRetractKeys) (bool, error) {
	if strings.TrimSpace(retirementID) == "" {
		return false, fmt.Errorf("retirement store graph keys: empty retirement id")
	}
	scopeIDs, err := json.Marshal(keys.ScopeIDs)
	if err != nil {
		return false, fmt.Errorf("retirement store graph keys marshal: %w", err)
	}
	var root any
	if keys.Root != "" {
		root = keys.Root
	}
	result, err := database.ExecContext(ctx, retirementStoreGraphKeysSQL,
		retirementID, root, string(scopeIDs), time.Now().UTC())
	if err != nil {
		return false, fmt.Errorf("retirement store graph keys: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("retirement store graph keys rows: %w", err)
	}
	return affected == 1, nil
}

// ReadGraphRetractKeys reads the tombstone's capture back. Every 2c
// resume (and the 2z census) reads these columns, never the live rows the
// purge may already have deleted.
func ReadGraphRetractKeys(ctx context.Context, database db.ExecQueryer, retirementID string) (GraphRetractKeys, error) {
	var keys GraphRetractKeys
	rows, err := database.QueryContext(ctx, `SELECT graph_repo_root, graph_scope_ids::text
		FROM repository_retirements WHERE retirement_id = $1`, retirementID)
	if err != nil {
		return keys, fmt.Errorf("retirement read graph keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var root sql.NullString
	var scopeIDs string
	found := false
	if rows.Next() {
		found = true
		if err := rows.Scan(&root, &scopeIDs); err != nil {
			return keys, fmt.Errorf("retirement read graph keys scan: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return keys, fmt.Errorf("retirement read graph keys: %w", err)
	}
	if !found {
		return keys, fmt.Errorf("retirement read graph keys %s: no tombstone row", retirementID)
	}
	if root.Valid {
		keys.Root = root.String
	}
	if err := json.Unmarshal([]byte(scopeIDs), &keys.ScopeIDs); err != nil {
		return keys, fmt.Errorf("retirement read graph keys unmarshal: %w", err)
	}
	return keys, nil
}
