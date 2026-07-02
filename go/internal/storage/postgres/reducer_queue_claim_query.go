// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

var claimReducerWorkQuery = `
WITH ` + reducerClaimReadinessRequirementsCTE() + `,
` + supersedeInactiveReducerGenerationsCTE + `,
candidate AS (
    SELECT work_item_id
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND NOT EXISTS (
          SELECT 1
          FROM superseded_stale_reducer_generations AS superseded
          WHERE superseded.work_item_id = fact_work_items.work_item_id
      )
      AND (visible_at IS NULL OR visible_at <= $1)
      AND (claim_until IS NULL OR claim_until <= $1)
      AND ($2::text[] IS NULL OR domain = ANY($2::text[]))
      -- NornicDB local_authoritative first-generation runs must not let
      -- reducer graph writes contend with source-local canonical projection
      -- for the same scope. Unrelated scopes can continue draining.
      AND ($5 = false OR NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS projector_work
          WHERE projector_work.stage = 'projector'
            AND projector_work.scope_id = fact_work_items.scope_id
            AND projector_work.status IN ('pending', 'retrying', 'claimed', 'running')
      ))
      -- Semantic entity materialization writes labels onto source-local
      -- content-entity nodes. On NornicDB, running those writes while any
      -- source-local projection is still active causes cross-scope graph
      -- backend contention and retry storms; non-semantic reducer domains can
      -- still drain once their own scope has passed the gate above.
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS projector_any
          WHERE projector_any.stage = 'projector'
            AND projector_any.domain = 'source_local'
            AND projector_any.status IN ('pending', 'retrying', 'claimed', 'running')
      ))
      -- In local-host watch mode the ingester discovers and enqueues source
      -- projector work incrementally. A temporary enqueue gap is not proof
      -- that the whole corpus has drained, so semantic reducers can also wait
      -- for the owner-discovered source-local success count.
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR $6 <= 0 OR (
          SELECT count(*)
          FROM fact_work_items AS projector_done
          WHERE projector_done.stage = 'projector'
            AND projector_done.domain = 'source_local'
            AND projector_done.status = 'succeeded'
      ) >= $6)
      -- Operators can cap cross-scope semantic-entity claims when focused
      -- NornicDB evidence shows backend saturation. The default keeps this
      -- cap disabled; source-local drain and conflict-domain fencing still
      -- protect projector overlap and same-scope code graph writes.
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR $7 <= 0 OR (
          SELECT count(*)
          FROM fact_work_items AS semantic_inflight
          WHERE semantic_inflight.stage = 'reducer'
            AND semantic_inflight.domain = 'semantic_entity_materialization'
            AND semantic_inflight.work_item_id <> fact_work_items.work_item_id
            AND semantic_inflight.status IN ('claimed', 'running')
            AND semantic_inflight.claim_until > $1
      ) < $7)
      -- Readiness-gated reducer domains stay pending until every required
      -- canonical-node phase for their domain is visible. The requirement set is
      -- data-shaped so adding a new edge domain adds one bounded row instead of
      -- another correlated predicate branch to the hot claim query.
      AND ` + reducerClaimReadinessGateSQL("fact_work_items", "readiness_req", "readiness_phase") + `
      -- Reducer domains can touch the same graph nodes for a scope. Fence by
      -- explicit conflict key so unrelated graph families can still overlap.
      -- A pending/retrying candidate defers to ANY claimed/running holder on the
      -- same key, NOT only a live (claim_until > $1) one (#4137): the live-lease
      -- unique index keeps at most one claimed/running row per key, so when that
      -- holder's lease expires it must be reclaimed (its own row stays the only
      -- claimable one for the key) rather than have an older pending sibling
      -- raced past it — which would hit the unique index (23505) and wedge the
      -- key. The holder itself has no OTHER claimed/running sibling, so it is not
      -- self-fenced and stays reclaimable.
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS inflight
          WHERE inflight.stage = 'reducer'
            AND inflight.conflict_domain = fact_work_items.conflict_domain
            AND COALESCE(inflight.conflict_key, inflight.scope_id) = COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id)
            AND inflight.work_item_id <> fact_work_items.work_item_id
            AND inflight.status IN ('claimed', 'running')
      )
    ORDER BY updated_at ASC, work_item_id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
),
claimed AS (
    UPDATE fact_work_items AS work
    SET status = 'claimed',
        attempt_count = ` + reducerClaimAttemptCountCaseSQL() + `,
        lease_owner = $3,
        claim_until = $4,
        last_attempt_at = $1,
        updated_at = $1
    FROM candidate
    WHERE work.work_item_id = candidate.work_item_id
    RETURNING
        work.work_item_id,
        work.scope_id,
        work.generation_id,
        work.domain,
        work.attempt_count,
        work.created_at,
        COALESCE(work.visible_at, work.created_at) AS available_at,
        work.payload
)
SELECT
    work_item_id,
    scope_id,
    generation_id,
    domain,
    attempt_count,
    created_at,
    available_at,
    payload
FROM claimed
`
