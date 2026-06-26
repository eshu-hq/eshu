// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

const (
	functionGraphIDBatchSize = 500
	functionGraphIDColumns   = 4
)

// functionGraphIDSchemaSQL is the durable FunctionID->graph-uid map (#2931). One
// row per function: the generation-independent FunctionID and the graph Function
// node uid the collector resolved, so the cross-repo fixpoint can project findings
// as TAINT_FLOWS_TO edges by uid without re-resolving names.
const functionGraphIDSchemaSQL = `
CREATE TABLE IF NOT EXISTS function_graph_ids (
    function_id TEXT PRIMARY KEY,
    uid TEXT NOT NULL,
    repo TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS function_graph_ids_repo_idx
    ON function_graph_ids (repo, function_id);

CREATE INDEX IF NOT EXISTS function_graph_ids_uid_idx
    ON function_graph_ids (uid);
`

const upsertFunctionGraphIDBatchPrefix = `
INSERT INTO function_graph_ids (
    function_id, uid, repo, updated_at
) VALUES `

const upsertFunctionGraphIDBatchSuffix = `
ON CONFLICT (function_id) DO UPDATE
SET uid = EXCLUDED.uid,
    repo = EXCLUDED.repo,
    updated_at = EXCLUDED.updated_at
WHERE function_graph_ids.updated_at <= EXCLUDED.updated_at
`

const deleteFunctionGraphIDsForRepoSQL = `
DELETE FROM function_graph_ids
WHERE repo = $1
  AND updated_at <= $2
`

const loadFunctionGraphIDsSQL = `
SELECT function_id, uid
FROM function_graph_ids
ORDER BY function_id ASC
`

// FunctionGraphIDSchemaSQL returns the DDL for the FunctionID->uid map.
func FunctionGraphIDSchemaSQL() string {
	return functionGraphIDSchemaSQL
}

// FunctionGraphIDStore persists the FunctionID->graph-uid map for the cross-repo
// fixpoint.
type FunctionGraphIDStore struct {
	db ExecQueryer
}

// NewFunctionGraphIDStore constructs a Postgres-backed FunctionID->uid store.
func NewFunctionGraphIDStore(db ExecQueryer) FunctionGraphIDStore {
	return FunctionGraphIDStore{db: db}
}

// EnsureSchema applies the FunctionID->uid map DDL.
func (s FunctionGraphIDStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("function graph id store database is required")
	}
	if _, err := s.db.ExecContext(ctx, functionGraphIDSchemaSQL); err != nil {
		return fmt.Errorf("ensure function graph id schema: %w", err)
	}
	return nil
}

// UpsertGraphIDs persists each FunctionID->uid mapping, idempotent on FunctionID.
// Mappings with an empty uid are skipped (an unresolved function has no node).
func (s FunctionGraphIDStore) UpsertGraphIDs(ctx context.Context, ids map[summary.FunctionID]string, updatedAt time.Time) error {
	if s.db == nil {
		return fmt.Errorf("function graph id store database is required")
	}
	if updatedAt.IsZero() {
		return fmt.Errorf("function graph id updated_at is required")
	}
	functionIDs := make([]summary.FunctionID, 0, len(ids))
	for id, uid := range ids {
		if strings.TrimSpace(string(id)) == "" || strings.TrimSpace(uid) == "" {
			continue
		}
		functionIDs = append(functionIDs, id)
	}
	if len(functionIDs) == 0 {
		return nil
	}
	sort.Slice(functionIDs, func(i, j int) bool { return functionIDs[i] < functionIDs[j] })
	for i := 0; i < len(functionIDs); i += functionGraphIDBatchSize {
		end := i + functionGraphIDBatchSize
		if end > len(functionIDs) {
			end = len(functionIDs)
		}
		if err := s.upsertBatch(ctx, functionIDs[i:end], ids, updatedAt.UTC()); err != nil {
			return err
		}
	}
	return nil
}

