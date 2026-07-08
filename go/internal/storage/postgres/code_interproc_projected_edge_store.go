// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	codeInterprocProjectedEdgeBatchSize = 500
	codeInterprocProjectedEdgeColumns   = 5
)

// codeInterprocProjectedEdgeSchemaSQL is the durable ledger of source Function
// uids of every projected TAINT_FLOWS_TO edge. The ledger is written before the
// graph edge write so it is always a superset of graph edges — over-inclusion
// is harmless because the Cypher retract WHERE still filters; under-inclusion
// would orphan.
const codeInterprocProjectedEdgeSchemaSQL = `CREATE TABLE IF NOT EXISTS code_interproc_projected_edge (
    evidence_source TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    source_function_uid TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (evidence_source, scope_id, generation_id, source_function_uid)
);
CREATE INDEX IF NOT EXISTS code_interproc_projected_edge_source_scope_idx
    ON code_interproc_projected_edge (evidence_source, scope_id);
CREATE INDEX IF NOT EXISTS code_interproc_projected_edge_source_idx
    ON code_interproc_projected_edge (evidence_source);
CREATE INDEX IF NOT EXISTS code_interproc_projected_edge_stale_idx
    ON code_interproc_projected_edge (evidence_source, scope_id, generation_id);
`

const upsertCodeInterprocProjectedEdgeBatchPrefix = `
INSERT INTO code_interproc_projected_edge (
    evidence_source, scope_id, generation_id, source_function_uid, updated_at
) VALUES `

const upsertCodeInterprocProjectedEdgeBatchSuffix = `
ON CONFLICT (evidence_source, scope_id, generation_id, source_function_uid) DO UPDATE
SET updated_at = EXCLUDED.updated_at
`

const listCodeInterprocSourceUIDsForScopesSQL = `
SELECT DISTINCT source_function_uid
FROM code_interproc_projected_edge
WHERE evidence_source = $1
  AND scope_id = ANY($2)
ORDER BY source_function_uid
`

const listCodeInterprocSourceUIDsForSourceSQL = `
SELECT DISTINCT source_function_uid
FROM code_interproc_projected_edge
WHERE evidence_source = $1
ORDER BY source_function_uid
`

const listStaleCodeInterprocSourceUIDsSQL = `
SELECT DISTINCT source_function_uid
FROM code_interproc_projected_edge
WHERE evidence_source = $1
  AND scope_id = $2
  AND generation_id <> $3
ORDER BY source_function_uid
LIMIT $4
`

const pruneCodeInterprocForScopesSQL = `
DELETE FROM code_interproc_projected_edge
WHERE evidence_source = $1
  AND scope_id = ANY($2)
`

const pruneCodeInterprocForSourceSQL = `
DELETE FROM code_interproc_projected_edge
WHERE evidence_source = $1
`

const pruneStaleCodeInterprocSQL = `
DELETE FROM code_interproc_projected_edge
WHERE evidence_source = $1
  AND scope_id = $2
  AND generation_id <> $3
`

const codeInterprocHasRowsForSourceSQL = `
SELECT EXISTS(
    SELECT 1 FROM code_interproc_projected_edge WHERE evidence_source = $1
)
`

// CodeInterprocProjectedEdgeSchemaSQL returns the DDL for the projected-edge ledger.
func CodeInterprocProjectedEdgeSchemaSQL() string {
	return codeInterprocProjectedEdgeSchemaSQL
}

// CodeInterprocProjectedEdgeStore records the source Function uids of every
// projected TAINT_FLOWS_TO edge so retraction can enumerate uids from the
// ledger instead of scanning the whole graph.
type CodeInterprocProjectedEdgeStore struct {
	db ExecQueryer
}

// NewCodeInterprocProjectedEdgeStore constructs a Postgres-backed projected-edge
// ledger.
func NewCodeInterprocProjectedEdgeStore(db ExecQueryer) CodeInterprocProjectedEdgeStore {
	return CodeInterprocProjectedEdgeStore{db: db}
}

// EnsureSchema applies the projected-edge ledger DDL.
func (s CodeInterprocProjectedEdgeStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("code interproc projected edge store database is required")
	}
	if _, err := s.db.ExecContext(ctx, codeInterprocProjectedEdgeSchemaSQL); err != nil {
		return fmt.Errorf("ensure code interproc projected edge schema: %w", err)
	}
	return nil
}

// RecordProjectedEdges records source Function uids, idempotent on the primary
// key. Blank uids are skipped and duplicate uids within a batch are de-duplicated
// before insert to avoid SQLSTATE 21000.
func (s CodeInterprocProjectedEdgeStore) RecordProjectedEdges(
	ctx context.Context,
	evidenceSource string,
	scopeID string,
	generationID string,
	sourceFunctionUIDs []string,
	updatedAt time.Time,
) error {
	if s.db == nil {
		return fmt.Errorf("code interproc projected edge store database is required")
	}
	if updatedAt.IsZero() {
		return fmt.Errorf("code interproc projected edge updated_at is required")
	}
	seen := make(map[string]struct{}, len(sourceFunctionUIDs))
	unique := make([]string, 0, len(sourceFunctionUIDs))
	for _, uid := range sourceFunctionUIDs {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		unique = append(unique, uid)
	}
	if len(unique) == 0 {
		return nil
	}
	sort.Strings(unique)
	utc := updatedAt.UTC()
	for i := 0; i < len(unique); i += codeInterprocProjectedEdgeBatchSize {
		end := i + codeInterprocProjectedEdgeBatchSize
		if end > len(unique) {
			end = len(unique)
		}
		if err := s.upsertBatch(ctx, evidenceSource, scopeID, generationID, unique[i:end], utc); err != nil {
			return err
		}
	}
	return nil
}

