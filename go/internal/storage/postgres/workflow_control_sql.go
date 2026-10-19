// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
    tenant_id,
    workspace_id,
    subject_class,
    policy_revision_hash,
    acceptance_unit_id,
    source_run_id,
    generation_id,
    fairness_key,
    claim_priority,
    claim_ecosystem,
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

// The fair-claim statement (priority-then-rotation candidate plus the
// shared claim/complete lifecycle) lives in workflow_control_fair_claim_sql.go
// beside its mechanism proof. The lifecycle CTEs below serve the heartbeat,
// completion, and failure mutations.

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
      AND (
          item.tenant_id = ''
          OR EXISTS (
              SELECT 1
              FROM tenant_scope_grants
              JOIN tenants
                ON tenants.tenant_id = tenant_scope_grants.tenant_id
              JOIN workspaces
                ON workspaces.tenant_id = tenant_scope_grants.tenant_id
               AND workspaces.workspace_id = tenant_scope_grants.workspace_id
              WHERE tenant_scope_grants.tenant_id = item.tenant_id
                AND tenant_scope_grants.workspace_id = item.workspace_id
                AND tenant_scope_grants.scope_id = item.scope_id
                AND tenant_scope_grants.subject_class = item.subject_class
                AND tenant_scope_grants.policy_revision_hash = item.policy_revision_hash
                AND tenants.status = 'active'
                AND workspaces.status = 'active'
                AND tenants.tombstoned_at IS NULL
                AND workspaces.tombstoned_at IS NULL
                AND tenant_scope_grants.tombstoned_at IS NULL
                AND tenant_scope_grants.effective_at <= $1
                AND (tenant_scope_grants.expires_at IS NULL OR tenant_scope_grants.expires_at > $1)
              FOR SHARE OF tenant_scope_grants, tenants, workspaces
          )
      )
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
      AND (
          item.tenant_id = ''
          OR EXISTS (
              SELECT 1
              FROM tenant_scope_grants
              JOIN tenants
                ON tenants.tenant_id = tenant_scope_grants.tenant_id
              JOIN workspaces
                ON workspaces.tenant_id = tenant_scope_grants.tenant_id
               AND workspaces.workspace_id = tenant_scope_grants.workspace_id
              WHERE tenant_scope_grants.tenant_id = item.tenant_id
                AND tenant_scope_grants.workspace_id = item.workspace_id
                AND tenant_scope_grants.scope_id = item.scope_id
                AND tenant_scope_grants.subject_class = item.subject_class
                AND tenant_scope_grants.policy_revision_hash = item.policy_revision_hash
                AND tenants.status = 'active'
                AND workspaces.status = 'active'
                AND tenants.tombstoned_at IS NULL
                AND workspaces.tombstoned_at IS NULL
                AND tenant_scope_grants.tombstoned_at IS NULL
                AND tenant_scope_grants.effective_at <= $1
                AND (tenant_scope_grants.expires_at IS NULL OR tenant_scope_grants.expires_at > $1)
              FOR SHARE OF tenant_scope_grants, tenants, workspaces
          )
      )
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
    scope_id = COALESCE(NULLIF($7, ''), item.scope_id),
    acceptance_unit_id = COALESCE(NULLIF($8, ''), item.acceptance_unit_id),
    source_run_id = COALESCE(NULLIF($9, ''), item.source_run_id),
    generation_id = COALESCE(NULLIF($10, ''), item.generation_id),
    current_claim_id = NULL,
    current_owner_id = NULL,
    lease_expires_at = $2,
    last_completed_at = $1,
    updated_at = $1,
    last_failure_class = NULL,
    last_failure_message = NULL,
    last_failure_operation = NULL,
    last_failure_status_code = NULL,
    last_failure_terminal = NULL
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
        failure_operation = NULLIF($9, ''),
        failure_status_code = NULLIF($10, 0),
        failure_terminal = $11,
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
    last_failure_message = NULLIF($8, ''),
    last_failure_operation = NULLIF($9, ''),
    last_failure_status_code = NULLIF($10, 0),
    last_failure_terminal = $11
FROM candidate
WHERE item.work_item_id = candidate.work_item_id
  AND EXISTS (SELECT 1 FROM updated_claim)
`

const failWorkflowClaimTerminalQuery = `
WITH candidate AS (
    SELECT item.work_item_id
    FROM workflow_work_items AS item
    JOIN workflow_claims AS claim
      ON claim.claim_id = $4
     AND claim.work_item_id = item.work_item_id
    WHERE item.work_item_id = $5
      AND item.current_claim_id = $4
      AND item.current_fencing_token = $2
      AND item.current_owner_id = $3
      AND item.status = 'claimed'
      AND claim.fencing_token = $2
      AND claim.owner_id = $3
      AND claim.status = 'active'
    FOR UPDATE OF item, claim
),
updated_claim AS (
    UPDATE workflow_claims AS claim
    SET status = 'failed_terminal',
        finished_at = $1,
        failure_class = NULLIF($6, ''),
        failure_message = NULLIF($7, ''),
        failure_operation = NULLIF($8, ''),
        failure_status_code = NULLIF($9, 0),
        failure_terminal = $10,
        updated_at = $1
    FROM candidate
    WHERE claim.claim_id = $4
      AND claim.work_item_id = candidate.work_item_id
    RETURNING claim.claim_id
)
UPDATE workflow_work_items AS item
SET status = 'failed_terminal',
    current_claim_id = NULL,
    current_owner_id = NULL,
    lease_expires_at = NULL,
    updated_at = $1,
    last_failure_class = NULLIF($6, ''),
    last_failure_message = NULLIF($7, ''),
    last_failure_operation = NULLIF($8, ''),
    last_failure_status_code = NULLIF($9, 0),
    last_failure_terminal = $10
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
