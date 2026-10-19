// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Priority-then-rotation fair claim selector (#8469 step 3).
//
// The candidate serves the lowest priority tier first (ClaimPriority ASC:
// configured_direct before owned before broad), which is the owner's stated
// cross-tier precedence, and rotates uniformly across the fairness keys
// inside that tier. Rotation is a per-claim lottery over the per-key heads,
// not a deterministic round robin: a deterministic stateless rotation is
// impossible in a single re-evaluated statement (any pure function of the
// current pending set that served key A last still serves key A's next head
// whenever that head wins the static order, which skewed arrivals arrange),
// and the stateful alternatives fail harder — a shared in-DB cursor races
// across concurrent claimers or serializes them on one hot row, and a
// per-key deficit counter makes a key's position depend on its own claim
// history. The lottery draws independently per claim, so concurrent
// claimers need no shared state and SKIP LOCKED resolves the occasional
// same-head collision without blocking.
//
// Honest limits, stated so no reader overclaims them:
//
//   - The overtaking bound is probabilistic, not deterministic: with K
//     eligible keys a key waits past N claims with probability
//     ((K-1)/K)^N. Rotation within a tier holds in expectation and is
//     measured so, not proved for every interleaving.
//   - Priority is ordered first, so head-of-line blocking relocates across
//     tiers: a burst at priority 0 still precedes small keys at priority 1.
//     Rotation only rotates within a tier.
//   - Keyless rows (NULL fairness_key) share one rotation slot keyed ”.
//
// The draw salt arrives as $7. Callers that need reproducible picks (tests)
// pass a fixed salt; production callers pass ” and the statement draws
// server-side randomness per head row, which keeps every concurrent claim
// an independent shuffle. An empty salt must never degrade to a static
// order, so the NULLIF/COALESCE falls back to random(), never to ”.
//
// Cost scales with the distinct (priority, key) count, not the backlog: a
// loose scan walks the pairs in index order and each pair probes its oldest
// head, all served by workflow_work_items_fair_claim_idx. The visibility
// OR stays a residual filter throughout, as in step 1. The final pick
// walks the drawn heads in order and locks the first unlocked one through
// its primary key via LATERAL, so the lock path is a bounded number of
// point probes at every scale: a plain join lets the planner choose a
// backlog-wide scan plus hash join at small cardinalities.
//
// The draw set holds every eligible key's head: no key is excluded or
// deprioritized by its own claim history, and position depends only on the
// declared priority and the current eligible set. When the single drawn
// head is momentarily locked by a rival claimer the attempt returns empty
// and the collector's normal poll retry absorbs it. The retry rate is
// governed by concurrent claim statements over draw slots: under the
// barrier-synchronized back-to-back depth drain (eight claimers, five
// keys) most attempts collide and retry, while handler-separated
// production claims barely overlap. The harness measures the worst case
// openly; depth pacing (#8470) relieves it by spacing claims.
const claimNextWorkflowWorkItemQuery = `
WITH RECURSIVE keys AS (
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
        ORDER BY COALESCE(i.visible_at, i.created_at), i.created_at, i.work_item_id LIMIT 1) AS work_item_id
    FROM keys k
),
ordered AS (
    SELECT work_item_id, claim_priority,
        encode(sha256(convert_to(fairness_key || COALESCE(NULLIF($7, ''), random()::text), 'UTF8')), 'hex') AS draw
    FROM heads WHERE work_item_id IS NOT NULL
),
-- Selection is lock-free; acquisition locks exactly one row. An earlier
-- shape probed every drawn head with FOR UPDATE inside a LATERAL, which
-- locked each free head it touched rather than only the winner: one claim
-- could briefly hold every head, saturating the draw set for rivals and
-- turning most attempts into wasted full scans. The scalar pick below
-- locks a single primary-key row; a rival that commits the same head in
-- the microsecond between pick and lock yields one empty attempt, which
-- the collector's normal poll retry absorbs.
pick AS (
    SELECT work_item_id FROM ordered
    ORDER BY claim_priority, draw
    LIMIT 1
),
candidate AS (
    SELECT item.work_item_id
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
    FOR UPDATE OF item SKIP LOCKED
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
    FROM candidate
    WHERE item.work_item_id = candidate.work_item_id
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
