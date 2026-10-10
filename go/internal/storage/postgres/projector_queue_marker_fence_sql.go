// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// lockProjectorMarkerFenceQuery is the first statement of the #7389
// write-start marker's transaction (#7819). It locks the scope's
// projector_scope_claim_fences row non-blocking, before the marker UPDATE
// locks the generation row. A claim holds that fence row while its candidate
// step runs, so the marker commit and a claim's fence lock serialize: when
// the fence is busy the marker defers (ErrWorkWriteMarkerDeferred, re-run by
// the caller) instead of committing a marker the claim's snapshot cannot see,
// which closes the skip-blind split where the sweep spared a marked retry
// while the same statement claimed the newer row.
//
// NO KEY UPDATE matches the lock the bump UPDATE below takes. SKIP LOCKED
// keeps the marker out of every wait cycle: the marker already waits on the
// generation row under lock_timeout, and waiting on the fence too would let
// it join a cycle with a claim holding the fence and pulling generations.
// A missing row means the scope is gone (the trigger creates the fence with
// the scope and the scope delete cascades to it), so the marker tells the two
// apart with the existence read below and routes a missing row to
// classifyWriteMarkerRefusal at once instead of spinning the deferral bound.
//
// $1 scope.
const lockProjectorMarkerFenceQuery = `
SELECT fence
FROM projector_scope_claim_fences
WHERE scope_id = $1
FOR NO KEY UPDATE SKIP LOCKED
`

// fenceProjectorMarkerFenceExistsQuery tells a busy fence row from a missing
// one after the lock statement finds nothing (#7907). It takes no lock, so it
// never waits.
//
// $1 scope.
const fenceProjectorMarkerFenceExistsQuery = `
SELECT EXISTS (
    SELECT 1
    FROM projector_scope_claim_fences
    WHERE scope_id = $1
)
`

// checkMarkerClaimFence locks the scope's claim fence row for the write-start
// marker. It reports whether the lock landed and whether the row exists: a
// row that exists but could not lock is busy (SKIP LOCKED) and the marker
// defers, while a missing row means the scope is gone and the marker refuses
// through the classifier instead of spinning the deferral bound. Neither read
// waits.
func checkMarkerClaimFence(ctx context.Context, tx db.Queryer, scopeID string) (locked, exists bool, err error) {
	fenceRows, err := tx.QueryContext(ctx, lockProjectorMarkerFenceQuery, scopeID)
	if err != nil {
		return false, false, fmt.Errorf("mark projection write started: lock claim fence: %w", err)
	}
	locked = fenceRows.Next()
	if err := fenceRows.Err(); err != nil {
		_ = fenceRows.Close()
		return false, false, fmt.Errorf("mark projection write started: lock claim fence: %w", err)
	}
	_ = fenceRows.Close()
	if locked {
		return true, true, nil
	}
	existsRows, err := tx.QueryContext(ctx, fenceProjectorMarkerFenceExistsQuery, scopeID)
	if err != nil {
		return false, false, fmt.Errorf("mark projection write started: check claim fence: %w", err)
	}
	if existsRows.Next() {
		if err := existsRows.Scan(&exists); err != nil {
			_ = existsRows.Close()
			return false, false, fmt.Errorf("mark projection write started: check claim fence: scan: %w", err)
		}
	}
	if err := existsRows.Err(); err != nil {
		_ = existsRows.Close()
		return false, false, fmt.Errorf("mark projection write started: check claim fence: %w", err)
	}
	_ = existsRows.Close()
	return false, exists, nil
}

// bumpProjectorMarkerFenceQuery is the last statement of the marker's
// transaction, run only when the marker set. The bump tells a claim whose
// snapshot predates the marker to drop its candidate through the #7115 fence
// recheck and re-read, so the snapshot-only hold guards see the marker on
// the next attempt. It updates only the row the lock statement already holds,
// so it never waits. A refused marker bumps nothing: nothing was written for
// a concurrent claim to re-read.
//
// $1 scope.
const bumpProjectorMarkerFenceQuery = `
UPDATE projector_scope_claim_fences
SET fence = fence + 1
WHERE scope_id = $1
`
