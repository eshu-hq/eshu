// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const storeAWSFreshnessTriggerQuery = `
INSERT INTO aws_freshness_triggers (
    trigger_id,
    delivery_key,
    freshness_key,
    event_kind,
    event_id,
    account_id,
    region,
    service_kind,
    resource_type,
    resource_id,
    status,
    observed_at,
    received_at,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
ON CONFLICT (freshness_key) DO UPDATE
SET trigger_id = CASE
        WHEN aws_freshness_triggers.status = 'claimed'
        THEN aws_freshness_triggers.trigger_id
        ELSE EXCLUDED.trigger_id
    END,
    delivery_key = EXCLUDED.delivery_key,
    event_kind = EXCLUDED.event_kind,
    event_id = EXCLUDED.event_id,
    account_id = EXCLUDED.account_id,
    region = EXCLUDED.region,
    service_kind = EXCLUDED.service_kind,
    resource_type = EXCLUDED.resource_type,
    resource_id = EXCLUDED.resource_id,
    status = CASE
        WHEN aws_freshness_triggers.status = 'claimed'
        THEN aws_freshness_triggers.status
        ELSE EXCLUDED.status
    END,
    duplicate_count = aws_freshness_triggers.duplicate_count + 1,
    observed_at = EXCLUDED.observed_at,
    claimed_by = CASE
        WHEN aws_freshness_triggers.status = 'claimed'
        THEN aws_freshness_triggers.claimed_by
        ELSE NULL
    END,
    claimed_at = CASE
        WHEN aws_freshness_triggers.status = 'claimed'
        THEN aws_freshness_triggers.claimed_at
        ELSE NULL
    END,
    handed_off_at = CASE
        WHEN aws_freshness_triggers.status = 'claimed'
        THEN aws_freshness_triggers.handed_off_at
        ELSE NULL
    END,
    failed_at = CASE
        WHEN aws_freshness_triggers.status = 'claimed'
        THEN aws_freshness_triggers.failed_at
        ELSE NULL
    END,
    failure_class = CASE
        WHEN aws_freshness_triggers.status = 'claimed'
        THEN aws_freshness_triggers.failure_class
        ELSE NULL
    END,
    failure_message = CASE
        WHEN aws_freshness_triggers.status = 'claimed'
        THEN aws_freshness_triggers.failure_message
        ELSE NULL
    END,
    updated_at = EXCLUDED.updated_at
RETURNING
    trigger_id,
    delivery_key,
    freshness_key,
    event_kind,
    event_id,
    account_id,
    region,
    service_kind,
    resource_type,
    resource_id,
    status,
    duplicate_count,
    observed_at,
    received_at,
    updated_at,
    claim_fencing_token
`

// claimQueuedAWSFreshnessTriggersQuery claims up to $1 'queued' rows and
// bumps claim_fencing_token on every claimed row (#4576). The bumped token is
// returned to the caller so it can later be presented back to
// MarkTriggersHandedOff/MarkTriggersFailed, which only complete a row whose
// claim_fencing_token still matches: this is what stops a stale claimant
// whose lease expired and was reaped, then re-claimed by a different owner
// (bumping the token again), from completing a claim it no longer holds
// (raised in PR #4682 review).
const claimQueuedAWSFreshnessTriggersQuery = `
WITH claimed AS (
    SELECT trigger_id
    FROM aws_freshness_triggers
    WHERE status = 'queued'
    ORDER BY received_at ASC, trigger_id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE aws_freshness_triggers AS trigger
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
    trigger.account_id,
    trigger.region,
    trigger.service_kind,
    trigger.resource_type,
    trigger.resource_id,
    trigger.status,
    trigger.duplicate_count,
    trigger.observed_at,
    trigger.received_at,
    trigger.updated_at,
    trigger.claim_fencing_token
`

// reapExpiredAWSFreshnessTriggerClaimsQuery reclaims 'claimed' AWS freshness
// triggers whose claim lease has expired back to 'queued' so a later handoff
// tick retries them, mirroring the workflow_claims expired-lease reclaim
// pattern (#4464). FOR UPDATE SKIP LOCKED lets a concurrent reap invocation
// (or a still-running claim-holder that has not yet raced to expiry) skip
// rows another transaction already holds instead of blocking, and the
// claim_expires_at < $1 predicate ensures a reap can never touch a claim
// whose lease has not actually expired yet, so it cannot race a live
// claim-holder still processing its batch within the lease window.
// claim_fencing_token is deliberately left untouched: it must keep climbing
// across reclaims so the stale holder's now-superseded token can never match
// a future claim again (#4576).
const reapExpiredAWSFreshnessTriggerClaimsQuery = `
WITH candidate AS (
    SELECT trigger_id
    FROM aws_freshness_triggers
    WHERE status = 'claimed'
      AND claim_expires_at IS NOT NULL
      AND claim_expires_at < $1
    ORDER BY claim_expires_at ASC, trigger_id ASC
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
UPDATE aws_freshness_triggers AS trigger
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
    trigger.account_id,
    trigger.region,
    trigger.service_kind,
    trigger.resource_type,
    trigger.resource_id,
    trigger.status,
    trigger.duplicate_count,
    trigger.observed_at,
    trigger.received_at,
    trigger.updated_at,
    trigger.claim_fencing_token
`

// markAWSFreshnessTriggersHandedOffQueryFormat fences completion by
// claim_fencing_token (#4576): the join predicate only updates a row whose
// current token still matches the token the caller received back from
// ClaimQueuedTriggers. A claim reaped by ReapExpiredTriggerClaims and later
// re-claimed by a different owner bumps claim_fencing_token again, so the
// original (now-stale) caller's completion silently affects zero rows
// instead of completing a claim it no longer holds (raised in PR #4682
// review). %s is one "($n, $n+1)" placeholder pair per (trigger_id,
// fencing_token) row.
const markAWSFreshnessTriggersHandedOffQueryFormat = `
UPDATE aws_freshness_triggers AS trigger
SET status = 'handed_off',
    handed_off_at = $%d,
    updated_at = $%d
FROM (VALUES %s) AS fenced(trigger_id, fencing_token)
WHERE trigger.trigger_id = fenced.trigger_id
  AND trigger.claim_fencing_token = fenced.fencing_token
  AND trigger.status = 'claimed'
`

// markAWSFreshnessTriggersFailedQueryFormat is
// markAWSFreshnessTriggersHandedOffQueryFormat's failure-path counterpart;
// see that constant's doc comment for the fencing rationale (#4576).
const markAWSFreshnessTriggersFailedQueryFormat = `
UPDATE aws_freshness_triggers AS trigger
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
