// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhookstore

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
    pull_request_number,
    pull_request_url,
    pull_request_title,
    status,
    received_at,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
    $21, $22
)
ON CONFLICT (refresh_key) DO UPDATE
SET trigger_id = EXCLUDED.trigger_id,
    delivery_key = EXCLUDED.delivery_key,
    provider = EXCLUDED.provider,
    event_kind = CASE
        WHEN EXCLUDED.pull_request_url <> ''
        THEN EXCLUDED.event_kind
        ELSE webhook_refresh_triggers.event_kind
    END,
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
    pull_request_number = COALESCE(NULLIF(EXCLUDED.pull_request_number, ''), webhook_refresh_triggers.pull_request_number),
    pull_request_url = COALESCE(NULLIF(EXCLUDED.pull_request_url, ''), webhook_refresh_triggers.pull_request_url),
    pull_request_title = COALESCE(NULLIF(EXCLUDED.pull_request_title, ''), webhook_refresh_triggers.pull_request_title),
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
    pull_request_number,
    pull_request_url,
    pull_request_title,
    status,
    duplicate_count,
    received_at,
    updated_at,
    claim_fencing_token
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
    claim_fencing_token = trigger.claim_fencing_token + 1,
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
    trigger.pull_request_number,
    trigger.pull_request_url,
    trigger.pull_request_title,
    trigger.status,
    trigger.duplicate_count,
    trigger.received_at,
    trigger.updated_at,
    trigger.claim_fencing_token
`

// reapStaleWebhookTriggerClaimsQuery requeues up to $3 claimed rows whose
// claimed_at predates $1 and whose fencing token is below the $2 attempt
// cap, the #7661 lease recovery for an ingester that died mid-handoff.
// The token doubles as the attempt counter: every claim bumps it, so a
// token of N means N claims already ran. FOR UPDATE SKIP LOCKED lets two
// reclaimers race without double-reaping; the partial
// webhook_refresh_triggers_claimed_at_idx serves the predicate. The reap
// deliberately does not bump the token — the next claim does — and clears
// the dead holder's identity so the row reads as never-claimed.
const reapStaleWebhookTriggerClaimsQuery = `
WITH stale AS (
    SELECT trigger_id
    FROM webhook_refresh_triggers
    WHERE status = 'claimed'
      AND claimed_at < $1
      AND claim_fencing_token < $2
    ORDER BY claimed_at ASC, trigger_id ASC
    LIMIT $3
    FOR UPDATE SKIP LOCKED
)
UPDATE webhook_refresh_triggers AS trigger
SET status = 'queued',
    claimed_by = NULL,
    claimed_at = NULL,
    updated_at = $4
FROM stale
WHERE trigger.trigger_id = stale.trigger_id
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
    trigger.pull_request_number,
    trigger.pull_request_url,
    trigger.pull_request_title,
    trigger.status,
    trigger.duplicate_count,
    trigger.received_at,
    trigger.updated_at,
    trigger.claim_fencing_token
`

// exhaustStaleWebhookTriggerClaimsQuery fails up to $3 claimed rows whose
// claimed_at predates $1 and whose fencing token reached the $2 attempt
// cap: a poison row that every owner fails must land in failed with a
// reason instead of looping through the lease forever (#7661).
const exhaustStaleWebhookTriggerClaimsQuery = `
WITH stale AS (
    SELECT trigger_id
    FROM webhook_refresh_triggers
    WHERE status = 'claimed'
      AND claimed_at < $1
      AND claim_fencing_token >= $2
    ORDER BY claimed_at ASC, trigger_id ASC
    LIMIT $3
    FOR UPDATE SKIP LOCKED
)
UPDATE webhook_refresh_triggers AS trigger
SET status = 'failed',
    failure_class = 'claim_lease_exhausted',
    failure_message = 'claim lease expired after the maximum claim attempts',
    failed_at = $4,
    updated_at = $4
FROM stale
WHERE trigger.trigger_id = stale.trigger_id
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
    trigger.pull_request_number,
    trigger.pull_request_url,
    trigger.pull_request_title,
    trigger.status,
    trigger.duplicate_count,
    trigger.received_at,
    trigger.updated_at,
    trigger.claim_fencing_token
`

// countStaleWebhookTriggerClaimsQuery counts the rows stuck in claimed
// past $1: the stuck-claim gauge's source (#7661). It reads the partial
// webhook_refresh_triggers_claimed_at_idx, not the table.
const countStaleWebhookTriggerClaimsQuery = `
SELECT COUNT(*)
FROM webhook_refresh_triggers
WHERE status = 'claimed'
  AND claimed_at < $1
`

// markWebhookTriggersHandedOffQueryFormat completes only rows whose fencing
// token still matches: a holder whose lease expired and was reaped affects
// zero rows when it finishes late, instead of completing the new owner's
// claim (#7661). Callers pass (trigger_id, fencing_token) VALUES pairs.
const markWebhookTriggersHandedOffQueryFormat = `
UPDATE webhook_refresh_triggers AS trigger
SET status = 'handed_off',
    handed_off_at = $%d,
    updated_at = $%d
FROM (VALUES %s) AS fenced(trigger_id, fencing_token)
WHERE trigger.trigger_id = fenced.trigger_id
  AND trigger.claim_fencing_token = fenced.fencing_token
  AND trigger.status = 'claimed'
`

// markWebhookTriggersFailedQueryFormat is
// markWebhookTriggersHandedOffQueryFormat's failure-path counterpart; see
// that constant's doc comment for the fencing rationale (#7661).
const markWebhookTriggersFailedQueryFormat = `
UPDATE webhook_refresh_triggers AS trigger
SET status = 'failed',
    failure_class = $%d,
    failure_message = $%d,
    failed_at = $%d,
    updated_at = $%d
FROM (VALUES %s) AS fenced(trigger_id, fencing_token)
WHERE trigger.trigger_id = fenced.trigger_id
  AND trigger.claim_fencing_token = fenced.fencing_token
  AND trigger.status = 'claimed'
`
