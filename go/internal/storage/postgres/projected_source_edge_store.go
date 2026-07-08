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
	projectedSourceEdgeBatchSize = 500
	projectedSourceEdgeColumns   = 5
)

// projectedSourceEdgeSchemaSQL is the durable ledger of source-node uids of
// every projected edge, generic across evidence sources. The ledger is
// written before the graph edge write so it is always a superset of graph
// edges — over-inclusion is harmless because the Cypher retract WHERE still
// filters; under-inclusion would orphan.
const projectedSourceEdgeSchemaSQL = `CREATE TABLE IF NOT EXISTS projected_source_edge (
    evidence_source TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    source_uid TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (evidence_source, scope_id, generation_id, source_uid)
);
CREATE INDEX IF NOT EXISTS projected_source_edge_source_scope_idx
    ON projected_source_edge (evidence_source, scope_id);
CREATE INDEX IF NOT EXISTS projected_source_edge_source_idx
    ON projected_source_edge (evidence_source);
CREATE INDEX IF NOT EXISTS projected_source_edge_stale_idx
    ON projected_source_edge (evidence_source, scope_id, generation_id);
`

const upsertProjectedSourceEdgeBatchPrefix = `
INSERT INTO projected_source_edge (
    evidence_source, scope_id, generation_id, source_uid, updated_at
) VALUES `

const upsertProjectedSourceEdgeBatchSuffix = `
ON CONFLICT (evidence_source, scope_id, generation_id, source_uid) DO UPDATE
SET updated_at = EXCLUDED.updated_at
`

const listProjectedSourceUIDsForScopesSQL = `
SELECT DISTINCT source_uid
FROM projected_source_edge
WHERE evidence_source = $1
  AND scope_id = ANY($2)
ORDER BY source_uid
`

const pruneProjectedSourceEdgeForScopesSQL = `
DELETE FROM projected_source_edge
WHERE evidence_source = $1
  AND scope_id = ANY($2)
`

// ProjectedSourceEdgeSchemaSQL returns the DDL for the projected-source-edge
// ledger.
func ProjectedSourceEdgeSchemaSQL() string {
	return projectedSourceEdgeSchemaSQL
}

// ProjectedSourceEdgeStore records the source-node uids of every projected
// edge for a given evidence source, so reducer edge retracts can enumerate
// prior-generation source uids from the ledger instead of scanning the whole
// graph. This is the generalized counterpart of
// CodeInterprocProjectedEdgeStore: the same superset-ledger pattern, keyed by
// an arbitrary evidence_source rather than a single hardcoded edge kind.
type ProjectedSourceEdgeStore struct {
	db ExecQueryer
}

// NewProjectedSourceEdgeStore constructs a Postgres-backed projected-source-edge
// ledger.
func NewProjectedSourceEdgeStore(db ExecQueryer) ProjectedSourceEdgeStore {
	return ProjectedSourceEdgeStore{db: db}
}

// EnsureSchema applies the projected-source-edge ledger DDL.
func (s ProjectedSourceEdgeStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("projected source edge store database is required")
	}
	if _, err := s.db.ExecContext(ctx, projectedSourceEdgeSchemaSQL); err != nil {
		return fmt.Errorf("ensure projected source edge schema: %w", err)
	}
	return nil
}

// RecordProjectedSources records source-node uids for the given evidence
// source, scope, and generation, idempotent on the primary key. Blank uids
// are skipped and duplicate uids within a batch are de-duplicated before
// insert to avoid SQLSTATE 21000. Callers MUST write to this ledger before
// the corresponding graph edge write completes, so the ledger stays a
// superset of the graph: over-inclusion here is harmless because a
// retraction's Cypher WHERE still filters against the live graph, but
// under-inclusion would let a retract miss a source uid and orphan a graph
// edge.
func (s ProjectedSourceEdgeStore) RecordProjectedSources(
	ctx context.Context,
	evidenceSource string,
	scopeID string,
	generationID string,
	sourceUIDs []string,
	updatedAt time.Time,
) error {
	if s.db == nil {
		return fmt.Errorf("projected source edge store database is required")
	}
	if updatedAt.IsZero() {
		return fmt.Errorf("projected source edge updated_at is required")
	}
	seen := make(map[string]struct{}, len(sourceUIDs))
	unique := make([]string, 0, len(sourceUIDs))
	for _, uid := range sourceUIDs {
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
	for i := 0; i < len(unique); i += projectedSourceEdgeBatchSize {
		end := i + projectedSourceEdgeBatchSize
		if end > len(unique) {
			end = len(unique)
		}
		if err := s.upsertBatch(ctx, evidenceSource, scopeID, generationID, unique[i:end], utc); err != nil {
			return err
		}
	}
	return nil
}

func (s ProjectedSourceEdgeStore) upsertBatch(
	ctx context.Context,
	evidenceSource string,
	scopeID string,
	generationID string,
	uids []string,
	updatedAt time.Time,
) error {
	values := make([]string, 0, len(uids))
	args := make([]any, 0, len(uids)*projectedSourceEdgeColumns)
	for _, uid := range uids {
		base := len(args)
		placeholders := make([]string, 0, projectedSourceEdgeColumns)
		for j := 1; j <= projectedSourceEdgeColumns; j++ {
			placeholders = append(placeholders, fmt.Sprintf("$%d", base+j))
		}
		values = append(values, "("+strings.Join(placeholders, ", ")+")")
		args = append(args, evidenceSource, scopeID, generationID, uid, updatedAt)
	}
	query := upsertProjectedSourceEdgeBatchPrefix + strings.Join(values, ", ") + upsertProjectedSourceEdgeBatchSuffix
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert projected source edges: %w", err)
	}
	return nil
}

// ListSourceUIDsForScopes returns the distinct source-node uids recorded for
// the given evidence source and scope IDs. Results from one evidence source
// never leak into another.
func (s ProjectedSourceEdgeStore) ListSourceUIDsForScopes(
	ctx context.Context,
	evidenceSource string,
	scopeIDs []string,
) ([]string, error) {
	if s.db == nil {
		return nil, fmt.Errorf("projected source edge store database is required")
	}
	rows, err := s.db.QueryContext(ctx, listProjectedSourceUIDsForScopesSQL, evidenceSource, scopeIDs)
	if err != nil {
		return nil, fmt.Errorf("list projected source uids for scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan projected source uid: %w", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projected source uids: %w", err)
	}
	return uids, nil
}

// PruneForScopes removes all ledger rows for the given evidence source and
// scope IDs.
func (s ProjectedSourceEdgeStore) PruneForScopes(
	ctx context.Context,
	evidenceSource string,
	scopeIDs []string,
) error {
	if s.db == nil {
		return fmt.Errorf("projected source edge store database is required")
	}
	if _, err := s.db.ExecContext(ctx, pruneProjectedSourceEdgeForScopesSQL, evidenceSource, scopeIDs); err != nil {
		return fmt.Errorf("prune projected source edges for scopes: %w", err)
	}
	return nil
}
