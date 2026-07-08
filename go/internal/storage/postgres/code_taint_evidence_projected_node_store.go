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
	codeTaintEvidenceProjectedNodeBatchSize = 500
	codeTaintEvidenceProjectedNodeColumns   = 5
)

// codeTaintEvidenceProjectedNodeSchemaSQL is the durable ledger of node uids of
// every projected CodeTaintEvidence node. The ledger is written before the graph
// node write so it is always a superset of graph nodes — over-inclusion is
// harmless because the anchored Cypher retract WHERE still filters;
// under-inclusion would orphan.
const codeTaintEvidenceProjectedNodeSchemaSQL = `CREATE TABLE IF NOT EXISTS code_taint_evidence_projected_node (
    evidence_source TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    node_uid TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (evidence_source, scope_id, generation_id, node_uid)
);
CREATE INDEX IF NOT EXISTS code_taint_evidence_projected_node_source_scope_idx
    ON code_taint_evidence_projected_node (evidence_source, scope_id);
CREATE INDEX IF NOT EXISTS code_taint_evidence_projected_node_source_idx
    ON code_taint_evidence_projected_node (evidence_source);
CREATE INDEX IF NOT EXISTS code_taint_evidence_projected_node_stale_idx
    ON code_taint_evidence_projected_node (evidence_source, scope_id, generation_id);
`

const upsertCodeTaintEvidenceProjectedNodeBatchPrefix = `
INSERT INTO code_taint_evidence_projected_node (
    evidence_source, scope_id, generation_id, node_uid, updated_at
) VALUES `

const upsertCodeTaintEvidenceProjectedNodeBatchSuffix = `
ON CONFLICT (evidence_source, scope_id, generation_id, node_uid) DO UPDATE
SET updated_at = EXCLUDED.updated_at
`

const listCodeTaintEvidenceNodeUIDsForScopesSQL = `
SELECT DISTINCT node_uid
FROM code_taint_evidence_projected_node
WHERE evidence_source = $1
  AND scope_id = ANY($2)
ORDER BY node_uid
`

const listStaleCodeTaintEvidenceNodeUIDsSQL = `
SELECT DISTINCT node_uid
FROM code_taint_evidence_projected_node
WHERE evidence_source = $1
  AND scope_id = $2
  AND generation_id <> $3
ORDER BY node_uid
LIMIT $4
`

const pruneCodeTaintEvidenceForScopesSQL = `
DELETE FROM code_taint_evidence_projected_node
WHERE evidence_source = $1
  AND scope_id = ANY($2)
`

const pruneStaleCodeTaintEvidenceForUIDsSQL = `
DELETE FROM code_taint_evidence_projected_node
WHERE evidence_source = $1
  AND scope_id = $2
  AND generation_id <> $3
  AND node_uid = ANY($4)
`

const codeTaintEvidenceHasRowsForSourceSQL = `
SELECT EXISTS(
    SELECT 1 FROM code_taint_evidence_projected_node WHERE evidence_source = $1
)
`

// CodeTaintEvidenceProjectedNodeSchemaSQL returns the DDL for the projected-node
// ledger.
func CodeTaintEvidenceProjectedNodeSchemaSQL() string {
	return codeTaintEvidenceProjectedNodeSchemaSQL
}

// CodeTaintEvidenceProjectedNodeStore records the uids of every projected
// CodeTaintEvidence node so retraction can enumerate uids from the ledger instead
// of scanning the whole graph.
type CodeTaintEvidenceProjectedNodeStore struct {
	db ExecQueryer
}

// NewCodeTaintEvidenceProjectedNodeStore constructs a Postgres-backed projected-node
// ledger.
func NewCodeTaintEvidenceProjectedNodeStore(db ExecQueryer) CodeTaintEvidenceProjectedNodeStore {
	return CodeTaintEvidenceProjectedNodeStore{db: db}
}

// EnsureSchema applies the projected-node ledger DDL.
func (s CodeTaintEvidenceProjectedNodeStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("code taint evidence projected node store database is required")
	}
	if _, err := s.db.ExecContext(ctx, codeTaintEvidenceProjectedNodeSchemaSQL); err != nil {
		return fmt.Errorf("ensure code taint evidence projected node schema: %w", err)
	}
	return nil
}

