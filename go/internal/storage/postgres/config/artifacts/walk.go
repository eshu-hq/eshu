// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// walkName keys the one persisted walker cursor. Every reducer replica claims
// its pages from the same row.
const walkName = "config_artifact_paths"

const loadCursorSQL = `
SELECT cursor FROM config_artifact_reconcile_cursor WHERE walk_name = $1`

// ensureCursorSQL creates the cursor row once so a claim can lock it.
const ensureCursorSQL = `
INSERT INTO config_artifact_reconcile_cursor (walk_name, cursor, updated_at)
VALUES ($1, '', now())
ON CONFLICT (walk_name) DO NOTHING`

// lockCursorSQL reads the cursor under its row lock. A concurrent claim waits
// here until this claim has stored the next position.
const lockCursorSQL = `
SELECT cursor FROM config_artifact_reconcile_cursor WHERE walk_name = $1 FOR UPDATE`

// walkPageSQL is the walker's universe: one row per repository in
// config_artifact_content_versions, which the PR3b-1b seed fills and the
// content_files trigger keeps current. The primary key serves the range scan,
// so a page costs O(budget), never a scan of content_files.
const walkPageSQL = `
SELECT repo_id FROM config_artifact_content_versions
WHERE repo_id > $1
ORDER BY repo_id
LIMIT $2`

const advanceCursorSQL = `
UPDATE config_artifact_reconcile_cursor
SET cursor = $2, updated_at = now()
WHERE walk_name = $1`

// LoadCursor returns the persisted walker cursor: the repo_id the last claimed
// page ended at, or "" when no page was claimed yet or the last claim reached
// the end of the walk.
func LoadCursor(ctx context.Context, queryer db.Queryer) (string, error) {
	rows, err := queryer.QueryContext(ctx, loadCursorSQL, walkName)
	if err != nil {
		return "", fmt.Errorf("load config artifact walk cursor: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var cursor string
	if rows.Next() {
		if err := rows.Scan(&cursor); err != nil {
			return "", fmt.Errorf("scan config artifact walk cursor: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("load config artifact walk cursor: %w", err)
	}
	return cursor, nil
}

// ClaimPage claims the next page of up to budget repositories from the shared
// walk and advances the stored cursor past it, in one short transaction that
// holds the cursor's row lock. Replicas that claim at the same moment
// serialize on that lock and take disjoint pages. A page shorter than budget
// reached the end: the cursor is reset to "" and the next claim starts over.
// It returns the page and the stored next cursor.
//
// The claim is not a lease: it holds no lock on the repositories it returns.
// Two walkers can still derive one repository (a page claimed again after a
// wrap, or a content Write's own derive). The derive protocol's advisory lock
// and advance-only write make that safe; the claim only spreads the work.
func ClaimPage(ctx context.Context, database db.ExecQueryer, budget int) (page []string, next string, err error) {
	if budget <= 0 {
		return nil, "", fmt.Errorf("config artifact walk claim: budget must be positive, got %d", budget)
	}
	beginner, ok := database.(db.Beginner)
	if !ok {
		return nil, "", errors.New("config artifact walk claim: database must support transactions")
	}
	if _, err := database.ExecContext(ctx, ensureCursorSQL, walkName); err != nil {
		return nil, "", fmt.Errorf("create config artifact walk cursor: %w", err)
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("begin config artifact walk claim: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	cursor, err := scanOneString(tx.QueryContext(ctx, lockCursorSQL, walkName))
	if err != nil {
		return nil, "", fmt.Errorf("lock config artifact walk cursor: %w", err)
	}
	page, err = scanStrings(tx.QueryContext(ctx, walkPageSQL, cursor, budget))
	if err != nil {
		return nil, "", fmt.Errorf("list config artifact walk page: %w", err)
	}
	if len(page) == budget {
		next = page[len(page)-1]
	}
	if _, err = tx.ExecContext(ctx, advanceCursorSQL, walkName, next); err != nil {
		return nil, "", fmt.Errorf("advance config artifact walk cursor: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("commit config artifact walk claim: %w", err)
	}
	return page, next, nil
}

// scanStrings reads a one-column result into a slice.
func scanStrings(rows db.Rows, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

// scanOneString reads the first value of a one-column result, or "" when the
// result is empty.
func scanOneString(rows db.Rows, err error) (string, error) {
	values, err := scanStrings(rows, err)
	if err != nil || len(values) == 0 {
		return "", err
	}
	return values[0], nil
}
