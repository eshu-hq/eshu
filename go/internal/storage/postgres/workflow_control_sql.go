package postgres

const createWorkflowRunQuery = `
INSERT INTO workflow_runs (
    run_id,
    trigger_kind,
    status,
    requested_scope_set,
    requested_collector,
    created_at,
    updated_at,
    finished_at
) VALUES ($1, $2, $3, $4::jsonb, NULLIF($5, ''), $6, $7, NULLIF($8, '')::timestamptz)
ON CONFLICT (run_id) DO UPDATE
SET trigger_kind = EXCLUDED.trigger_kind,
    requested_scope_set = EXCLUDED.requested_scope_set,
    requested_collector = EXCLUDED.requested_collector,
    updated_at = EXCLUDED.updated_at
`

const enqueueWorkflowWorkItemsPrefix = `
INSERT INTO workflow_work_items (
    work_item_id,
    run_id,
    collector_kind,
    collector_instance_id,
    source_system,
    scope_id,
    acceptance_unit_id,
    source_run_id,
    generation_id,
    fairness_key,
    status,
    attempt_count,
    current_claim_id,
    current_fencing_token,
    current_owner_id,
    lease_expires_at,
    visible_at,
    last_claimed_at,
    last_completed_at,
    last_failure_class,
    last_failure_message,
    created_at,
    updated_at
) VALUES `

const enqueueWorkflowWorkItemsSuffix = `
ON CONFLICT DO NOTHING
`

// TODO(phase-2-fairness): This selector is intentionally FIFO within one
// collector family. Multi-family fairness must move into an explicit scheduler
// before this ORDER BY changes, otherwise family starvation can regress
// silently under the wrong claim model.
const claimNextWorkflowWorkItemQuery = `
WITH candidate AS (
    SELECT work_item_id
    FROM workflow_work_items
    WHERE collector_kind = $1
      AND collector_instance_id = $2
      AND status = 'pending'
      AND source_system <> ''
      AND acceptance_unit_id <> ''
      AND source_run_id <> ''
      AND generation_id <> ''
      AND (visible_at IS NULL OR visible_at <= $3)
    ORDER BY COALESCE(visible_at, created_at), created_at, work_item_id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
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
        item.acceptance_unit_id,
        item.source_run_id,
        item.generation_id,
        COALESCE(item.fairness_key, '') AS fairness_key,
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
    claimed_item.acceptance_unit_id,
    claimed_item.source_run_id,
    claimed_item.generation_id,
    claimed_item.fairness_key,
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

const heartbeatWorkflowClaimQuery = `
WITH candidate AS (
    SELECT item.work_item_id
    FROM workflow_work_items AS item
    JOIN workflow_claims AS claim
      ON claim.claim_id = $5
     AND claim.work_item_id = item.work_item_id
    WHERE item.work_item_id = $6
      AND item.current_claim_id = $5
      AND item.current_fencing_token = $3
      AND item.current_owner_id = $4
      AND item.status = 'claimed'
      AND claim.fencing_token = $3
      AND claim.owner_id = $4
      AND claim.status = 'active'
    FOR UPDATE OF item, claim
),
updated_claim AS (
    UPDATE workflow_claims AS claim
    SET heartbeat_at = $1,
        lease_expires_at = $2,
        updated_at = $1
    FROM candidate
    WHERE claim.claim_id = $5
      AND claim.work_item_id = candidate.work_item_id
    RETURNING claim.claim_id
)
UPDATE workflow_work_items AS item
SET lease_expires_at = $2,
    updated_at = $1
FROM candidate
WHERE item.work_item_id = candidate.work_item_id
  AND EXISTS (SELECT 1 FROM updated_claim)
`

const completeWorkflowClaimQuery = `
WITH candidate AS (
    SELECT item.work_item_id
    FROM workflow_work_items AS item
    JOIN workflow_claims AS claim
      ON claim.claim_id = $5
     AND claim.work_item_id = item.work_item_id
    WHERE item.work_item_id = $6
      AND item.current_claim_id = $5
      AND item.current_fencing_token = $3
      AND item.current_owner_id = $4
      AND item.status = 'claimed'
      AND claim.fencing_token = $3
      AND claim.owner_id = $4
      AND claim.status = 'active'
    FOR UPDATE OF item, claim
),
updated_claim AS (
    UPDATE workflow_claims AS claim
    SET status = 'completed',
        finished_at = $1,
        updated_at = $1
    FROM candidate
    WHERE claim.claim_id = $5
      AND claim.work_item_id = candidate.work_item_id
    RETURNING claim.claim_id
)
UPDATE workflow_work_items AS item
SET status = 'completed',
    current_claim_id = NULL,
    current_owner_id = NULL,
    lease_expires_at = $2,
    last_completed_at = $1,
    updated_at = $1,
    last_failure_class = NULL,
    last_failure_message = NULL
FROM candidate
WHERE item.work_item_id = candidate.work_item_id
  AND EXISTS (SELECT 1 FROM updated_claim)
