// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Lease-mirror CTE fragments for #10879 phase 1 (dual-write cutover).
//
// Every reducer writer that creates or extends liveness maintains the
// fact_work_item_leases mirror in the same statement, with the main row
// locked first and the lease row second (the global main-then-lease lock
// order). Phase 1 keeps the main columns authoritative: no reader changes,
// so behavior is unchanged and the mirror can be verified row-for-row
// before phase 2 switches reads and narrows heartbeat.
//
// A data-modifying CTE executes exactly once even when the outer query does
// not read its output, so the terminal chains below append the clear as an
// unreferenced CTE without changing their outer SELECT shape (and therefore
// their RowsAffected contract under ExecContext).

// queryTerminalMatchCount runs a phase-1 terminal statement (a WITH
// done/cleared chain ending in SELECT (SELECT count(*) FROM done)) and
// reports the main-table match count. Success depends only on the fence
// matching, never on mirror presence, so hand-inserted live rows and
// version-skewed writers terminate exactly like claimed ones; the mirror
// delete stays in-statement and atomic.
func queryTerminalMatchCount(
	ctx context.Context,
	database db.ExecQueryer,
	op string,
	query string,
	args ...any,
) (int64, error) {
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	defer func() { _ = rows.Close() }()
	var matched int64
	if rows.Next() {
		if err := rows.Scan(&matched); err != nil {
			return 0, fmt.Errorf("%s: scan match count: %w", op, err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	return matched, nil
}

// reducerQueueLeaseMirrorCTE inserts (or refreshes, on reclaim) the mirror
// row for the just-claimed rows. It is appended after the claimed CTE of a
// claim statement, which supplies $3 (owner), $4 (claim_until) and the
// per-row claimed_at (the new last_attempt_at). The join back to
// fact_work_items carries the denormalized conflict columns; those rows are
// already locked by the claim UPDATE, so the join cannot block.
const reducerQueueLeaseMirrorCTE = `leased AS (
    INSERT INTO fact_work_item_leases (
        work_item_id, stage, scope_id, conflict_domain, conflict_key,
        lease_owner, claim_until, last_attempt_at
    )
    SELECT claimed.work_item_id, 'reducer', claimed.scope_id,
        work.conflict_domain, work.conflict_key,
        $3, $4, claimed.claimed_at
    FROM claimed
    JOIN fact_work_items AS work ON work.work_item_id = claimed.work_item_id
    ON CONFLICT (work_item_id) DO UPDATE SET
        scope_id = EXCLUDED.scope_id,
        conflict_domain = EXCLUDED.conflict_domain,
        conflict_key = EXCLUDED.conflict_key,
        lease_owner = EXCLUDED.lease_owner,
        claim_until = EXCLUDED.claim_until,
        last_attempt_at = EXCLUDED.last_attempt_at
)`

// reducerQueueLeaseClearCTE deletes the mirror rows for a terminal
// statement's acknowledged CTE. Every terminal chain names that CTE
// acknowledged and returns work_item_id from it.
const reducerQueueLeaseClearCTE = `cleared_leases AS (
    DELETE FROM fact_work_item_leases AS lease
    USING acknowledged
    WHERE lease.work_item_id = acknowledged.work_item_id
)`
