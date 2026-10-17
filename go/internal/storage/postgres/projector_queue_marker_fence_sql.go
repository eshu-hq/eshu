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

// supersedeProjectorObsoleteGenerationsQuery is Ack's obsolete-generation
// supersede. It folds the failure a failed, dead-lettered or retried row carried
// into failure_details.prior_failure (#7320); failure_class and failure_message
// stay the supersede marker. See priorFailureStaleSQL. #7469: it spares a
// generation that already started writing (projection_write_started_at IS
// NULL), like the claim sweep's first branch, so a marked generation Acked
// past is left for its retry, replay, or the graph_dirty full snapshot.
// #7820: the spare reads the marker at lock time, not from the statement
// snapshot. The lock step takes each stale generation row non-blocking in
// generation order, so a marker committed after the snapshot is caught by
// the lock's EvalPlanQual recheck and a marker still in flight is skipped
// without waiting (waiting would only reach the lock timeout and defer the
// whole Ack). The work UPDATE then retires only rows whose generation the
// lock step holds, re-applying the generation predicate for the recheck.
// #7187: the lock step takes the scope's claim fence first, then each stale
// generation row, both SKIP LOCKED. The fence is a claim-only target with no
// FK children, so locking it never walks an update chain; a scope whose
// fence the claim sweep, another Ack, or the write-start marker holds
// contributes no rows, and the maintenance is deferred to the next claim or
// Ack instead of waiting on an open generation update (40P01 with a
// concurrent claim).
// #8261: locked_obsolete_work then locks the held generations' projector
// rows in work_item_id COLLATE "C" order (FOR NO KEY UPDATE, waiting) before
// the work UPDATE touches any. That is the order the recovery and admin
// replays lock in (#8205). Without it the UPDATE locked them in generation_id
// order under the database's default collation; where that order and C
// order disagree (en_US.utf8 and generation ids differing in case or
// punctuation), Ack and a projector-stage replay over the same dead letters
// could lock them in opposite orders and deadlock (40P01). The lock step's
// row predicates are rechecked on the newest row version when a lock waits
// (EvalPlanQual); the UPDATE repeats them with the generation guards.
const supersedeProjectorObsoleteGenerationsQuery = `
WITH locked_obsolete_stale_generations AS (
    SELECT stale_generation.generation_id
    FROM scope_generations AS stale_generation,
         scope_generations AS current_generation,
         projector_scope_claim_fences AS obsolete_fence
    WHERE obsolete_fence.scope_id = $2
      AND stale_generation.scope_id = $2
      AND stale_generation.generation_id <> $3
      AND stale_generation.status IN ('pending', 'failed')
      AND stale_generation.projection_write_started_at IS NULL
      AND current_generation.scope_id = $2
      AND current_generation.generation_id = $3
      AND current_generation.status IN ('pending', 'active')
      AND (
          stale_generation.ingested_at < current_generation.ingested_at
          OR (
              stale_generation.ingested_at = current_generation.ingested_at
              AND stale_generation.generation_id < current_generation.generation_id
          )
      )
    ORDER BY stale_generation.generation_id
    FOR NO KEY UPDATE OF obsolete_fence SKIP LOCKED
    FOR NO KEY UPDATE OF stale_generation SKIP LOCKED
),
locked_obsolete_work AS (
    SELECT stale.work_item_id
    FROM fact_work_items AS stale
    JOIN locked_obsolete_stale_generations AS locked
      ON locked.generation_id = stale.generation_id
    WHERE stale.stage = 'projector'
      AND stale.scope_id = $2
      AND stale.generation_id <> $3
      AND stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')
    ORDER BY stale.work_item_id COLLATE "C"
    FOR NO KEY UPDATE OF stale
),
superseded_work AS (
    UPDATE fact_work_items AS stale
    SET status = 'superseded',
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = NULL,
        next_attempt_at = NULL,
        updated_at = $1,
        failure_class = 'projector_superseded_by_newer_generation',
        failure_message = 'projector work superseded by newer same-scope generation',
        failure_details = jsonb_build_object(
            'scope_id', stale.scope_id,
            'work_item_id', stale.work_item_id,
            'generation_id', stale.generation_id,
            'current_generation_id', $3
        ) || ` + priorFailureStaleSQL + `
    FROM locked_obsolete_stale_generations AS locked,
         scope_generations AS stale_generation,
         scope_generations AS current_generation
    WHERE stale.work_item_id = ANY(ARRAY(SELECT work_item_id FROM locked_obsolete_work))
      AND stale.stage = 'projector'
      AND stale.scope_id = $2
      AND stale.generation_id <> $3
      AND stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')
      AND stale.generation_id = locked.generation_id
      AND stale_generation.scope_id = stale.scope_id
      AND stale_generation.generation_id = locked.generation_id
      AND stale_generation.status IN ('pending', 'failed')
      AND stale_generation.projection_write_started_at IS NULL
      AND current_generation.scope_id = stale.scope_id
      AND current_generation.generation_id = $3
      AND current_generation.status IN ('pending', 'active')
      AND (
          stale_generation.ingested_at < current_generation.ingested_at
          OR (
              stale_generation.ingested_at = current_generation.ingested_at
              AND stale_generation.generation_id < current_generation.generation_id
          )
      )
    RETURNING stale.work_item_id, stale.generation_id
)
UPDATE scope_generations AS generation
SET status = 'superseded',
    superseded_at = $1
FROM superseded_work
WHERE generation.generation_id = superseded_work.generation_id
  AND generation.status IN ('pending', 'failed')
`
