// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// activeFactWorkItemsCTE keeps live status, drain, and observer reads from
// reporting unleased reducer rows whose generation is older than the scope's
// current active generation. It deliberately keeps claimed/running rows visible
// so a live stale worker remains diagnosable instead of disappearing.
const activeFactWorkItemsCTE = `
active_fact_work_items AS (
  SELECT work.*
  FROM fact_work_items AS work
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = work.scope_id
  JOIN scope_generations AS stale_generation
    ON stale_generation.scope_id = work.scope_id
   AND stale_generation.generation_id = work.generation_id
  LEFT JOIN scope_generations AS active_generation
    ON scope.active_generation_id = active_generation.generation_id
   AND active_generation.scope_id = work.scope_id
  WHERE NOT (
    work.stage = 'reducer'
    AND work.status IN ('pending', 'retrying', 'failed', 'dead_letter')
    AND active_generation.generation_id IS NOT NULL
    AND work.generation_id <> scope.active_generation_id
    AND (
      stale_generation.ingested_at < active_generation.ingested_at
      OR (
        stale_generation.ingested_at = active_generation.ingested_at
        AND stale_generation.generation_id < active_generation.generation_id
      )
    )
  )
)
`

// supersedeInactiveReducerGenerationsCTE terminalizes unleased older-generation
// reducer rows during claim so audit history remains durable without letting
// obsolete work keep readiness in progress forever.
const supersedeInactiveReducerGenerationsCTE = `
superseded_stale_reducer_generations AS (
    UPDATE fact_work_items AS stale
    SET status = 'superseded',
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = NULL,
        next_attempt_at = NULL,
        updated_at = $1,
        failure_class = 'reducer_superseded_by_newer_active_generation',
        failure_message = 'reducer work superseded by newer active generation',
        failure_details = jsonb_build_object(
            'reason', 'inactive_generation',
            'scope_id', stale.scope_id,
            'work_item_id', stale.work_item_id,
            'generation_id', stale.generation_id,
            'active_generation_id', scope.active_generation_id,
            'domain', stale.domain
        )::text
    FROM ingestion_scopes AS scope,
         scope_generations AS stale_generation,
         scope_generations AS active_generation
    WHERE stale.stage = 'reducer'
      AND stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')
      AND ($2::text[] IS NULL OR stale.domain = ANY($2::text[]))
      AND scope.scope_id = stale.scope_id
      AND stale_generation.scope_id = stale.scope_id
      AND stale_generation.generation_id = stale.generation_id
      AND scope.active_generation_id = active_generation.generation_id
      AND active_generation.scope_id = stale.scope_id
      AND stale.generation_id <> scope.active_generation_id
      AND (
        stale_generation.ingested_at < active_generation.ingested_at
        OR (
          stale_generation.ingested_at = active_generation.ingested_at
          AND stale_generation.generation_id < active_generation.generation_id
        )
      )
    RETURNING stale.work_item_id
)
`
