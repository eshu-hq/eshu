// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// expireStaleOpenRunsQuery terminal-fails pending and claimed work items of
// non-terminal runs older than the cutoff (eshu-hq/eshu#10791). The shape
// mirrors the sanctioned never-claimed retirement: the selected CTE picks
// up to expireStaleOpenRunsLimit candidates newest first without locking
// them; the locked CTE locks the rows that are still eligible in
// work_item_id COLLATE "C" order under SKIP LOCKED and rechecks every
// predicate on the locked row (EvalPlanQual recheck under Read Committed);
// the updates recheck again and return what they transitioned, so the
// response count means "expired by this call" and a repeated call is a
// safe no-op.
//
// Lock order (#8261 pattern): the locked step takes the item row lock in
// work_item_id COLLATE "C" order, the same order every ordered locker of
// these rows uses, and the claim path locks at most one row per statement
// through its primary-key candidate. A claim and an expiry over the same
// row therefore serialize on the item row's lock instead of deadlocking:
// whichever commits first wins the row, and the loser's rechecked
// predicates (the item's pending/claimed status plus claim identity match
// for the claim path, the run-age and item-status rechecks here) drop it.
// The claim row itself is not locked — the claim finish rechecks
// claim.status = 'active' in the same snapshot, and any concurrent claim
// holder either holds the item lock (expiry skips the row) or blocks on it
// (its own recheck drops the row after expiry commits) — exactly one
// winner, never both, never neither without a terminal row to show for it.
//
// Rows whose claim already finished elsewhere are skipped: such shapes
// cannot arise through the paired writers, which transition item and claim
// atomically and clear current_claim_id on reap.
const expireStaleOpenRunsQuery = `
WITH selected AS (
    SELECT item.work_item_id, item.status AS selected_status, item.current_claim_id AS selected_claim
    FROM workflow_work_items AS item
    JOIN workflow_runs AS run ON run.run_id = item.run_id
    WHERE run.status NOT IN ('complete', 'failed')
      AND run.created_at < $1
      AND item.status IN ('pending', 'claimed')
    ORDER BY item.updated_at DESC, item.work_item_id ASC
    LIMIT $2
), locked AS (
    SELECT item.work_item_id, item.scope_id, item.run_id, selected.selected_status, selected.selected_claim
    FROM workflow_work_items AS item
    JOIN selected ON selected.work_item_id = item.work_item_id
    JOIN workflow_runs AS run ON run.run_id = item.run_id
    LEFT JOIN workflow_claims AS claim
      ON claim.claim_id = item.current_claim_id
     AND claim.work_item_id = item.work_item_id
    WHERE item.status = selected.selected_status
      AND item.status IN ('pending', 'claimed')
      AND (item.current_claim_id IS NOT DISTINCT FROM selected.selected_claim)
      AND run.status NOT IN ('complete', 'failed')
      AND run.created_at < $1
      AND (claim.claim_id IS NULL OR claim.status = 'active')
    ORDER BY item.work_item_id COLLATE "C"
    FOR NO KEY UPDATE OF item SKIP LOCKED
), updated_claim AS (
    UPDATE workflow_claims AS claim
    SET status = 'failed_terminal',
        finished_at = $3,
        failure_class = $4,
        failure_message = $5,
        failure_operation = $6,
        failure_terminal = TRUE,
        updated_at = $3
    FROM locked
    WHERE claim.claim_id = locked.selected_claim
      AND locked.selected_status = 'claimed'
      AND claim.status = 'active'
    RETURNING claim.claim_id, claim.work_item_id
), updated AS (
    UPDATE workflow_work_items AS item
    SET status = 'failed_terminal',
        current_claim_id = NULL,
        current_owner_id = NULL,
        lease_expires_at = NULL,
        updated_at = $3,
        last_failure_class = $4,
        last_failure_message = $5,
        last_failure_operation = $6,
        last_failure_terminal = TRUE
    FROM locked
    WHERE item.work_item_id = locked.work_item_id
      AND item.status = locked.selected_status
      AND item.status IN ('pending', 'claimed')
      AND (locked.selected_status = 'pending'
           OR EXISTS (SELECT 1 FROM updated_claim WHERE updated_claim.work_item_id = locked.work_item_id))
    RETURNING item.work_item_id, item.scope_id, item.run_id
)
SELECT work_item_id, scope_id, run_id FROM updated ORDER BY work_item_id
`
