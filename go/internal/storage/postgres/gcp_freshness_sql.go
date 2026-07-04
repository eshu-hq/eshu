// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const storeGCPFreshnessTriggerQuery = `
INSERT INTO gcp_freshness_triggers (
    trigger_id,
    delivery_key,
    freshness_key,
    event_kind,
    event_id,
    parent_scope_kind,
    parent_scope_id,
    asset_type,
    location,
    status,
    observed_at,
    received_at,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
ON CONFLICT (freshness_key) DO UPDATE
SET trigger_id = CASE
        WHEN gcp_freshness_triggers.status = 'claimed'
        THEN gcp_freshness_triggers.trigger_id
        ELSE EXCLUDED.trigger_id
    END,
    delivery_key = EXCLUDED.delivery_key,
    event_kind = EXCLUDED.event_kind,
    event_id = EXCLUDED.event_id,
    parent_scope_kind = EXCLUDED.parent_scope_kind,
    parent_scope_id = EXCLUDED.parent_scope_id,
    asset_type = EXCLUDED.asset_type,
    location = EXCLUDED.location,
    status = CASE
        WHEN gcp_freshness_triggers.status = 'claimed'
        THEN gcp_freshness_triggers.status
        ELSE EXCLUDED.status
    END,
    duplicate_count = gcp_freshness_triggers.duplicate_count + 1,
    observed_at = EXCLUDED.observed_at,
    claimed_by = CASE
        WHEN gcp_freshness_triggers.status = 'claimed'
        THEN gcp_freshness_triggers.claimed_by
        ELSE NULL
    END,
    claimed_at = CASE
        WHEN gcp_freshness_triggers.status = 'claimed'
        THEN gcp_freshness_triggers.claimed_at
        ELSE NULL
    END,
    handed_off_at = CASE
        WHEN gcp_freshness_triggers.status = 'claimed'
        THEN gcp_freshness_triggers.handed_off_at
        ELSE NULL
    END,
    failed_at = CASE
        WHEN gcp_freshness_triggers.status = 'claimed'
        THEN gcp_freshness_triggers.failed_at
        ELSE NULL
    END,
    failure_class = CASE
        WHEN gcp_freshness_triggers.status = 'claimed'
        THEN gcp_freshness_triggers.failure_class
        ELSE NULL
    END,
    failure_message = CASE
        WHEN gcp_freshness_triggers.status = 'claimed'
        THEN gcp_freshness_triggers.failure_message
        ELSE NULL
    END,
    updated_at = EXCLUDED.updated_at
RETURNING
    trigger_id,
    delivery_key,
    freshness_key,
    event_kind,
    event_id,
    parent_scope_kind,
    parent_scope_id,
    asset_type,
    location,
    status,
    duplicate_count,
    observed_at,
    received_at,
    updated_at,
    claim_fencing_token
`

// claimQueuedGCPFreshnessTriggersQuery mirrors
// claimQueuedAWSFreshnessTriggersQuery; see that constant's doc comment for
// the claim_fencing_token rationale (#4576).
const claimQueuedGCPFreshnessTriggersQuery = `
WITH claimed AS (
    SELECT trigger_id
    FROM gcp_freshness_triggers
    WHERE status = 'queued'
    ORDER BY received_at ASC, trigger_id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE gcp_freshness_triggers AS trigger
SET status = 'claimed',
    claimed_by = $2,
    claimed_at = $3,
    claim_expires_at = $4,
    claim_fencing_token = trigger.claim_fencing_token + 1,
    updated_at = $3
FROM claimed
WHERE trigger.trigger_id = claimed.trigger_id
RETURNING
    trigger.trigger_id,
    trigger.delivery_key,
    trigger.freshness_key,
    trigger.event_kind,
    trigger.event_id,
    trigger.parent_scope_kind,
    trigger.parent_scope_id,
    trigger.asset_type,
    trigger.location,
    trigger.status,
    trigger.duplicate_count,
    trigger.observed_at,
    trigger.received_at,
    trigger.updated_at,
    trigger.claim_fencing_token
`

// reapExpiredGCPFreshnessTriggerClaimsQuery reclaims 'claimed' GCP freshness
// triggers whose claim lease has expired back to 'queued' so a later handoff
// tick retries them, mirroring the workflow_claims expired-lease reclaim
// pattern (#4464) and the identical AWS freshness reap query. FOR UPDATE SKIP
// LOCKED and the claim_expires_at < $1 predicate keep a reap from ever
// touching a claim whose lease has not actually expired, so it cannot race a
// live claim-holder still processing its batch within the lease window.
// claim_fencing_token is deliberately left untouched: it must keep climbing
// across reclaims so the stale holder's now-superseded token can never match
// a future claim again (#4576).
const reapExpiredGCPFreshnessTriggerClaimsQuery = `
WITH candidate AS (
    SELECT trigger_id
    FROM gcp_freshness_triggers
    WHERE status = 'claimed'
      AND claim_expires_at IS NOT NULL
      AND claim_expires_at < $1
    ORDER BY claim_expires_at ASC, trigger_id ASC
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
UPDATE gcp_freshness_triggers AS trigger
SET status = 'queued',
    claimed_by = NULL,
    claimed_at = NULL,
    claim_expires_at = NULL,
    updated_at = $1
FROM candidate
WHERE trigger.trigger_id = candidate.trigger_id
RETURNING
    trigger.trigger_id,
    trigger.delivery_key,
    trigger.freshness_key,
    trigger.event_kind,
    trigger.event_id,
    trigger.parent_scope_kind,
    trigger.parent_scope_id,
    trigger.asset_type,
    trigger.location,
    trigger.status,
    trigger.duplicate_count,
    trigger.observed_at,
    trigger.received_at,
    trigger.updated_at,
    trigger.claim_fencing_token
`

// markGCPFreshnessTriggersHandedOffQueryFormat mirrors
// markAWSFreshnessTriggersHandedOffQueryFormat; see that constant's doc
// comment for the claim_fencing_token fencing rationale (#4576).
const markGCPFreshnessTriggersHandedOffQueryFormat = `
UPDATE gcp_freshness_triggers AS trigger
SET status = 'handed_off',
    handed_off_at = $%d,
    updated_at = $%d
FROM (VALUES %s) AS fenced(trigger_id, fencing_token)
WHERE trigger.trigger_id = fenced.trigger_id
  AND trigger.claim_fencing_token = fenced.fencing_token
  AND trigger.status = 'claimed'
`

// markGCPFreshnessTriggersFailedQueryFormat mirrors
// markAWSFreshnessTriggersFailedQueryFormat; see that constant's doc comment
// for the claim_fencing_token fencing rationale (#4576).
const markGCPFreshnessTriggersFailedQueryFormat = `
UPDATE gcp_freshness_triggers AS trigger
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
