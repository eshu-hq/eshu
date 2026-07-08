// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"
)

// codeValueFlowBackfillStateSchemaSQL is the durable completion marker table so
// value-flow backfillers detect whether a source was fully backfilled on a
// previous run rather than relying on the presence of any ledger row (which a
// partial-backfill write before a later error would leave behind).
const codeValueFlowBackfillStateSchemaSQL = `CREATE TABLE IF NOT EXISTS code_value_flow_backfill_state (
    backfill_key TEXT PRIMARY KEY,
    completed_at TIMESTAMPTZ NOT NULL
);
`

const isCodeValueFlowBackfillCompleteSQL = `
SELECT EXISTS(
    SELECT 1 FROM code_value_flow_backfill_state WHERE backfill_key = $1
)
`

const markCodeValueFlowBackfillCompleteSQL = `
INSERT INTO code_value_flow_backfill_state (backfill_key, completed_at)
VALUES ($1, $2)
ON CONFLICT (backfill_key) DO NOTHING
`

// CodeValueFlowBackfillStateSchemaSQL returns the DDL for the backfill-state
// marker table.
func CodeValueFlowBackfillStateSchemaSQL() string {
	return codeValueFlowBackfillStateSchemaSQL
}

// CodeValueFlowBackfillStateStore provides durable per-source completion markers
// for value-flow ledger backfills so a partially failed backfill re-runs on the
// next startup instead of being treated as done.
type CodeValueFlowBackfillStateStore struct {
	db ExecQueryer
}

// NewCodeValueFlowBackfillStateStore constructs a Postgres-backed backfill-state
// marker store.
func NewCodeValueFlowBackfillStateStore(db ExecQueryer) CodeValueFlowBackfillStateStore {
	return CodeValueFlowBackfillStateStore{db: db}
}

// EnsureSchema applies the backfill-state marker DDL.
func (s CodeValueFlowBackfillStateStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("code value flow backfill state store database is required")
	}
	if _, err := s.db.ExecContext(ctx, codeValueFlowBackfillStateSchemaSQL); err != nil {
		return fmt.Errorf("ensure code value flow backfill state schema: %w", err)
	}
	return nil
}

// IsComplete returns true when the backfill key has been marked complete.
func (s CodeValueFlowBackfillStateStore) IsComplete(ctx context.Context, key string) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("code value flow backfill state store database is required")
	}
	rows, err := s.db.QueryContext(ctx, isCodeValueFlowBackfillCompleteSQL, key)
	if err != nil {
		return false, fmt.Errorf("check code value flow backfill complete: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var exists bool
	if err := rows.Scan(&exists); err != nil {
		return false, fmt.Errorf("scan code value flow backfill exists: %w", err)
	}
	return exists, nil
}

// MarkComplete records the backfill key as complete, idempotent via ON CONFLICT
// DO NOTHING.
func (s CodeValueFlowBackfillStateStore) MarkComplete(ctx context.Context, key string, at time.Time) error {
	if s.db == nil {
		return fmt.Errorf("code value flow backfill state store database is required")
	}
	if _, err := s.db.ExecContext(ctx, markCodeValueFlowBackfillCompleteSQL, key, at.UTC()); err != nil {
		return fmt.Errorf("mark code value flow backfill complete: %w", err)
	}
	return nil
}
