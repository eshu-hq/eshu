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

// activateProjectorGenerationQuery is Ack's last statement and the first one
// that row-locks the target generation. superseded is terminal (#7130), so the
// status predicate refuses to revive it. Because the predicate sits on the
// locked row, EvalPlanQual re-evaluates it against a supersede that committed
// after this statement's snapshot, including the claim path's stale-generation
// supersede, which locks the generation row but not the scope row. Ack treats
// zero affected rows as a superseded generation and rolls back.
const activateProjectorGenerationQuery = `
UPDATE scope_generations
SET status = 'active',
    activated_at = COALESCE(activated_at, $1),
    superseded_at = NULL
WHERE scope_id = $2
  AND generation_id = $3
  AND status <> 'superseded'
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
  AND attempt_count = $5
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
  AND attempt_count = $6
  AND status IN ('claimed', 'running')
`

// supersedeRunningProjectorWorkQuery runs on every heartbeat. It must never
// wait on the scope row: ingestion holds that row while it streams facts, and a
// waiting heartbeat would let the lease lapse. SKIP LOCKED defers supersession
// to a later heartbeat while ingestion, Ack, or Fail owns the scope, and the
// caller then renews the lease. NO KEY UPDATE still conflicts with those scope
// writers but not with foreign-key KEY SHARE locks from unrelated child inserts.
const supersedeRunningProjectorWorkQuery = `
WITH locked_scope AS MATERIALIZED (
    SELECT scope_id
    FROM ingestion_scopes
    WHERE scope_id = $2
    FOR NO KEY UPDATE SKIP LOCKED
),
superseded_work AS (
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
FROM locked_scope AS scope,
     scope_generations AS current_generation
WHERE work.stage = 'projector'
  AND work.scope_id = scope.scope_id
  AND work.generation_id = $3
  AND work.lease_owner = $4
  AND work.attempt_count = $5
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
-- An active generation remains published until successor Ack changes the scope
-- pointer in the same transaction. Still update this row so RowsAffected
-- reports the superseded work to Heartbeat for either generation status.
SET status = CASE WHEN generation.status = 'pending' THEN 'superseded' ELSE generation.status END,
    superseded_at = CASE WHEN generation.status = 'pending' THEN $1 ELSE generation.superseded_at END
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
  AND attempt_count = $9
  AND status IN ('claimed', 'running')
`

const failProjectorWorkQuery = `
WITH locked_scope AS MATERIALIZED (
    SELECT scope_id
    FROM ingestion_scopes
    WHERE scope_id = $5
    FOR NO KEY UPDATE
),
owned_work AS MATERIALIZED (
    SELECT work.work_item_id, work.scope_id, work.generation_id
    FROM locked_scope AS scope
    JOIN fact_work_items AS work ON work.scope_id = scope.scope_id
    WHERE work.stage = 'projector'
      AND work.generation_id = $6
      AND work.lease_owner = $7
      AND work.attempt_count = $8
      AND work.status IN ('claimed', 'running')
    FOR UPDATE OF work
),
failed_generation AS (
    UPDATE scope_generations AS generation
    SET status = 'failed'
    FROM owned_work
    WHERE generation.generation_id = owned_work.generation_id
      AND generation.scope_id = owned_work.scope_id
      AND generation.status IN ('pending', 'active')
    RETURNING generation.scope_id, generation.generation_id
),
scope_update AS (
    UPDATE ingestion_scopes AS scope
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
    FROM failed_generation
    WHERE scope.scope_id = failed_generation.scope_id
)
UPDATE fact_work_items AS work
SET status = 'dead_letter',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = $2,
    failure_message = $3,
    failure_details = $4
FROM owned_work
WHERE work.work_item_id = owned_work.work_item_id
`

// projectorAckGenerationSupersededClass is the failure_class Ack records when
// it refuses to activate a generation that is already superseded (#7130).
const projectorAckGenerationSupersededClass = "projector_ack_generation_superseded"

// projectorReplayGenerationSupersededClass labels projector rows a replay left
// terminal because their generation is superseded (#7130).
const projectorReplayGenerationSupersededClass = "projector_replay_generation_superseded"

// markProjectorAckSupersededQuery ends a claimed projector work item whose
// generation Ack refused to activate. It runs after the Ack transaction rolled
// back, as one statement that locks only the work row, so it cannot join a
// lock cycle. It re-checks ownership and the superseded status (terminal, so
// the read cannot go stale), which keeps a lost claim a claim rejection.
const markProjectorAckSupersededQuery = `
UPDATE fact_work_items AS work
SET status = 'superseded',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    next_attempt_at = NULL,
    updated_at = $1,
    failure_class = '` + projectorAckGenerationSupersededClass + `',
    failure_message = 'projector ack refused: generation already superseded',
    failure_details = jsonb_build_object(
        'scope_id', work.scope_id,
        'work_item_id', work.work_item_id,
        'generation_id', work.generation_id
    )
FROM scope_generations AS generation
WHERE work.stage = 'projector'
  AND work.scope_id = $2
  AND work.generation_id = $3
  AND work.lease_owner = $4
  AND work.attempt_count = $5
  AND work.status IN ('claimed', 'running')
  AND generation.scope_id = work.scope_id
  AND generation.generation_id = work.generation_id
  AND generation.status = 'superseded'
`

// supersededProjectorGenerationFence matches a projector row whose scope
// generation is superseded (#7130). superseded is terminal, and acking such a
// row would try to re-activate the retired generation, so replay leaves these
// rows terminal. The fence is projector-only; reducer work keeps its own
// generation handling. The unqualified column resolves to the fact_work_items
// row of the innermost enclosing query in every template that uses it.
const supersededProjectorGenerationFence = `(stage = 'projector' AND EXISTS (
          SELECT 1 FROM scope_generations AS fenced_generation
          WHERE fenced_generation.generation_id = fact_work_items.generation_id
            AND fenced_generation.status = 'superseded'))`

// countSupersededReplaySkipsTemplate counts the rows the replay fence left in
// place for the same filter. %s is the unfenced predicate, from $1.
const countSupersededReplaySkipsTemplate = `
SELECT COUNT(*) FROM fact_work_items
WHERE status IN ('dead_letter', 'failed')
  %s
  AND ` + supersededProjectorGenerationFence + `
`