// ReplaceGraphIDs replaces one repository's complete FunctionID->uid snapshot.
// Empty or unresolved uids are meaningful: they retract stale mappings for
// functions that no longer resolve to graph Function nodes.
func (s FunctionGraphIDStore) ReplaceGraphIDs(
	ctx context.Context,
	repo string,
	ids map[summary.FunctionID]string,
	updatedAt time.Time,
) error {
	if s.db == nil {
		return fmt.Errorf("function graph id store database is required")
	}
	if updatedAt.IsZero() {
		return fmt.Errorf("function graph id updated_at is required")
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return fmt.Errorf("function graph id repo is required")
	}
	if beginner, ok := s.db.(Beginner); ok {
		tx, err := beginner.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin function graph id replacement transaction: %w", err)
		}
		defer func() { _ = tx.Rollback() }()
		if err := replaceFunctionGraphIDs(ctx, tx, repo, ids, updatedAt.UTC()); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit function graph id replacement transaction: %w", err)
		}
		return nil
	}
	return replaceFunctionGraphIDs(ctx, s.db, repo, ids, updatedAt.UTC())
}

func replaceFunctionGraphIDs(
	ctx context.Context,
	db ExecQueryer,
	repo string,
	ids map[summary.FunctionID]string,
	updatedAt time.Time,
) error {
	for id := range ids {
		functionID := strings.TrimSpace(string(id))
		if functionID == "" {
			return fmt.Errorf("function graph id is required")
		}
		if got := functionIDRepo(functionID); got != repo {
			return fmt.Errorf("function graph id repo %q does not match replacement repo %q", got, repo)
		}
	}
	if _, err := db.ExecContext(ctx, deleteFunctionGraphIDsForRepoSQL, repo, updatedAt); err != nil {
		return fmt.Errorf("delete stale function graph ids for repo %q: %w", repo, err)
	}
	if len(ids) == 0 {
		return nil
	}
	store := FunctionGraphIDStore{db: db}
	return store.upsertResolvedGraphIDs(ctx, ids, updatedAt)
}

func (s FunctionGraphIDStore) upsertResolvedGraphIDs(
	ctx context.Context,
	ids map[summary.FunctionID]string,
	updatedAt time.Time,
) error {
	functionIDs := make([]summary.FunctionID, 0, len(ids))
	for id, uid := range ids {
		if strings.TrimSpace(string(id)) == "" || strings.TrimSpace(uid) == "" {
			continue
		}
		functionIDs = append(functionIDs, id)
	}
	if len(functionIDs) == 0 {
		return nil
	}
	sort.Slice(functionIDs, func(i, j int) bool { return functionIDs[i] < functionIDs[j] })
	for i := 0; i < len(functionIDs); i += functionGraphIDBatchSize {
		end := i + functionGraphIDBatchSize
		if end > len(functionIDs) {
			end = len(functionIDs)
		}
		if err := s.upsertBatch(ctx, functionIDs[i:end], ids, updatedAt); err != nil {
			return err
		}
	}
	return nil
}

func (s FunctionGraphIDStore) upsertBatch(ctx context.Context, functionIDs []summary.FunctionID, ids map[summary.FunctionID]string, updatedAt time.Time) error {
	values := make([]string, 0, len(functionIDs))
	args := make([]any, 0, len(functionIDs)*functionGraphIDColumns)
	for _, id := range functionIDs {
		base := len(args)
		placeholders := make([]string, 0, functionGraphIDColumns)
		for i := 1; i <= functionGraphIDColumns; i++ {
			placeholders = append(placeholders, fmt.Sprintf("$%d", base+i))
		}
		values = append(values, "("+strings.Join(placeholders, ", ")+")")
		args = append(args, string(id), ids[id], functionIDRepo(string(id)), updatedAt)
	}
	query := upsertFunctionGraphIDBatchPrefix + strings.Join(values, ", ") + upsertFunctionGraphIDBatchSuffix
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert function graph ids: %w", err)
	}
	return nil
}

// LoadGraphIDs reloads the full FunctionID->uid map for the cross-repo fixpoint.
func (s FunctionGraphIDStore) LoadGraphIDs(ctx context.Context) (map[summary.FunctionID]string, error) {
	if s.db == nil {
		return nil, fmt.Errorf("function graph id store database is required")
	}
	rows, err := s.db.QueryContext(ctx, loadFunctionGraphIDsSQL)
	if err != nil {
		return nil, fmt.Errorf("load function graph ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	ids := make(map[summary.FunctionID]string)
	for rows.Next() {
		var functionID, uid string
		if err := rows.Scan(&functionID, &uid); err != nil {
			return nil, fmt.Errorf("scan function graph id: %w", err)
		}
		ids[summary.FunctionID(functionID)] = uid
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load function graph ids: %w", err)
	}
	return ids, nil
}
