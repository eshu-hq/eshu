// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// claimProjectorWorkPoolSQL is the pool half of the projector claim
// statement: the candidate pool, the fenced lock step, the claim, the fence
// bump, the post-claim sibling reclaim, and the result SELECT. It
// concatenates byte for byte after claimProjectorWorkSweepSQL; the split is
// a file-length cut at a CTE boundary, keep it there.
const claimProjectorWorkPoolSQL = `-- candidate_pool is the snapshot view of claimable rows, with each row's
-- snapshot fence. EvalPlanQual in the lock step re-reads only the locked work
-- and fence rows; pool columns keep their snapshot values either way.
-- MATERIALIZED is a plan choice, not a correctness one: it keeps the pool
-- evaluated once and stops the planner from inlining it into one join tree.
candidate_pool AS MATERIALIZED (
    SELECT work.work_item_id,
           work.scope_id,
           scoped_fence.fence AS snapshot_fence,
           work.updated_at,
           CASE
             WHEN work.status IN ('claimed', 'running') AND work.claim_until <= $1 THEN 0
             ELSE 1
           END AS reclaim_rank,
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
    JOIN projector_scope_claim_fences AS scoped_fence
      ON scoped_fence.scope_id = work.scope_id
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
      -- #7469: the oldest-ready subquery also skips a row held behind an
      -- older same-scope marked generation with waiting (pending, retrying)
      -- work, so the holder becomes oldest and its retry is claimed first; a
      -- guard in this outer WHERE alone stalls when the holder sorts after
      -- the held row by updated_at. Terminal rows never hold: a dead-lettered
      -- marked generation heals through the graph_dirty full snapshot. The
      -- second guard reads the same hold from the sweep's lock-time
      -- marker_spared flag, for a marker committed after the snapshot. All
      -- order comparisons are row-form, identical to the OR tiebreak above.
      -- #7473: a newer delta is also held behind an older same-scope full
      -- with waiting (pending, retrying) work, so the full projects first;
      -- the held side stays a delta, because a newer full supersedes the
      -- older one instead of waiting behind it. Terminal fulls never hold:
      -- a dead-lettered full heals through the collector's next reconcile
      -- decision, not through the queue.
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
            AND NOT EXISTS (
                SELECT 1
                FROM fact_work_items AS waiting
                JOIN scope_generations AS waiting_generation
                  ON waiting_generation.generation_id = waiting.generation_id
                JOIN scope_generations AS held_generation
                  ON held_generation.generation_id = same.generation_id
                WHERE waiting.stage = 'projector'
                  AND waiting.scope_id = same.scope_id
                  AND waiting.work_item_id <> same.work_item_id
                  AND waiting.status IN ('pending', 'retrying')
                  AND waiting_generation.status IN ('pending', 'failed')
                  AND waiting_generation.projection_write_started_at IS NOT NULL
                  AND (waiting_generation.ingested_at, waiting_generation.generation_id) <
                      (held_generation.ingested_at, held_generation.generation_id)
            )
            AND NOT EXISTS (
                SELECT 1
                FROM locked_stale_scope_generations AS spared
                JOIN scope_generations AS spared_generation
                  ON spared_generation.generation_id = spared.generation_id
                JOIN scope_generations AS held_generation
                  ON held_generation.generation_id = same.generation_id
                WHERE spared.marker_spared
                  AND spared_generation.scope_id = same.scope_id
                  AND (spared_generation.ingested_at, spared_generation.generation_id) <
                      (held_generation.ingested_at, held_generation.generation_id)
            )
            AND NOT EXISTS (
                SELECT 1
                FROM fact_work_items AS waiting
                JOIN scope_generations AS waiting_generation
                  ON waiting_generation.generation_id = waiting.generation_id
                JOIN scope_generations AS held_generation
                  ON held_generation.generation_id = same.generation_id
                WHERE waiting.stage = 'projector'
                  AND waiting.scope_id = same.scope_id
                  AND waiting.work_item_id <> same.work_item_id
                  AND waiting.status IN ('pending', 'retrying')
                  AND waiting_generation.status IN ('pending', 'failed')
                  AND NOT waiting_generation.is_delta
                  AND held_generation.is_delta
                  AND (waiting_generation.ingested_at, waiting_generation.generation_id) <
                      (held_generation.ingested_at, held_generation.generation_id)
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
),
-- The lock step visits pool rows in claim order and locks the work row, then
-- its scope's fence row, both SKIP LOCKED; LockRows takes them in locking
-- clause order. It repeats the work row's row-self predicates so EvalPlanQual
-- drops a row claimed after the snapshot (#7108), and joins on fence equality
-- so it drops a scope another claimer claimed in after the snapshot (#7115).
-- A busy work row leaves the fence row unlocked. The pool is sorted in a
-- subquery on the claim-order keys so the planner sees those pathkeys, drops
-- the top-level Sort, and nested-loops the work and fence probes in order:
-- LIMIT 1 then stops at the first lockable row instead of probing every pool
-- row. A CTE scan exposes no order, so ORDER BY inside candidate_pool would
-- not do this. The outer ORDER BY stays and keeps the order guaranteed.
candidate AS (
    SELECT work.work_item_id
    FROM (
        SELECT *
        FROM candidate_pool
        ORDER BY
          reclaim_rank,
          projector_source_inflight_count ASC,
          projector_source_fair_rank ASC,
          updated_at ASC,
          work_item_id ASC
    ) AS pool
    JOIN fact_work_items AS work
      ON work.work_item_id = pool.work_item_id
    JOIN projector_scope_claim_fences AS claim_fence
      ON claim_fence.scope_id = pool.scope_id
     AND claim_fence.fence = pool.snapshot_fence
    WHERE work.stage = 'projector'
      AND work.status IN ('pending', 'retrying', 'claimed', 'running')
      AND (work.visible_at IS NULL OR work.visible_at <= $1)
      AND (work.claim_until IS NULL OR work.claim_until <= $1)
    ORDER BY
      pool.reclaim_rank,
      pool.projector_source_inflight_count ASC,
      pool.projector_source_fair_rank ASC,
      pool.updated_at ASC,
      pool.work_item_id ASC
    LIMIT 1
    FOR UPDATE OF work SKIP LOCKED
    FOR NO KEY UPDATE OF claim_fence SKIP LOCKED
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
-- Bump the fence of the scope this statement claimed in. Data-modifying CTEs
-- always run to completion, so this runs although nothing reads it. It updates
-- only the fence row the lock step already holds, so it never waits.
claimed_scope_fence AS (
    UPDATE projector_scope_claim_fences AS fenced_scope
    SET fence = fenced_scope.fence + 1
    FROM claimed
    WHERE fenced_scope.scope_id = claimed.scope_id
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
        failure_details = CASE
            WHEN stale.failure_class = 'projector_stale_scope_reclaim' THEN stale.failure_details
            ELSE (jsonb_build_object(
                'scope_id', stale.scope_id,
                'work_item_id', stale.work_item_id,
                'claimed_work_item_id', locked.claimed_work_item_id
            ) || ` + priorFailureStaleSQL + `)::text
        END
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