// RecordProjectedNodes records node uids, idempotent on the primary key. Blank
// uids are skipped and duplicate uids within a batch are de-duplicated before
// insert to avoid SQLSTATE 21000.
func (s CodeTaintEvidenceProjectedNodeStore) RecordProjectedNodes(
	ctx context.Context,
	evidenceSource string,
	scopeID string,
	generationID string,
	nodeUIDs []string,
	updatedAt time.Time,
) error {
	if s.db == nil {
		return fmt.Errorf("code taint evidence projected node store database is required")
	}
	if updatedAt.IsZero() {
		return fmt.Errorf("code taint evidence projected node updated_at is required")
	}
	seen := make(map[string]struct{}, len(nodeUIDs))
	unique := make([]string, 0, len(nodeUIDs))
	for _, uid := range nodeUIDs {
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
	for i := 0; i < len(unique); i += codeTaintEvidenceProjectedNodeBatchSize {
		end := i + codeTaintEvidenceProjectedNodeBatchSize
		if end > len(unique) {
			end = len(unique)
		}
		if err := s.upsertBatch(ctx, evidenceSource, scopeID, generationID, unique[i:end], utc); err != nil {
			return err
		}
	}
	return nil
}

func (s CodeTaintEvidenceProjectedNodeStore) upsertBatch(
	ctx context.Context,
	evidenceSource string,
	scopeID string,
	generationID string,
	uids []string,
	updatedAt time.Time,
) error {
	values := make([]string, 0, len(uids))
	args := make([]any, 0, len(uids)*codeTaintEvidenceProjectedNodeColumns)
	for _, uid := range uids {
		base := len(args)
		placeholders := make([]string, 0, codeTaintEvidenceProjectedNodeColumns)
		for j := 1; j <= codeTaintEvidenceProjectedNodeColumns; j++ {
			placeholders = append(placeholders, fmt.Sprintf("$%d", base+j))
		}
		values = append(values, "("+strings.Join(placeholders, ", ")+")")
		args = append(args, evidenceSource, scopeID, generationID, uid, updatedAt)
	}
	query := upsertCodeTaintEvidenceProjectedNodeBatchPrefix + strings.Join(values, ", ") + upsertCodeTaintEvidenceProjectedNodeBatchSuffix
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert code taint evidence projected nodes: %w", err)
	}
	return nil
}

// ListNodeUIDsForScopes returns the distinct node uids recorded for the given
// evidence source and scope IDs.
func (s CodeTaintEvidenceProjectedNodeStore) ListNodeUIDsForScopes(
	ctx context.Context,
	evidenceSource string,
	scopeIDs []string,
) ([]string, error) {
	if s.db == nil {
		return nil, fmt.Errorf("code taint evidence projected node store database is required")
	}
	rows, err := s.db.QueryContext(ctx, listCodeTaintEvidenceNodeUIDsForScopesSQL, evidenceSource, scopeIDs)
	if err != nil {
		return nil, fmt.Errorf("list code taint evidence node uids for scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan code taint evidence node uid: %w", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate code taint evidence node uids: %w", err)
	}
	return uids, nil
}

// ListStaleNodeUIDs returns node uids recorded for the given scope whose
// generation is not the current generation, up to limit rows.
func (s CodeTaintEvidenceProjectedNodeStore) ListStaleNodeUIDs(
	ctx context.Context,
	evidenceSource string,
	scopeID string,
	currentGenerationID string,
	limit int,
) ([]string, error) {
	if s.db == nil {
		return nil, fmt.Errorf("code taint evidence projected node store database is required")
	}
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(
		ctx, listStaleCodeTaintEvidenceNodeUIDsSQL,
		evidenceSource, scopeID, currentGenerationID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list stale code taint evidence node uids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan stale code taint evidence node uid: %w", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stale code taint evidence node uids: %w", err)
	}
	return uids, nil
}

// PruneForScopes removes all ledger rows for the given evidence source and scope
// IDs.
func (s CodeTaintEvidenceProjectedNodeStore) PruneForScopes(
	ctx context.Context,
	evidenceSource string,
	scopeIDs []string,
) error {
	if s.db == nil {
		return fmt.Errorf("code taint evidence projected node store database is required")
	}
	if _, err := s.db.ExecContext(ctx, pruneCodeTaintEvidenceForScopesSQL, evidenceSource, scopeIDs); err != nil {
		return fmt.Errorf("prune code taint evidence projected nodes for scopes: %w", err)
	}
	return nil
}

// PruneStaleForUIDs removes ledger rows for the given evidence source, scope,
// and stale generation whose node_uid is in the provided list. Only the uids
// that were actually retracted from the graph are pruned so a deleteLimit-bounded
// enumeration never orphans graph nodes from ledger rows beyond the batch.
func (s CodeTaintEvidenceProjectedNodeStore) PruneStaleForUIDs(
	ctx context.Context,
	evidenceSource string,
	scopeID string,
	currentGenerationID string,
	uids []string,
) error {
	if s.db == nil {
		return fmt.Errorf("code taint evidence projected node store database is required")
	}
	if len(uids) == 0 {
		return nil
	}
	if _, err := s.db.ExecContext(
		ctx, pruneStaleCodeTaintEvidenceForUIDsSQL,
		evidenceSource, scopeID, currentGenerationID, uids,
	); err != nil {
		return fmt.Errorf("prune stale code taint evidence projected nodes for uids: %w", err)
	}
	return nil
}

// LedgerHasRowsForSource returns true when at least one row exists for the
// given evidence source.
func (s CodeTaintEvidenceProjectedNodeStore) LedgerHasRowsForSource(
	ctx context.Context,
	evidenceSource string,
) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("code taint evidence projected node store database is required")
	}
	rows, err := s.db.QueryContext(ctx, codeTaintEvidenceHasRowsForSourceSQL, evidenceSource)
	if err != nil {
		return false, fmt.Errorf("check code taint evidence rows for source: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var exists bool
	if err := rows.Scan(&exists); err != nil {
		return false, fmt.Errorf("scan code taint evidence exists: %w", err)
	}
	return exists, nil
}