`

const failWorkflowClaimRetryableQuery = `
WITH candidate AS (
    SELECT item.work_item_id
    FROM workflow_work_items AS item
    JOIN workflow_claims AS claim
      ON claim.claim_id = $5
     AND claim.work_item_id = item.work_item_id
    WHERE item.work_item_id = $6
      AND item.current_claim_id = $5
      AND item.current_fencing_token = $3
      AND item.current_owner_id = $4
      AND item.status = 'claimed'
      AND claim.fencing_token = $3
      AND claim.owner_id = $4
      AND claim.status = 'active'
    FOR UPDATE OF item, claim
),
updated_claim AS (
    UPDATE workflow_claims AS claim
    SET status = 'failed_retryable',
        finished_at = $1,
        failure_class = NULLIF($7, ''),
        failure_message = NULLIF($8, ''),
        updated_at = $1
    FROM candidate
    WHERE claim.claim_id = $5
      AND claim.work_item_id = candidate.work_item_id
    RETURNING claim.claim_id
)
UPDATE workflow_work_items AS item
SET status = 'pending',
    current_claim_id = NULL,
    current_owner_id = NULL,
    lease_expires_at = NULL,
    visible_at = $2,
    updated_at = $1,
    last_failure_class = NULLIF($7, ''),
    last_failure_message = NULLIF($8, '')
FROM candidate
WHERE item.work_item_id = candidate.work_item_id
  AND EXISTS (SELECT 1 FROM updated_claim)
`

const failWorkflowClaimTerminalQuery = `
WITH candidate AS (
    SELECT item.work_item_id
    FROM workflow_work_items AS item
    JOIN workflow_claims AS claim
      ON claim.claim_id = $5
     AND claim.work_item_id = item.work_item_id
    WHERE item.work_item_id = $6
      AND item.current_claim_id = $5
      AND item.current_fencing_token = $3
      AND item.current_owner_id = $4
      AND item.status = 'claimed'
      AND claim.fencing_token = $3
      AND claim.owner_id = $4
      AND claim.status = 'active'
    FOR UPDATE OF item, claim
),
updated_claim AS (
    UPDATE workflow_claims AS claim
    SET status = 'failed_terminal',
        finished_at = $1,
        failure_class = NULLIF($7, ''),
        failure_message = NULLIF($8, ''),
        updated_at = $1
    FROM candidate
    WHERE claim.claim_id = $5
      AND claim.work_item_id = candidate.work_item_id
    RETURNING claim.claim_id
)
UPDATE workflow_work_items AS item
SET status = 'failed_terminal',
    current_claim_id = NULL,
    current_owner_id = NULL,
    lease_expires_at = NULL,
    updated_at = $1,
    last_failure_class = NULLIF($7, ''),
    last_failure_message = NULLIF($8, '')
FROM candidate
WHERE item.work_item_id = candidate.work_item_id
  AND EXISTS (SELECT 1 FROM updated_claim)
`

const reapExpiredWorkflowClaimsQuery = `
WITH candidate AS (
    SELECT
        claim.claim_id,
        claim.work_item_id,
        claim.fencing_token,
        claim.owner_id,
        claim.claimed_at,
        claim.heartbeat_at,
        claim.lease_expires_at,
        claim.created_at
    FROM workflow_claims AS claim
    JOIN workflow_work_items AS item
      ON item.work_item_id = claim.work_item_id
    WHERE claim.status = 'active'
      AND claim.lease_expires_at < $1
      AND item.current_claim_id = claim.claim_id
      AND item.current_fencing_token = claim.fencing_token
      AND item.current_owner_id = claim.owner_id
      AND item.status = 'claimed'
    ORDER BY claim.lease_expires_at ASC, claim.claim_id ASC
    LIMIT $2
    FOR UPDATE OF claim, item SKIP LOCKED
),
updated_claim AS (
    UPDATE workflow_claims AS claim
    SET status = 'expired',
        finished_at = $1,
        updated_at = $1
    FROM candidate
    WHERE claim.claim_id = candidate.claim_id
    RETURNING
        candidate.claim_id,
        candidate.work_item_id,
        candidate.fencing_token,
        candidate.owner_id,
        'expired' AS status,
        candidate.claimed_at,
        candidate.heartbeat_at,
        candidate.lease_expires_at,
        candidate.created_at,
        $1 AS updated_at
)
,
updated_item AS (
UPDATE workflow_work_items AS item
SET status = 'pending',
    current_claim_id = NULL,
    current_owner_id = NULL,
    lease_expires_at = NULL,
    visible_at = $3,
    updated_at = $1
FROM candidate
WHERE item.work_item_id = candidate.work_item_id
  AND item.current_claim_id = candidate.claim_id
  AND item.current_fencing_token = candidate.fencing_token
  AND item.current_owner_id = candidate.owner_id
  AND item.status = 'claimed'
RETURNING item.work_item_id
)
SELECT
    updated_claim.claim_id,
    updated_claim.work_item_id,
    updated_claim.fencing_token,
    updated_claim.owner_id,
    updated_claim.status,
    updated_claim.claimed_at,
    updated_claim.heartbeat_at,
    updated_claim.lease_expires_at,
    updated_claim.created_at,
    updated_claim.updated_at
FROM updated_claim
JOIN updated_item ON updated_item.work_item_id = updated_claim.work_item_id
`
