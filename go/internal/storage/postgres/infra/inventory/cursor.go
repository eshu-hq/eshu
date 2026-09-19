// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// reconcileWalkName keys the one persisted walk cursor. Every reducer replica
// resumes from and advances the same row.
const reconcileWalkName = "infra_resource_entities"

const loadCursorSQL = `
SELECT cursor FROM infra_resource_entity_reconcile_cursor WHERE walk_name = $1`

const saveCursorSQL = `
INSERT INTO infra_resource_entity_reconcile_cursor (walk_name, cursor, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (walk_name) DO UPDATE
SET cursor = EXCLUDED.cursor, updated_at = EXCLUDED.updated_at`

// LoadCursor returns the persisted reconcile walk cursor: the repo_id the last
// cycle stopped after, or "" when no cycle has stored one or the last cycle
// reached the end of the walk. It is a primary-key lookup, so a process that
// starts its walk never enumerates the corpus to pick where to begin.
func LoadCursor(ctx context.Context, queryer db.Queryer) (string, error) {
	rows, err := queryer.QueryContext(ctx, loadCursorSQL, reconcileWalkName)
	if err != nil {
		return "", fmt.Errorf("load infra inventory reconcile cursor: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var cursor string
	if rows.Next() {
		if err := rows.Scan(&cursor); err != nil {
			return "", fmt.Errorf("scan infra inventory reconcile cursor: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("load infra inventory reconcile cursor: %w", err)
	}
	return cursor, nil
}

// SaveCursor persists the walk cursor. Replicas overwrite each other's value,
// which is safe: each writes a position it reached by walking forward from a
// stored one, so a resumed walk is at most one page behind the furthest
// replica and still advances every cycle, however often processes restart.
func SaveCursor(ctx context.Context, executor db.Executor, cursor string) error {
	if _, err := executor.ExecContext(ctx, saveCursorSQL, reconcileWalkName, cursor); err != nil {
		return fmt.Errorf("save infra inventory reconcile cursor: %w", err)
	}
	return nil
}
