// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const enqueueProjectorWorkQuery = `
INSERT INTO fact_work_items (
    work_item_id,
    scope_id,
    generation_id,
    stage,
    domain,
    status,
    attempt_count,
    lease_owner,
    claim_until,
    visible_at,
    last_attempt_at,
    next_attempt_at,
    failure_class,
    failure_message,
    failure_details,
    payload,
    created_at,
    updated_at
) VALUES (
    $1, $2, $3, 'projector', $4, 'pending', 0, NULL, NULL, $5, NULL, NULL, NULL, NULL, NULL, '{}'::jsonb, $5, $5
)
ON CONFLICT (work_item_id) DO NOTHING
`

const supersedeProjectorActiveGenerationQuery = `
UPDATE scope_generations
SET status = 'superseded',
    superseded_at = $1
WHERE scope_id = $2
  AND generation_id <> $3
  AND status = 'active'
`

const supersedeProjectorObsoleteGenerationsQuery = `
WITH superseded_work AS (
    UPDATE fact_work_items AS stale
    SET status = 'superseded',
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = NULL,
        next_attempt_at = NULL,
        updated_at = $1,
        failure_class = 'projector_superseded_by_newer_generation',
        failure_message = 'projector work superseded by newer same-scope generation',
        failure_details = jsonb_build_object(
            'scope_id', stale.scope_id,
            'work_item_id', stale.work_item_id,
            'generation_id', stale.generation_id,
            'current_generation_id', $3
        )
    FROM scope_generations AS stale_generation,
         scope_generations AS current_generation
    WHERE stale.stage = 'projector'
      AND stale.scope_id = $2
      AND stale.generation_id <> $3
      AND stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')
      AND stale_generation.scope_id = stale.scope_id
      AND stale_generation.generation_id = stale.generation_id
      AND stale_generation.status IN ('pending', 'failed')
      AND current_generation.scope_id = stale.scope_id
      AND current_generation.generation_id = $3
      AND current_generation.status IN ('pending', 'active')
      AND (
          stale_generation.ingested_at < current_generation.ingested_at
          OR (
              stale_generation.ingested_at = current_generation.ingested_at
              AND stale_generation.generation_id < current_generation.generation_id
          )
      )
    RETURNING stale.work_item_id, stale.generation_id
)
UPDATE scope_generations AS generation
SET status = 'superseded',
    superseded_at = $1
FROM superseded_work
WHERE generation.generation_id = superseded_work.generation_id
  AND generation.status IN ('pending', 'failed')
`

const activateProjectorGenerationQuery = `
UPDATE scope_generations
SET status = 'active',
    activated_at = COALESCE(activated_at, $1),
    superseded_at = NULL
WHERE scope_id = $2
  AND generation_id = $3
`

const updateProjectorScopeGenerationQuery = `
UPDATE ingestion_scopes
SET status = 'active',
    active_generation_id = $3,
    ingested_at = $1
WHERE scope_id = $2
`

const ackProjectorWorkItemQuery = `
UPDATE fact_work_items
SET status = 'succeeded',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE stage = 'projector'
  AND scope_id = $2
  AND generation_id = $3
  AND lease_owner = $4
  AND status IN ('claimed', 'running')
`

const heartbeatProjectorWorkQuery = `
UPDATE fact_work_items
SET status = 'running',
    claim_until = $1,
    updated_at = $2
WHERE stage = 'projector'
  AND scope_id = $3
  AND generation_id = $4
  AND lease_owner = $5
  AND status IN ('claimed', 'running')
`

const supersedeRunningProjectorWorkQuery = `
WITH superseded_work AS (
UPDATE fact_work_items AS work
SET status = 'superseded',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    next_attempt_at = NULL,
    updated_at = $1,
    failure_class = 'projector_superseded_by_newer_generation',
    failure_message = 'running projector work superseded by newer same-scope generation',
    failure_details = jsonb_build_object(
        'scope_id', work.scope_id,
        'work_item_id', work.work_item_id,
        'generation_id', work.generation_id
    )
FROM scope_generations AS current_generation
WHERE work.stage = 'projector'
  AND work.scope_id = $2
  AND work.generation_id = $3
  AND work.lease_owner = $4
  AND work.status IN ('claimed', 'running')
  AND current_generation.scope_id = work.scope_id
  AND current_generation.generation_id = work.generation_id
  AND current_generation.status IN ('pending', 'active')
  AND EXISTS (
      SELECT 1
      FROM scope_generations AS newer
      WHERE newer.scope_id = current_generation.scope_id
        AND newer.generation_id <> current_generation.generation_id
        AND newer.status IN ('pending', 'active')
        AND (
            newer.ingested_at > current_generation.ingested_at
            OR (
                newer.ingested_at = current_generation.ingested_at
                AND newer.generation_id > current_generation.generation_id
            )
        )
  )
  RETURNING work.generation_id
)
UPDATE scope_generations AS generation
SET status = 'superseded',
    superseded_at = $1
FROM superseded_work
WHERE generation.generation_id = superseded_work.generation_id
  AND generation.status IN ('pending', 'active')
`

const retryProjectorWorkQuery = `
UPDATE fact_work_items
SET status = 'retrying',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $5,
    next_attempt_at = $5,
    updated_at = $1,
    failure_class = $2,
    failure_message = $3,
    failure_details = $4
WHERE stage = 'projector'
  AND scope_id = $6
  AND generation_id = $7
  AND lease_owner = $8
  AND status IN ('claimed', 'running')
`

const failProjectorWorkQuery = `
WITH failed_generation AS (
    UPDATE scope_generations
    SET status = 'failed'
    WHERE generation_id = $6
      AND status IN ('pending', 'active')
),
scope_update AS (
    UPDATE ingestion_scopes
    SET status = CASE
            WHEN active_generation_id = $6 OR active_generation_id IS NULL THEN 'failed'
            ELSE status
        END,
        active_generation_id = CASE
            WHEN active_generation_id = $6 THEN NULL
            ELSE active_generation_id
        END,
        ingested_at = CASE
            WHEN active_generation_id = $6 OR active_generation_id IS NULL THEN $1
            ELSE ingested_at
        END
    WHERE scope_id = $5
)
UPDATE fact_work_items
SET status = 'dead_letter',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = $2,
    failure_message = $3,
    failure_details = $4
WHERE stage = 'projector'
  AND scope_id = $5
  AND generation_id = $6
  AND lease_owner = $7
  AND status IN ('claimed', 'running')
`
