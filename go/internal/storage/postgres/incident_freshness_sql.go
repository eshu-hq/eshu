// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const storeIncidentFreshnessTriggerQuery = `
INSERT INTO incident_freshness_triggers (
    trigger_id,
    delivery_key,
    freshness_key,
    provider,
    event_kind,
    event_id,
    scope_id,
    resource_id,
    status,
    observed_at,
    received_at,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
ON CONFLICT (freshness_key) DO UPDATE
SET trigger_id = CASE
        WHEN incident_freshness_triggers.status = 'claimed'
        THEN incident_freshness_triggers.trigger_id
        ELSE EXCLUDED.trigger_id
    END,
    delivery_key = EXCLUDED.delivery_key,
    provider = EXCLUDED.provider,
    event_kind = EXCLUDED.event_kind,
    event_id = EXCLUDED.event_id,
    scope_id = EXCLUDED.scope_id,
    resource_id = EXCLUDED.resource_id,
    status = CASE
        WHEN incident_freshness_triggers.status = 'claimed'
        THEN incident_freshness_triggers.status
        ELSE EXCLUDED.status
    END,
    duplicate_count = incident_freshness_triggers.duplicate_count + 1,
    observed_at = EXCLUDED.observed_at,
    claimed_by = CASE
        WHEN incident_freshness_triggers.status = 'claimed'
        THEN incident_freshness_triggers.claimed_by
        ELSE NULL
    END,
    claimed_at = CASE
        WHEN incident_freshness_triggers.status = 'claimed'
        THEN incident_freshness_triggers.claimed_at
        ELSE NULL
    END,
    handed_off_at = CASE
        WHEN incident_freshness_triggers.status = 'claimed'
        THEN incident_freshness_triggers.handed_off_at
        ELSE NULL
    END,
    failed_at = CASE
        WHEN incident_freshness_triggers.status = 'claimed'
        THEN incident_freshness_triggers.failed_at
        ELSE NULL
    END,
    failure_class = CASE
        WHEN incident_freshness_triggers.status = 'claimed'
        THEN incident_freshness_triggers.failure_class
        ELSE NULL
    END,
    failure_message = CASE
        WHEN incident_freshness_triggers.status = 'claimed'
        THEN incident_freshness_triggers.failure_message
        ELSE NULL
    END,
    updated_at = EXCLUDED.updated_at
RETURNING
    trigger_id,
    delivery_key,
    freshness_key,
    provider,
    event_kind,
    event_id,
    scope_id,
    resource_id,
    status,
    duplicate_count,
    observed_at,
    received_at,
    updated_at
`

const claimQueuedIncidentFreshnessTriggersQuery = `
WITH claimed AS (
    SELECT trigger_id
    FROM incident_freshness_triggers
    WHERE status = 'queued'
    ORDER BY received_at ASC, trigger_id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE incident_freshness_triggers AS trigger
SET status = 'claimed',
    claimed_by = $2,
    claimed_at = $3,
    updated_at = $3
FROM claimed
WHERE trigger.trigger_id = claimed.trigger_id
RETURNING
    trigger.trigger_id,
    trigger.delivery_key,
    trigger.freshness_key,
    trigger.provider,
    trigger.event_kind,
    trigger.event_id,
    trigger.scope_id,
    trigger.resource_id,
    trigger.status,
    trigger.duplicate_count,
    trigger.observed_at,
    trigger.received_at,
    trigger.updated_at
`

const markIncidentFreshnessTriggersHandedOffQueryFormat = `
UPDATE incident_freshness_triggers
SET status = 'handed_off',
    handed_off_at = $%d,
    updated_at = $%d
WHERE trigger_id IN (%s)
  AND status = 'claimed'
`

const markIncidentFreshnessTriggersFailedQueryFormat = `
UPDATE incident_freshness_triggers
SET status = 'failed',
    failure_class = $%d,
    failure_message = $%d,
    failed_at = $%d,
    updated_at = $%d
WHERE trigger_id IN (%s)
  AND status = 'claimed'
`
