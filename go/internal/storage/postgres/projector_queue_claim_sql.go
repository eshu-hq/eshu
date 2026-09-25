// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const claimProjectorWorkQuery = `
WITH source_scoped_projector_work AS (
    SELECT work.work_item_id,
           candidate_scope.source_system
    FROM fact_work_items AS work
    JOIN ingestion_scopes AS candidate_scope
      ON candidate_scope.scope_id = work.scope_id
    WHERE work.stage = 'projector'
      AND (
          $4 = ''
          OR candidate_scope.source_system = $4
      )
),
-- Every maintenance branch locks its rows first with SKIP LOCKED and then
-- updates only the rows it locked (#7108). A blocking multi-row UPDATE here
-- let concurrent claimers lock the same stale rows in different orders and
-- deadlock (40P01). With every row lock non-blocking, the claim statement
-- never waits on a row lock, so it cannot join a wait cycle. The locking
-- SELECT repeats each row-self predicate so the EvalPlanQual recheck drops a
-- row another transaction changed after this statement's snapshot. NO KEY
-- UPDATE matches the lock the UPDATE takes and stays compatible with the
-- KEY SHARE locks that foreign-key inserts hold on scope_generations.
locked_stale_projector_duplicates AS (
    SELECT stale.work_item_id
    FROM fact_work_items AS stale
    JOIN source_scoped_projector_work AS scoped
      ON scoped.work_item_id = stale.work_item_id
    WHERE stale.stage = 'projector'
      AND stale.status IN ('claimed', 'running')
      AND stale.claim_until <= $1
      AND EXISTS (
          SELECT 1
          FROM fact_work_items AS live
          WHERE live.stage = 'projector'
            AND live.scope_id = stale.scope_id
            AND live.work_item_id <> stale.work_item_id
            AND live.status IN ('claimed', 'running')
            AND live.claim_until > $1
      )
    ORDER BY stale.work_item_id
    FOR NO KEY UPDATE OF stale SKIP LOCKED
),
reclaimed_stale_projector_duplicates AS (
    UPDATE fact_work_items AS stale
    SET status = 'retrying',
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = $1,
        next_attempt_at = $1,
        updated_at = $1,
        failure_class = 'projector_stale_scope_reclaim',
        failure_message = 'expired duplicate projector lease reclaimed',
        failure_details = jsonb_build_object(
            'scope_id', stale.scope_id,
            'work_item_id', stale.work_item_id
        )
    FROM locked_stale_projector_duplicates AS locked
    WHERE stale.work_item_id = locked.work_item_id
      AND stale.stage = 'projector'
      AND stale.status IN ('claimed', 'running')
      AND stale.claim_until <= $1
),
-- supersedable_projector_generations is the snapshot view of stale work.
-- Supersede then locks each generation row and after it the work row, both
-- non-blocking, and updates only work rows it locked. The candidate never
-- claims a supersedable row, even one this statement could not lock; the
-- oldest-ready-row subquery still skips only rows superseded here, so a scope
-- whose stale row is busy yields no claim instead of a newer generation.
supersedable_projector_generations AS (
    SELECT stale.work_item_id,
           stale_generation.generation_id
    FROM fact_work_items AS stale
    JOIN source_scoped_projector_work AS scoped
      ON scoped.work_item_id = stale.work_item_id
    JOIN scope_generations AS stale_generation
      ON stale_generation.generation_id = stale.generation_id
    WHERE stale.stage = 'projector'
      AND stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')
      AND stale_generation.status IN ('pending', 'failed')
      AND EXISTS (
          SELECT 1
          FROM fact_work_items AS newer
          JOIN scope_generations AS newer_generation
            ON newer_generation.generation_id = newer.generation_id
          WHERE newer.stage = 'projector'
            AND newer.scope_id = stale.scope_id
            AND newer.work_item_id <> stale.work_item_id
            AND newer.status IN ('pending', 'retrying', 'claimed', 'running', 'succeeded', 'failed', 'dead_letter', 'superseded')
            AND (
                newer_generation.ingested_at > stale_generation.ingested_at
                OR (
                    newer_generation.ingested_at = stale_generation.ingested_at
                    AND newer_generation.generation_id > stale_generation.generation_id
                )
            )
      )
),
locked_stale_scope_generations AS (
    SELECT supersedable.work_item_id
    FROM scope_generations AS stale_generation
    JOIN supersedable_projector_generations AS supersedable
      ON supersedable.generation_id = stale_generation.generation_id
    WHERE stale_generation.status IN ('pending', 'failed')
    ORDER BY stale_generation.generation_id
    FOR NO KEY UPDATE OF stale_generation SKIP LOCKED
),
locked_stale_projector_generations AS (
    SELECT stale.work_item_id
    FROM fact_work_items AS stale
    JOIN locked_stale_scope_generations AS locked_generation
      ON locked_generation.work_item_id = stale.work_item_id
    WHERE stale.stage = 'projector'
      AND stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')
    ORDER BY stale.work_item_id
    FOR NO KEY UPDATE OF stale SKIP LOCKED
),
superseded_stale_projector_generations AS (
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
            'work_item_id', stale.work_item_id
        )
    FROM locked_stale_projector_generations AS locked
    WHERE stale.work_item_id = locked.work_item_id
      AND stale.stage = 'projector'
      AND stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')
    RETURNING stale.work_item_id, stale.generation_id
),
superseded_stale_scope_generations AS (
    UPDATE scope_generations AS generation
    SET status = 'superseded',
        superseded_at = $1
    FROM superseded_stale_projector_generations AS stale
    WHERE generation.generation_id = stale.generation_id
      AND generation.status IN ('pending', 'failed')
),
candidate AS (
    SELECT work.work_item_id,
           (
               SELECT count(*)
               FROM fact_work_items AS inflight_source
               JOIN ingestion_scopes AS inflight_scope
                 ON inflight_scope.scope_id = inflight_source.scope_id
               WHERE inflight_source.stage = 'projector'
                 AND inflight_scope.source_system = scoped.source_system
                 AND inflight_source.work_item_id <> work.work_item_id
                 AND inflight_source.status IN ('claimed', 'running')
                 AND inflight_source.claim_until > $1
           ) AS projector_source_inflight_count,
           0 AS projector_source_fair_rank
    FROM fact_work_items AS work
    JOIN source_scoped_projector_work AS scoped
      ON scoped.work_item_id = work.work_item_id
    WHERE work.stage = 'projector'
      AND work.status IN ('pending', 'retrying', 'claimed', 'running')
      AND (work.visible_at IS NULL OR work.visible_at <= $1)
      AND (work.claim_until IS NULL OR work.claim_until <= $1)
      AND NOT EXISTS (
          SELECT 1
          FROM supersedable_projector_generations AS supersedable
          WHERE supersedable.work_item_id = work.work_item_id
      )
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS inflight
          WHERE inflight.stage = 'projector'
            AND inflight.scope_id = work.scope_id
            AND inflight.work_item_id <> work.work_item_id
            AND inflight.status IN ('claimed', 'running')
            AND inflight.claim_until > $1
      )
      -- Every concurrent projector claimer for a scope must target the same
      -- oldest ready row. Otherwise FOR UPDATE SKIP LOCKED lets workers skip a
      -- locked older row and start a newer generation for the same repository.
      AND work.work_item_id = (
          SELECT same.work_item_id
          FROM fact_work_items AS same
          WHERE same.stage = 'projector'
            AND same.scope_id = work.scope_id
            AND same.status IN ('pending', 'retrying', 'claimed', 'running')
            AND (same.visible_at IS NULL OR same.visible_at <= $1)
            AND (same.claim_until IS NULL OR same.claim_until <= $1)
            AND NOT EXISTS (
                SELECT 1
                FROM superseded_stale_projector_generations AS superseded_same
                WHERE superseded_same.work_item_id = same.work_item_id
            )
          ORDER BY
            CASE
              WHEN same.status IN ('claimed', 'running') AND same.claim_until <= $1 THEN 0
              ELSE 1
            END,
            same.updated_at ASC,
            same.work_item_id ASC
          LIMIT 1
      )
    ORDER BY
      CASE
        WHEN work.status IN ('claimed', 'running') AND work.claim_until <= $1 THEN 0
        ELSE 1
      END,
      projector_source_inflight_count ASC,
      projector_source_fair_rank ASC,
      work.updated_at ASC,
      work.work_item_id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
),
claimed AS (
    UPDATE fact_work_items AS work
    SET status = 'claimed',
        attempt_count = work.attempt_count + 1,
        lease_owner = $2,
        claim_until = $3,
        last_attempt_at = $1,
        updated_at = $1
    FROM candidate
    WHERE work.work_item_id = candidate.work_item_id
    RETURNING work.work_item_id, work.scope_id, work.generation_id, work.attempt_count
),
locked_claim_siblings AS (
    SELECT stale.work_item_id,
           claimed.work_item_id AS claimed_work_item_id
    FROM fact_work_items AS stale
    JOIN claimed
      ON stale.scope_id = claimed.scope_id
    WHERE stale.stage = 'projector'
      AND stale.work_item_id <> claimed.work_item_id
      AND stale.status IN ('claimed', 'running')
      AND stale.claim_until <= $1
    ORDER BY stale.work_item_id
    FOR NO KEY UPDATE OF stale SKIP LOCKED
),
reclaimed_claim_siblings AS (
    UPDATE fact_work_items AS stale
    SET status = 'retrying',
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = $1,
        next_attempt_at = $1,
        updated_at = $1,
        failure_class = 'projector_stale_scope_reclaim',
        failure_message = 'expired duplicate projector lease reclaimed',
        failure_details = jsonb_build_object(
            'scope_id', stale.scope_id,
            'work_item_id', stale.work_item_id,
            'claimed_work_item_id', locked.claimed_work_item_id
        )
    FROM locked_claim_siblings AS locked
    WHERE stale.work_item_id = locked.work_item_id
      AND stale.stage = 'projector'
      AND stale.status IN ('claimed', 'running')
      AND stale.claim_until <= $1
)
SELECT
    scope.scope_id,
    scope.source_system,
    scope.scope_kind,
    COALESCE(scope.parent_scope_id, ''),
    COALESCE(scope.active_generation_id, ''),
    EXISTS (
        SELECT 1
        FROM scope_generations AS prior_generation
        WHERE prior_generation.scope_id = scope.scope_id
          AND prior_generation.generation_id <> claimed.generation_id
    ),
    scope.collector_kind,
    scope.partition_key,
    generation.generation_id,
    claimed.attempt_count,
    generation.observed_at,
    generation.ingested_at,
    generation.status,
    generation.trigger_kind,
    COALESCE(generation.freshness_hint, ''),
    COALESCE(scope.payload, '{}'::jsonb)
FROM claimed
JOIN ingestion_scopes AS scope
  ON scope.scope_id = claimed.scope_id
JOIN scope_generations AS generation
  ON generation.generation_id = claimed.generation_id
`
