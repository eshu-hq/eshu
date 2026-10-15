// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TryLockGenerations takes the per-generation advisory lock that Drain's
// batches take, pg_try_advisory_xact_lock(LockClass, hashtext(generation_id)),
// for each id inside the caller's transaction q, and returns the ids it got.
// The retention prune calls it before it re-locks its batch, so a pre-delete
// batch (primary-key order) and the prune's foreign-key cascade (physical
// order) never delete the same generation's rows at once and cannot deadlock.
// It never waits: an id another session holds is left out, and the caller
// skips that generation for the pass. The locks last until q ends.
func TryLockGenerations(ctx context.Context, q db.Queryer, generationIDs []string) (map[string]bool, error) {
	locked := make(map[string]bool, len(generationIDs))
	if len(generationIDs) == 0 {
		return locked, nil
	}
	rows, err := q.QueryContext(ctx, tryLockGenerationsQuery, generationIDs)
	if err != nil {
		return nil, fmt.Errorf("search index pre-delete: try lock generations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var generationID string
		if err := rows.Scan(&generationID); err != nil {
			return nil, fmt.Errorf("search index pre-delete: scan locked generation: %w", err)
		}
		locked[generationID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search index pre-delete: try lock generations: %w", err)
	}
	return locked, nil
}
