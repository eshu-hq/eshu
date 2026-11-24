-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #11187 timer-path census: classify every outstanding reducer row of one
-- source by why a paced intake cannot (or can) clear it. Read-only. Run it
-- at the end of every replay arm; arm T3 reads it as its pass evidence.
--
-- Variables (psql -v): source (the source_system to census) and
-- engage_age_seconds. Run it through census.sh, which first creates the
-- session temp table reducer_claim_readiness_requirements from the Go source.
--
-- Each row lands in exactly one class, first match wins, in this order:
--   stale_generation_fenced  superseded by the scope's newer active
--                            generation; the signal drops it (the periodic
--                            sweep terminalizes it).
--   parked_wait_unwoken      retrying with a reducer_waiting_items row and no
--                            reopened_at: parked on a named wait. The signal
--                            skips it (#11187 R4-3); a timer (10 minute
--                            backstop visible_at) or a wake reopens it.
--   woken_holding_wait      a wait row still present although reopened_at is
--                            set (woken on one of several wait keys). It is
--                            real work, but the signal's parked-wait test is the
--                            wait row itself, so the signal skips it until the
--                            row is re-claimed. The enablement report counts
--                            these; a material count returns to the arbiter.
--   timer_future_visible     visible_at in the future: a retry backoff, a
--                            wait backstop, or a paced grant delay. The
--                            signal skips it; only the clock reopens it.
--   readiness_gated          a readiness requirement has no committed phase:
--                            waits on canonical phases the intake delivers.
--                            The signal skips it.
--   conflict_fenced          a live lease holds the row's conflict key, so
--                            the claim cannot take it until the holder
--                            finishes. The signal COUNTS it (it drains
--                            without intake).
--   projector_drain_blocked  a projector item for the scope is still live.
--                            Counted by the signal; the claim skips it only
--                            when the projector drain gate is on.
--   takeable                 counted by the signal and takeable by a claim.
-- The first five classes are outside the signal; the last three are inside
-- it. A latch that stays engaged while takeable stays 0 and the signal
-- classes are all fenced or drain-blocked is a timer-path stall.
--
-- The predicates mirror go/internal/storage/postgres/source_backpressure.go
-- (claimableReadySQL, parkedWaitProbeSQL),
-- reducer_generation_filter_sql.go (activeFactWorkItemsPerRowCTE) and
-- reducer_queue_batch_claim_gates.go (live_fence_keys,
-- projector_live_scopes). Keep them in step when those change.
\set ON_ERROR_STOP on
\pset format aligned

WITH rows AS (
  SELECT work.work_item_id, work.domain, work.scope_id, work.status,
         work.conflict_domain,
         COALESCE(work.conflict_key, work.scope_id) AS ckey,
         work.visible_at, work.reopened_at,
         EXTRACT(EPOCH FROM (now() - COALESCE(work.reopened_at, work.created_at))) AS age_s,
         EXISTS (SELECT 1 FROM reducer_waiting_items w WHERE w.work_item_id = work.work_item_id) AS has_wait,
         (scope.active_generation_id IS NOT NULL
          AND work.generation_id <> scope.active_generation_id
          AND (stale_generation.ingested_at < active_generation.ingested_at
               OR (stale_generation.ingested_at = active_generation.ingested_at
                   AND stale_generation.generation_id < active_generation.generation_id))) AS stale_fenced,
         (work.domain IN (SELECT domain FROM reducer_claim_readiness_requirements)
          AND EXISTS (
            SELECT 1 FROM reducer_claim_readiness_requirements req
            WHERE req.domain = work.domain
              AND NOT EXISTS (
                SELECT 1 FROM graph_projection_phase_state ph
                WHERE ph.scope_id = work.scope_id
                  AND ph.acceptance_unit_id = CASE req.acceptance_unit_source
                        WHEN 'payload_entity_key' THEN COALESCE(NULLIF(work.payload->>'entity_key', ''), work.scope_id)
                        WHEN 'scope_prefix' THEN req.acceptance_unit_prefix || work.scope_id
                        ELSE work.scope_id END
                  AND ph.source_run_id = work.generation_id
                  AND ph.generation_id = work.generation_id
                  AND ph.keyspace = req.keyspace
                  AND ph.phase = req.phase))) AS readiness_missing
  FROM fact_work_items work
  JOIN ingestion_scopes scope ON scope.scope_id = work.scope_id
  JOIN scope_generations stale_generation
    ON stale_generation.scope_id = work.scope_id AND stale_generation.generation_id = work.generation_id
  LEFT JOIN scope_generations active_generation
    ON active_generation.generation_id = scope.active_generation_id AND active_generation.scope_id = work.scope_id
  WHERE work.stage = 'reducer'
    AND work.status IN ('pending', 'retrying')
    AND COALESCE(NULLIF(BTRIM(work.payload->>'source_system'), ''), NULLIF(BTRIM(scope.source_system), ''), 'unknown') = :'source'
),
live_fence AS (
  SELECT DISTINCT conflict_domain, COALESCE(conflict_key, scope_id) AS ckey
  FROM fact_work_items
  WHERE stage = 'reducer' AND status IN ('claimed', 'running') AND claim_until > now()
),
projector_live AS (
  SELECT DISTINCT scope_id FROM fact_work_items
  WHERE stage = 'projector' AND status IN ('pending', 'retrying', 'claimed', 'running')
),
classed AS (
  SELECT rows.*,
    CASE
      WHEN stale_fenced THEN 'stale_generation_fenced'
      WHEN has_wait AND reopened_at IS NULL THEN 'parked_wait_unwoken'
      WHEN has_wait AND reopened_at IS NOT NULL THEN 'woken_holding_wait'
      WHEN visible_at IS NOT NULL AND visible_at > now() THEN 'timer_future_visible'
      WHEN readiness_missing THEN 'readiness_gated'
      WHEN EXISTS (SELECT 1 FROM live_fence f WHERE f.conflict_domain = rows.conflict_domain AND f.ckey = rows.ckey) THEN 'conflict_fenced'
      WHEN EXISTS (SELECT 1 FROM projector_live p WHERE p.scope_id = rows.scope_id) THEN 'projector_drain_blocked'
      ELSE 'takeable'
    END AS class
  FROM rows
)
SELECT class,
       count(*) AS row_count,
       count(*) FILTER (WHERE status = 'retrying') AS retrying,
       round(COALESCE(max(age_s), 0))::bigint AS oldest_age_seconds,
       count(*) FILTER (WHERE age_s > :engage_age_seconds) AS past_engage_age
FROM classed
GROUP BY class
ORDER BY CASE class
  WHEN 'takeable' THEN 1 WHEN 'projector_drain_blocked' THEN 2 WHEN 'conflict_fenced' THEN 3
  WHEN 'readiness_gated' THEN 4 WHEN 'timer_future_visible' THEN 5 WHEN 'woken_holding_wait' THEN 6
  WHEN 'parked_wait_unwoken' THEN 7 ELSE 8 END;

SELECT count(*) AS wait_table_rows FROM reducer_waiting_items;
