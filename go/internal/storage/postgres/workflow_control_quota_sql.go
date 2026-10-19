// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Quota-gated claim selector (#8471).
//
// This statement is claimNextWorkflowWorkItemQuery (priority-then-rotation
// fair claim, #8469 step 3) plus the per-(component, scope) generation
// quota (#8471): $8 is the enforced generations-per-hour limit per scope
// row, and the statement grants only when it reserves budget in the same
// row. A component with items in N scopes can therefore grant up to N times
// the limit per hour; this partitioning keeps an exhausted scope from
// head-of-line blocking the component's other scopes. The plain
// statement is untouched and stays the path for quota-less components and
// for the unreadable-quota floor fallback.
//
// Atomicity contract (the check-then-grant bug this exists to prevent):
//   - quota_full steers SELECTION away from scopes already at budget, so
//     other scopes of the same component keep flowing instead of
//     head-of-line blocking behind an exhausted head. Its reads are
//     snapshot-local and may be stale; it is liveness only, never the
//     gate.
//   - reserve_quota is the gate. The conditional upsert (INSERT, else
//     UPDATE ... WHERE grants < $8) serializes concurrent replicas on the
//     usage row: the loser of the row lock sees the winner's committed
//     count in its EvalPlanQual recheck, its WHERE fails, it returns no
//     row, and claimed_item (which joins the reservation) grants nothing.
//     No overshoot by construction, at any claimant count.
//   - Lock order is item row (candidate FOR UPDATE) then usage row
//     (reserve upsert), identical for every claimer; reap and complete
//     never touch usage rows, so no lock cycle exists.
//   - The window is a database-side hour bucket (date_trunc on the
//     statement clock), so HA coordinators with skewed wall clocks share
//     one window. $3 (coordinator now) still drives visibility and lease
//     exactly as in the plain statement; it never defines the window.
//   - prune_quota deletes buckets older than the previous hour in the same
//     statement. It touches only elapsed rows, never the row being
//     reserved, so it cannot contend with the reservation.
//
// No new index: the usage table holds at most two buckets per active
// (component, scope), so the quota_full scan and the reserve point lookup
// are served by the primary key.
const claimNextWorkflowWorkItemQuotaQuery = `
WITH RECURSIVE quota_window AS (
    SELECT date_trunc('hour', now()) AS window_start
),
quota_full AS (
    SELECT w.scope_id
    FROM component_quota_windows AS w
    WHERE w.component = $1
      AND w.window_start = (SELECT quota_window.window_start FROM quota_window)
      AND w.grants >= $8
),
keys AS (
    (SELECT claim_priority, COALESCE(fairness_key, '') AS fairness_key
    FROM workflow_work_items
    WHERE collector_kind = $1
      AND collector_instance_id = $2
      AND status = 'pending'
      AND source_system <> ''
      AND acceptance_unit_id <> ''
      AND source_run_id <> ''
      AND generation_id <> ''
      AND (visible_at IS NULL OR visible_at <= $3)
      AND NOT EXISTS (SELECT 1 FROM quota_full WHERE quota_full.scope_id = workflow_work_items.scope_id)
    ORDER BY claim_priority, COALESCE(fairness_key, '') LIMIT 1)
    UNION ALL
    (SELECT s.claim_priority, s.fairness_key FROM keys k, LATERAL (
        SELECT i.claim_priority, COALESCE(i.fairness_key, '') AS fairness_key
        FROM workflow_work_items AS i
        WHERE i.collector_kind = $1
          AND i.collector_instance_id = $2
          AND i.status = 'pending'
          AND i.source_system <> ''
          AND i.acceptance_unit_id <> ''
          AND i.source_run_id <> ''
          AND i.generation_id <> ''
          AND (i.visible_at IS NULL OR i.visible_at <= $3)
          AND (i.claim_priority, COALESCE(i.fairness_key, '')) > (k.claim_priority, k.fairness_key)
          AND NOT EXISTS (SELECT 1 FROM quota_full WHERE quota_full.scope_id = i.scope_id)
        ORDER BY i.claim_priority, COALESCE(i.fairness_key, '') LIMIT 1) s)
),
heads AS (
    SELECT k.claim_priority, k.fairness_key,
        (SELECT i.work_item_id
        FROM workflow_work_items AS i
        WHERE i.collector_kind = $1
          AND i.collector_instance_id = $2
          AND i.status = 'pending'
          AND i.source_system <> ''
          AND i.acceptance_unit_id <> ''
          AND i.source_run_id <> ''
          AND i.generation_id <> ''
          AND (i.visible_at IS NULL OR i.visible_at <= $3)
          AND i.claim_priority = k.claim_priority
          AND COALESCE(i.fairness_key, '') = k.fairness_key
          AND NOT EXISTS (SELECT 1 FROM quota_full WHERE quota_full.scope_id = i.scope_id)
        ORDER BY COALESCE(i.visible_at, i.created_at), i.created_at, i.work_item_id LIMIT 1) AS work_item_id
    FROM keys k
),
ordered AS (
    SELECT work_item_id, claim_priority,
        encode(sha256(convert_to(fairness_key || COALESCE(NULLIF($7, ''), random()::text), 'UTF8')), 'hex') AS draw
    FROM heads WHERE work_item_id IS NOT NULL
),
pick AS (
    SELECT work_item_id FROM ordered
    ORDER BY claim_priority, draw
    LIMIT 1
),
candidate AS (
    SELECT item.work_item_id, item.scope_id
    FROM workflow_work_items AS item
    WHERE item.work_item_id = (SELECT work_item_id FROM pick)
      AND item.collector_kind = $1
      AND item.collector_instance_id = $2
      AND item.status = 'pending'
      AND item.source_system <> ''
      AND item.acceptance_unit_id <> ''
      AND item.source_run_id <> ''
      AND item.generation_id <> ''
      AND (item.visible_at IS NULL OR item.visible_at <= $3)
      AND NOT EXISTS (SELECT 1 FROM quota_full WHERE quota_full.scope_id = item.scope_id)
    FOR UPDATE OF item SKIP LOCKED
),
reserve_quota AS (
    INSERT INTO component_quota_windows AS w (component, scope_id, window_start, grants, updated_at)
    SELECT $1, candidate.scope_id, quota_window.window_start, 1, now()
    FROM candidate, quota_window
    ON CONFLICT (component, scope_id, window_start) DO UPDATE
    SET grants = w.grants + 1,
        updated_at = now()
    WHERE w.grants < $8
    RETURNING w.scope_id
),
prune_quota AS (
    DELETE FROM component_quota_windows AS w
    USING quota_window
    WHERE w.window_start < quota_window.window_start - INTERVAL '1 hour'
),
claimed_item AS (
    UPDATE workflow_work_items AS item
    SET status = 'claimed',
        attempt_count = item.attempt_count + 1,
        current_claim_id = $5,
        current_fencing_token = item.current_fencing_token + 1,
        current_owner_id = $4,
        lease_expires_at = $6,
        last_claimed_at = $3,
        updated_at = $3
    FROM candidate, reserve_quota
    WHERE item.work_item_id = candidate.work_item_id
      AND candidate.scope_id = reserve_quota.scope_id
    RETURNING
        item.work_item_id,
        item.run_id,
        item.collector_kind,
        item.collector_instance_id,
        item.source_system,
        item.scope_id,
        item.tenant_id,
        item.workspace_id,
        item.subject_class,
        item.policy_revision_hash,
        item.acceptance_unit_id,
        item.source_run_id,
        item.generation_id,
        COALESCE(item.fairness_key, '') AS fairness_key,
        item.claim_priority,
        COALESCE(item.claim_ecosystem, '') AS claim_ecosystem,
        item.status,
        item.attempt_count,
        COALESCE(item.current_claim_id, '') AS current_claim_id,
        item.current_fencing_token,
        COALESCE(item.current_owner_id, '') AS current_owner_id,
        item.lease_expires_at,
        item.created_at,
        item.updated_at
),
inserted_claim AS (
    INSERT INTO workflow_claims (
        claim_id,
        work_item_id,
        fencing_token,
        owner_id,
        status,
        claimed_at,
        heartbeat_at,
        lease_expires_at,
        created_at,
        updated_at
    )
    SELECT
        $5,
        work_item_id,
        current_fencing_token,
        $4,
        'active',
        $3,
        $3,
        $6,
        $3,
        $3
    FROM claimed_item
    RETURNING
        work_item_id,
        claim_id,
        fencing_token,
        owner_id,
        status,
        claimed_at,
        heartbeat_at,
        lease_expires_at,
        created_at,
        updated_at
)
SELECT
    claimed_item.work_item_id,
    claimed_item.run_id,
    claimed_item.collector_kind,
    claimed_item.collector_instance_id,
    claimed_item.source_system,
    claimed_item.scope_id,
    claimed_item.tenant_id,
    claimed_item.workspace_id,
    claimed_item.subject_class,
    claimed_item.policy_revision_hash,
    claimed_item.acceptance_unit_id,
    claimed_item.source_run_id,
    claimed_item.generation_id,
    claimed_item.fairness_key,
    claimed_item.claim_priority,
    claimed_item.claim_ecosystem,
    claimed_item.status,
    claimed_item.attempt_count,
    claimed_item.current_claim_id,
    claimed_item.current_fencing_token,
    claimed_item.current_owner_id,
    claimed_item.lease_expires_at,
    claimed_item.created_at,
    claimed_item.updated_at,
    inserted_claim.claim_id,
    inserted_claim.fencing_token,
    inserted_claim.owner_id,
    inserted_claim.status,
    inserted_claim.claimed_at,
    inserted_claim.heartbeat_at,
    inserted_claim.lease_expires_at,
    inserted_claim.created_at,
    inserted_claim.updated_at
FROM claimed_item
JOIN inserted_claim ON inserted_claim.work_item_id = claimed_item.work_item_id
`