func (s CodeInterprocProjectedEdgeStore) upsertBatch(
	ctx context.Context,
	evidenceSource string,
	scopeID string,
	generationID string,
	uids []string,
	updatedAt time.Time,
) error {
	values := make([]string, 0, len(uids))
	args := make([]any, 0, len(uids)*codeInterprocProjectedEdgeColumns)
	for _, uid := range uids {
		base := len(args)
		placeholders := make([]string, 0, codeInterprocProjectedEdgeColumns)
		for j := 1; j <= codeInterprocProjectedEdgeColumns; j++ {
			placeholders = append(placeholders, fmt.Sprintf("$%d", base+j))
		}
		values = append(values, "("+strings.Join(placeholders, ", ")+")")
		args = append(args, evidenceSource, scopeID, generationID, uid, updatedAt)
	}
	query := upsertCodeInterprocProjectedEdgeBatchPrefix + strings.Join(values, ", ") + upsertCodeInterprocProjectedEdgeBatchSuffix
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert code interproc projected edges: %w", err)
	}
	return nil
}

// ListSourceUIDsForScopes returns the distinct source Function uids recorded for
// the given evidence source and scope IDs.
func (s CodeInterprocProjectedEdgeStore) ListSourceUIDsForScopes(
	ctx context.Context,
	evidenceSource string,
	scopeIDs []string,
) ([]string, error) {
	if s.db == nil {
		return nil, fmt.Errorf("code interproc projected edge store database is required")
	}
	rows, err := s.db.QueryContext(ctx, listCodeInterprocSourceUIDsForScopesSQL, evidenceSource, scopeIDs)
	if err != nil {
		return nil, fmt.Errorf("list code interproc source uids for scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan code interproc source uid: %w", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate code interproc source uids: %w", err)
	}
	return uids, nil
}

// ListSourceUIDsForSource returns the distinct source Function uids recorded for
// the given evidence source only.
func (s CodeInterprocProjectedEdgeStore) ListSourceUIDsForSource(
	ctx context.Context,
	evidenceSource string,
) ([]string, error) {
	if s.db == nil {
		return nil, fmt.Errorf("code interproc projected edge store database is required")
	}
	rows, err := s.db.QueryContext(ctx, listCodeInterprocSourceUIDsForSourceSQL, evidenceSource)
	if err != nil {
		return nil, fmt.Errorf("list code interproc source uids for source: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan code interproc source uid: %w", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate code interproc source uids: %w", err)
	}
	return uids, nil
}

// ListStaleSourceUIDs returns source Function uids recorded for the given scope
// whose generation is not the current generation, up to limit rows.
func (s CodeInterprocProjectedEdgeStore) ListStaleSourceUIDs(
	ctx context.Context,
	evidenceSource string,
	scopeID string,
	currentGenerationID string,
	limit int,
) ([]string, error) {
	if s.db == nil {
		return nil, fmt.Errorf("code interproc projected edge store database is required")
	}
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(
		ctx, listStaleCodeInterprocSourceUIDsSQL,
		evidenceSource, scopeID, currentGenerationID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list stale code interproc source uids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan stale code interproc source uid: %w", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stale code interproc source uids: %w", err)
	}
	return uids, nil
}

// PruneForScopes removes all ledger rows for the given evidence source and scope
// IDs.
func (s CodeInterprocProjectedEdgeStore) PruneForScopes(
	ctx context.Context,
	evidenceSource string,
	scopeIDs []string,
) error {
	if s.db == nil {
		return fmt.Errorf("code interproc projected edge store database is required")
	}
	if _, err := s.db.ExecContext(ctx, pruneCodeInterprocForScopesSQL, evidenceSource, scopeIDs); err != nil {
		return fmt.Errorf("prune code interproc projected edges for scopes: %w", err)
	}
	return nil
}

// PruneForSource removes all ledger rows for the given evidence source.
func (s CodeInterprocProjectedEdgeStore) PruneForSource(
	ctx context.Context,
	evidenceSource string,
) error {
	if s.db == nil {
		return fmt.Errorf("code interproc projected edge store database is required")
	}
	if _, err := s.db.ExecContext(ctx, pruneCodeInterprocForSourceSQL, evidenceSource); err != nil {
		return fmt.Errorf("prune code interproc projected edges for source: %w", err)
	}
	return nil
}

// PruneStale removes ledger rows for the given evidence source and scope whose
// generation is not the current generation.
func (s CodeInterprocProjectedEdgeStore) PruneStale(
	ctx context.Context,
	evidenceSource string,
	scopeID string,
	currentGenerationID string,
) error {
	if s.db == nil {
		return fmt.Errorf("code interproc projected edge store database is required")
	}
	if _, err := s.db.ExecContext(
		ctx, pruneStaleCodeInterprocSQL,
		evidenceSource, scopeID, currentGenerationID,
	); err != nil {
		return fmt.Errorf("prune stale code interproc projected edges: %w", err)
	}
	return nil
}

// LedgerHasRowsForSource returns true when at least one row exists for the
// given evidence source.
func (s CodeInterprocProjectedEdgeStore) LedgerHasRowsForSource(
	ctx context.Context,
	evidenceSource string,
) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("code interproc projected edge store database is required")
	}
	rows, err := s.db.QueryContext(ctx, codeInterprocHasRowsForSourceSQL, evidenceSource)
	if err != nil {
		return false, fmt.Errorf("check code interproc rows for source: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var exists bool
	if err := rows.Scan(&exists); err != nil {
		return false, fmt.Errorf("scan code interproc exists: %w", err)
	}
	return exists, nil
}
