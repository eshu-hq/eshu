package postgres

const releaseWorkflowClaimQuery = `
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
    SET status = 'released',
        finished_at = $1,
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
    lease_expires_at = $2,
    visible_at = $1,
    updated_at = $1,
    last_failure_class = NULL,
    last_failure_message = NULL
FROM candidate
WHERE item.work_item_id = candidate.work_item_id
  AND EXISTS (SELECT 1 FROM updated_claim)
`
