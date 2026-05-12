package postgres

const storeWebhookTriggerQuery = `
INSERT INTO webhook_refresh_triggers (
    trigger_id,
    delivery_key,
    refresh_key,
    provider,
    event_kind,
    decision,
    reason,
    delivery_id,
    repository_external_id,
    repository_full_name,
    default_branch,
    ref,
    before_sha,
    target_sha,
    action,
    sender,
    status,
    received_at,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19
)
ON CONFLICT (refresh_key) DO UPDATE
SET trigger_id = EXCLUDED.trigger_id,
    delivery_key = EXCLUDED.delivery_key,
    provider = EXCLUDED.provider,
    event_kind = EXCLUDED.event_kind,
    decision = EXCLUDED.decision,
    reason = EXCLUDED.reason,
    delivery_id = EXCLUDED.delivery_id,
    repository_external_id = EXCLUDED.repository_external_id,
    repository_full_name = EXCLUDED.repository_full_name,
    default_branch = EXCLUDED.default_branch,
    ref = EXCLUDED.ref,
    before_sha = EXCLUDED.before_sha,
    target_sha = EXCLUDED.target_sha,
    action = EXCLUDED.action,
    sender = EXCLUDED.sender,
    status = CASE
        WHEN webhook_refresh_triggers.status = 'ignored' AND EXCLUDED.status = 'queued'
        THEN EXCLUDED.status
        ELSE webhook_refresh_triggers.status
    END,
    duplicate_count = webhook_refresh_triggers.duplicate_count + 1,
    updated_at = EXCLUDED.updated_at
RETURNING
    trigger_id,
    delivery_key,
    refresh_key,
    provider,
    event_kind,
    decision,
    reason,
    delivery_id,
    repository_external_id,
    repository_full_name,
    default_branch,
    ref,
    before_sha,
    target_sha,
    action,
    sender,
    status,
    duplicate_count,
    received_at,
    updated_at
`

const claimQueuedWebhookTriggersQuery = `
WITH claimed AS (
    SELECT trigger_id
    FROM webhook_refresh_triggers
    WHERE status = 'queued'
    ORDER BY received_at ASC, trigger_id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE webhook_refresh_triggers AS trigger
SET status = 'claimed',
    claimed_by = $2,
    claimed_at = $3,
    updated_at = $3
FROM claimed
WHERE trigger.trigger_id = claimed.trigger_id
RETURNING
    trigger.trigger_id,
    trigger.delivery_key,
    trigger.refresh_key,
    trigger.provider,
    trigger.event_kind,
    trigger.decision,
    trigger.reason,
    trigger.delivery_id,
    trigger.repository_external_id,
    trigger.repository_full_name,
    trigger.default_branch,
    trigger.ref,
    trigger.before_sha,
    trigger.target_sha,
    trigger.action,
    trigger.sender,
    trigger.status,
    trigger.duplicate_count,
    trigger.received_at,
    trigger.updated_at
`

const markWebhookTriggersHandedOffQueryFormat = `
UPDATE webhook_refresh_triggers
SET status = 'handed_off',
    handed_off_at = $%d,
    updated_at = $%d
WHERE trigger_id IN (%s)
  AND status = 'claimed'
`

const markWebhookTriggersFailedQueryFormat = `
UPDATE webhook_refresh_triggers
SET status = 'failed',
    failure_class = $%d,
    failure_message = $%d,
    failed_at = $%d,
    updated_at = $%d
WHERE trigger_id IN (%s)
  AND status = 'claimed'
`
