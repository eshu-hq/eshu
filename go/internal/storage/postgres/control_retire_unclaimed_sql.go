// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// retireUnclaimedInstanceGateQuery reports, for one (collector_kind,
// collector_instance_id) pair, whether any collector_instances row names it
// (known) and whether that row is still an enabled, non-deactivated
// configuration (active). The sanctioned never-claimed retirement
// (eshu-hq/eshu#10532) proceeds only when the instance is known and not
// active: an unknown pair is a typo to fail closed on, and an active pair
// may still have a live collector claiming its pending rows. Both answers
// come from one statement so the gate is a single round trip.
const retireUnclaimedInstanceGateQuery = `
SELECT EXISTS (
    SELECT 1
    FROM collector_instances
    WHERE instance_id = $1
      AND collector_kind = $2
) AS known,
EXISTS (
    SELECT 1
    FROM collector_instances
    WHERE instance_id = $1
      AND collector_kind = $2
      AND enabled
      AND deactivated_at IS NULL
) AS active
`

// retireUnclaimedActiveRecheckSQL rechecks the deconfiguration inside the
// retire statement. The gate probe runs first and fails fast on unknown or
// still-configured instances, but a reactivation landing between the gate
// and the retire must not retire rows under a live collector. The NOT
// EXISTS shares the statement snapshot with the row locks, so the retire
// either runs while the instance is still deconfigured (proceeds) or after
// the reactivation committed (retires nothing; the operator retries into a
// 409). $1 is the collector kind, $2 the instance id.
const retireUnclaimedActiveRecheckSQL = `AND NOT EXISTS (
      SELECT 1
      FROM collector_instances AS inst
      WHERE inst.instance_id = $2
        AND inst.collector_kind = $1
        AND inst.enabled
        AND inst.deactivated_at IS NULL
    )`

// retireUnclaimedWorkflowWorkItemsQuery moves at most 100 never-claimed
// pending rows of one deconfigured collector instance to failed_terminal
// with the operator-retired failure class (eshu-hq/eshu#10532). The shape
// mirrors the admin repository skip: the selected CTE picks up to 100
// candidates newest first without locking them; the locked CTE locks the
// rows that are still eligible in work_item_id COLLATE "C" order under
// SKIP LOCKED; the UPDATE rechecks every predicate on the locked row and
// returns what it transitioned, so the response count means "retired by
// this call" and a repeated call retires the residue or nothing. Both the
// lock and the update recheck the deconfiguration, so a reactivation that
// commits between the gate probe and this statement retires nothing.
//
// Lock order (#8261 pattern): the locked step takes row locks in
// work_item_id COLLATE "C" order, the same order every ordered locker of
// these rows uses, and the collector claim path locks at most one row per
// statement through its primary-key candidate. A claim and a retire over
// the same row therefore serialize on that row's lock instead of
// deadlocking: whichever commits first wins the row, and the loser's
// rechecked predicates (status pending, attempt_count 0, no claim versus
// the claim's pending plus identity match) drop it. Claim-versus-retire
// never both win and never deadlock. The 100-row bound matches the skip;
// a deconfigured instance holds a handful of rows, and a repeat call
// drains any residue.
const retireUnclaimedWorkflowWorkItemsQuery = `
WITH selected AS (
    SELECT work.work_item_id, work.status AS selected_status
    FROM workflow_work_items AS work
    WHERE work.collector_kind = $1
      AND work.collector_instance_id = $2
      AND work.status = 'pending'
      AND work.attempt_count = 0
      AND work.current_claim_id IS NULL
    ORDER BY work.updated_at DESC, work.work_item_id ASC
    LIMIT 100
), locked AS (
    SELECT work.work_item_id, work.scope_id, selected.selected_status
    FROM workflow_work_items AS work
    JOIN selected ON selected.work_item_id = work.work_item_id
    WHERE work.status = selected.selected_status
      AND work.status = 'pending'
      AND work.attempt_count = 0
      AND work.current_claim_id IS NULL
    ` + retireUnclaimedActiveRecheckSQL + `
    ORDER BY work.work_item_id COLLATE "C"
    FOR NO KEY UPDATE OF work SKIP LOCKED
), updated AS (
    UPDATE workflow_work_items AS work
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
    WHERE work.work_item_id = locked.work_item_id
      AND work.status = locked.selected_status
      AND work.status = 'pending'
      AND work.attempt_count = 0
      AND work.current_claim_id IS NULL
    ` + retireUnclaimedActiveRecheckSQL + `
    RETURNING
        work.work_item_id,
        work.scope_id
)
SELECT * FROM updated ORDER BY work_item_id ASC
`
