// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// reconcileWalkName keys the one persisted walk cursor. Every reducer replica
// claims its pages from the same row.
const reconcileWalkName = "infra_resource_entities"

const loadCursorSQL = `
SELECT cursor FROM infra_resource_entity_reconcile_cursor WHERE walk_name = $1`

// ensureCursorSQL creates the cursor row once so the claim can lock it.
const ensureCursorSQL = `
INSERT INTO infra_resource_entity_reconcile_cursor (walk_name, cursor, updated_at)
VALUES ($1, '', now())
ON CONFLICT (walk_name) DO NOTHING`

// lockCursorSQL reads the cursor under its row lock; a concurrent claim waits
// here until this claim has stored the next position.
const lockCursorSQL = `
SELECT cursor FROM infra_resource_entity_reconcile_cursor WHERE walk_name = $1 FOR UPDATE`

const advanceCursorSQL = `
UPDATE infra_resource_entity_reconcile_cursor
SET cursor = $2, updated_at = now()
WHERE walk_name = $1`

// LoadCursor returns the persisted reconcile walk cursor: the repo_id the last
// claimed page ended at, or "" when no page was claimed yet or the last claim
// reached the end of the walk.
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

// ClaimPage claims the next page of up to budget repositories from the shared
// walk and advances the stored cursor past it, in one short transaction that
// holds the cursor's row lock. Replicas that claim at the same moment
// serialize on that lock and take disjoint pages, so N replicas walk the
// corpus N times faster instead of re-checking the same pages. A page shorter
// than budget reached the end: the cursor is reset to "" and the next claim
// starts the walk over. It returns the page and the stored next cursor.
func ClaimPage(ctx context.Context, database db.ExecQueryer, budget int) (page []string, next string, err error) {
	beginner, ok := database.(db.Beginner)
	if !ok {
		return nil, "", errors.New("infra inventory reconcile claim: database must support transactions")
	}
	if _, err := database.ExecContext(ctx, ensureCursorSQL, reconcileWalkName); err != nil {
		return nil, "", fmt.Errorf("create infra inventory reconcile cursor: %w", err)
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("begin infra inventory reconcile claim: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	rows, err := tx.QueryContext(ctx, lockCursorSQL, reconcileWalkName)
	if err != nil {
		return nil, "", fmt.Errorf("lock infra inventory reconcile cursor: %w", err)
	}
	var cursor string
	if rows.Next() {
		err = rows.Scan(&cursor)
	}
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, "", fmt.Errorf("lock infra inventory reconcile cursor: %w", err)
	}
	page, err = reconcileRepositories(ctx, tx, cursor, budget)
	if err != nil {
		return nil, "", err
	}
	if len(page) == budget {
		next = page[len(page)-1]
	}
	if _, err = tx.ExecContext(ctx, advanceCursorSQL, reconcileWalkName, next); err != nil {
		return nil, "", fmt.Errorf("advance infra inventory reconcile cursor: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("commit infra inventory reconcile claim: %w", err)
	}
	return page, next, nil
}