// quotaThrottleProbeQuery distinguishes "no work" from "work exists but the
// quota blocked it" after a quota-gated claim returns empty. It runs only on
// the empty path, so the hot grant path pays nothing, and its answer feeds
// the quota_exceeded counter only — never admission, so its snapshot timing
// cannot affect correctness.
const quotaThrottleProbeQuery = `
SELECT
    EXISTS(
        SELECT 1 FROM workflow_work_items
        WHERE collector_kind = $1
          AND collector_instance_id = $2
          AND status = 'pending'
          AND source_system <> ''
          AND acceptance_unit_id <> ''
          AND source_run_id <> ''
          AND generation_id <> ''
          AND (visible_at IS NULL OR visible_at <= $3)
    ) AS has_work,
    EXISTS(
        SELECT 1 FROM workflow_work_items AS item
        JOIN component_quota_windows AS w
          ON w.component = $1
         AND w.scope_id = item.scope_id
         AND w.window_start = date_trunc('hour', now())
        WHERE item.collector_kind = $1
          AND item.collector_instance_id = $2
          AND item.status = 'pending'
          AND item.source_system <> ''
          AND item.acceptance_unit_id <> ''
          AND item.source_run_id <> ''
          AND item.generation_id <> ''
          AND (item.visible_at IS NULL OR item.visible_at <= $3)
          AND w.grants >= $4
    ) AS throttled
`
