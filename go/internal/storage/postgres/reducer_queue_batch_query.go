// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

var claimReducerWorkBatchQuery = `
WITH ` + supersedeInactiveReducerGenerationsCTE + `,
` + reducerClaimReadinessRequirementsCTE() + `,
reducer_source_inflight AS (
    SELECT
        COALESCE(NULLIF(BTRIM(payload->>'source_system'), ''), 'unknown') AS reducer_source_system,
        count(*) AS reducer_source_inflight_count
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('claimed', 'running')
      AND claim_until > $1
    GROUP BY reducer_source_system
),
candidate AS (
    SELECT
        work_item_id,
        COALESCE(NULLIF(BTRIM(fact_work_items.payload->>'source_system'), ''), 'unknown') AS reducer_source_system,
        -- Derived search-document catch-up can be expensive; graph truth and
        -- materialization reducers must not wait behind it when both are ready.
        CASE WHEN fact_work_items.domain = 'eshu_search_document' THEN 1 ELSE 0 END AS reducer_domain_priority,
        COALESCE(source_counts.reducer_source_inflight_count, 0) AS reducer_source_inflight_count,
        -- Per-source fairness rank (#4053). Source systems are a bounded
        -- collector vocabulary carried in the reducer intent payload. The
        -- first ready representative for each source sorts ahead of additional
        -- same-source rows, which prevents a noisy older source from filling
        -- every batch while preserving the existing conflict-key fence.
        CASE WHEN fact_work_items.work_item_id = (
            SELECT source_head.work_item_id
            FROM fact_work_items AS source_head
            WHERE source_head.stage = 'reducer'
              AND source_head.domain = fact_work_items.domain
              AND COALESCE(NULLIF(BTRIM(source_head.payload->>'source_system'), ''), 'unknown') =
                  COALESCE(NULLIF(BTRIM(fact_work_items.payload->>'source_system'), ''), 'unknown')
              AND source_head.status IN ('pending', 'retrying', 'claimed', 'running')
              AND NOT EXISTS (
                  SELECT 1
                  FROM superseded_stale_reducer_generations AS source_superseded
                  WHERE source_superseded.work_item_id = source_head.work_item_id
              )
              AND (source_head.visible_at IS NULL OR source_head.visible_at <= $1)
              AND (source_head.claim_until IS NULL OR source_head.claim_until <= $1)
              AND ($2::text[] IS NULL OR source_head.domain = ANY($2::text[]))
              AND ` + reducerClaimReadinessGateSQL("source_head", "source_readiness_req", "source_readiness_phase") + `
            ORDER BY
              CASE WHEN source_head.domain = 'eshu_search_document' THEN 1 ELSE 0 END ASC,
              source_head.updated_at ASC,
              source_head.work_item_id ASC
            LIMIT 1
        ) THEN 0 ELSE 1 END AS reducer_source_fair_rank,
        -- Per-domain fairness rank (#3385, P1 fix #3386). A lane that claims
        -- several domains must not let one high-volume domain with an older,
        -- continuously regenerated backlog monopolize every batch and
        -- indefinitely starve a newer, lower-volume domain (the AWS cloud
        -- producers sat pending behind supply_chain_impact /
        -- package_source_correlation). The rank is the count of same-domain
        -- conflict-group representatives that (a) sort strictly before this
        -- row by (updated_at, work_item_id) AND (b) pass every gate the outer
        -- candidate WHERE applies — superseded-stale exclusion, projector
        -- drain, semantic caps, and readiness. Counting only actually-claimable
        -- representatives prevents inactive-generation rows or readiness-gated
        -- rows from inflating the rank of the first truly-claimable row in a
        -- domain and recreating the starvation. A correlated count is used
        -- instead of row_number() because Postgres forbids window functions in
        -- a FOR UPDATE SKIP LOCKED select. Conflict fencing and the same-group
        -- representative below still pick exactly one safe row per conflict
        -- key, so this only changes which ready rows are taken, never how many
        -- run concurrently per conflict key.
        (
            SELECT count(*)
            FROM fact_work_items AS fair_peer
            WHERE fair_peer.stage = 'reducer'
              AND fair_peer.domain = fact_work_items.domain
              AND fair_peer.status IN ('pending', 'retrying', 'claimed', 'running')
              -- Supersede gate: inactive-generation rows are skipped by the
              -- supersede CTE in the outer query; exclude them here too so
              -- they do not inflate the rank of the first claimable row.
              AND NOT EXISTS (
                  SELECT 1
                  FROM superseded_stale_reducer_generations AS fair_superseded
                  WHERE fair_superseded.work_item_id = fair_peer.work_item_id
              )
              AND (fair_peer.visible_at IS NULL OR fair_peer.visible_at <= $1)
              AND (fair_peer.claim_until IS NULL OR fair_peer.claim_until <= $1)
              AND ($2::text[] IS NULL OR fair_peer.domain = ANY($2::text[]))
              -- Projector drain gate: mirror the outer candidate predicate so
              -- projector-gated peers do not inflate the rank.
              AND ($5 = false OR NOT EXISTS (
                  SELECT 1
                  FROM fact_work_items AS fair_projector_work
                  WHERE fair_projector_work.stage = 'projector'
                    AND fair_projector_work.scope_id = fair_peer.scope_id
                    AND fair_projector_work.status IN ('pending', 'retrying', 'claimed', 'running')
              ))
              AND ($5 = false OR fair_peer.domain <> 'semantic_entity_materialization' OR NOT EXISTS (
                  SELECT 1
                  FROM fact_work_items AS fair_projector_any
                  WHERE fair_projector_any.stage = 'projector'
                    AND fair_projector_any.domain = 'source_local'
                    AND fair_projector_any.status IN ('pending', 'retrying', 'claimed', 'running')
              ))
              AND ($5 = false OR fair_peer.domain <> 'semantic_entity_materialization' OR $6 <= 0 OR (
                  SELECT count(*)
                  FROM fact_work_items AS fair_projector_done
                  WHERE fair_projector_done.stage = 'projector'
                    AND fair_projector_done.domain = 'source_local'
                    AND fair_projector_done.status = 'succeeded'
              ) >= $6)
              AND ($5 = false OR fair_peer.domain <> 'semantic_entity_materialization' OR $7 <= 0 OR (
                  SELECT count(*)
                  FROM fact_work_items AS fair_semantic_inflight
                  WHERE fair_semantic_inflight.stage = 'reducer'
                    AND fair_semantic_inflight.domain = 'semantic_entity_materialization'
                    AND fair_semantic_inflight.work_item_id <> fair_peer.work_item_id
                    AND fair_semantic_inflight.status IN ('claimed', 'running')
                    AND fair_semantic_inflight.claim_until > $1
              ) < $7)
              AND ($5 = false OR fair_peer.domain <> 'semantic_entity_materialization' OR $7 <= 0 OR (
                  SELECT count(*)
                  FROM fact_work_items AS fair_semantic_next
                  WHERE fair_semantic_next.stage = 'reducer'
                    AND fair_semantic_next.domain = 'semantic_entity_materialization'
                    AND fair_semantic_next.status IN ('pending', 'retrying', 'claimed', 'running')
                    AND (fair_semantic_next.visible_at IS NULL OR fair_semantic_next.visible_at <= $1)
                    AND (fair_semantic_next.claim_until IS NULL OR fair_semantic_next.claim_until <= $1)
                    AND (
                        fair_semantic_next.updated_at < fair_peer.updated_at
                        OR (
                            fair_semantic_next.updated_at = fair_peer.updated_at
                            AND fair_semantic_next.work_item_id <= fair_peer.work_item_id
                        )
                    )
              ) <= $7 - (
                  SELECT count(*)
                  FROM fact_work_items AS fair_semantic_inflight2
                  WHERE fair_semantic_inflight2.stage = 'reducer'
                    AND fair_semantic_inflight2.domain = 'semantic_entity_materialization'
                    AND fair_semantic_inflight2.status IN ('claimed', 'running')
                    AND fair_semantic_inflight2.claim_until > $1
              ))
              -- Readiness gate: mirror the outer candidate predicate so
              -- readiness-gated peers do not inflate the rank.
              AND ` + reducerClaimReadinessGateSQL("fair_peer", "fair_readiness_req", "fair_readiness_phase") + `
              -- The fair_peer must be the conflict-group representative for its
              -- own conflict key (same logic as the outer same subquery), and
              -- that representative must also pass supersede + readiness gates.
              AND fair_peer.work_item_id = (
                  SELECT fair_same.work_item_id
                  FROM fact_work_items AS fair_same
                  WHERE fair_same.stage = 'reducer'
                    AND fair_same.conflict_domain = fair_peer.conflict_domain
                    AND COALESCE(fair_same.conflict_key, fair_same.scope_id) = COALESCE(fair_peer.conflict_key, fair_peer.scope_id)
                    AND fair_same.status IN ('pending', 'retrying', 'claimed', 'running')
                    AND NOT EXISTS (
                        SELECT 1
                        FROM superseded_stale_reducer_generations AS fair_same_superseded
                        WHERE fair_same_superseded.work_item_id = fair_same.work_item_id
                    )
                    AND (fair_same.visible_at IS NULL OR fair_same.visible_at <= $1)
                    AND (fair_same.claim_until IS NULL OR fair_same.claim_until <= $1)
                    AND ($2::text[] IS NULL OR fair_same.domain = ANY($2::text[]))
                    AND ` + reducerClaimReadinessGateSQL("fair_same", "fair_same_readiness_req", "fair_same_readiness_phase") + `
                  ORDER BY
                    CASE WHEN fair_same.domain = 'eshu_search_document' THEN 1 ELSE 0 END ASC,
                    fair_same.updated_at ASC,
                    fair_same.work_item_id ASC
                  LIMIT 1
              )
              AND (
                  fair_peer.updated_at < fact_work_items.updated_at
                  OR (
                      fair_peer.updated_at = fact_work_items.updated_at
                      AND fair_peer.work_item_id < fact_work_items.work_item_id
                  )
              )
        ) AS reducer_domain_fair_rank
    FROM fact_work_items
    LEFT JOIN reducer_source_inflight AS source_counts
      ON source_counts.reducer_source_system = COALESCE(NULLIF(BTRIM(fact_work_items.payload->>'source_system'), ''), 'unknown')
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
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR $7 <= 0 OR (
          SELECT count(*)
          FROM fact_work_items AS semantic_next
          WHERE semantic_next.stage = 'reducer'
            AND semantic_next.domain = 'semantic_entity_materialization'
            AND semantic_next.status IN ('pending', 'retrying', 'claimed', 'running')
            AND (semantic_next.visible_at IS NULL OR semantic_next.visible_at <= $1)
            AND (semantic_next.claim_until IS NULL OR semantic_next.claim_until <= $1)
            AND (
                semantic_next.updated_at < fact_work_items.updated_at
                OR (
                    semantic_next.updated_at = fact_work_items.updated_at
                    AND semantic_next.work_item_id <= fact_work_items.work_item_id
                )
            )
      ) <= $7 - (
          SELECT count(*)
          FROM fact_work_items AS semantic_inflight
          WHERE semantic_inflight.stage = 'reducer'
            AND semantic_inflight.domain = 'semantic_entity_materialization'
            AND semantic_inflight.status IN ('claimed', 'running')
            AND semantic_inflight.claim_until > $1
      ))
      -- Readiness-gated reducer domains stay pending until every required
      -- canonical-node phase for their domain is visible. The requirement set is
      -- data-shaped so adding a new edge domain adds one bounded row instead of
      -- another correlated predicate branch to the hot batch claim query.
      AND ` + reducerClaimReadinessGateSQL("fact_work_items", "readiness_req", "readiness_phase") + `
      -- Reducer domains can touch the same graph nodes for a scope. Fence by
      -- explicit conflict key so unrelated graph families can still overlap.
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS inflight
          WHERE inflight.stage = 'reducer'
            AND inflight.conflict_domain = fact_work_items.conflict_domain
            AND COALESCE(inflight.conflict_key, inflight.scope_id) = COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id)
            AND inflight.work_item_id <> fact_work_items.work_item_id
            AND inflight.status IN ('claimed', 'running')
            AND inflight.claim_until > $1
      )
      -- A batch claim must not claim two ready rows for the same conflict key
      -- in one transaction, or the worker pool can race them immediately.
      AND work_item_id = (
          SELECT same.work_item_id
          FROM fact_work_items AS same
          WHERE same.stage = 'reducer'
            AND same.conflict_domain = fact_work_items.conflict_domain
            AND COALESCE(same.conflict_key, same.scope_id) = COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id)
            AND same.status IN ('pending', 'retrying', 'claimed', 'running')
            AND NOT EXISTS (
                SELECT 1
                FROM superseded_stale_reducer_generations AS superseded_same
                WHERE superseded_same.work_item_id = same.work_item_id
            )
            AND (same.visible_at IS NULL OR same.visible_at <= $1)
            AND (same.claim_until IS NULL OR same.claim_until <= $1)
            AND ($2::text[] IS NULL OR same.domain = ANY($2::text[]))
            AND ` + reducerClaimReadinessGateSQL("same", "same_readiness_req", "same_readiness_phase") + `
          ORDER BY
            CASE WHEN same.domain = 'eshu_search_document' THEN 1 ELSE 0 END ASC,
            same.updated_at ASC,
            same.work_item_id ASC
          LIMIT 1
      )
    ORDER BY reducer_domain_priority ASC, reducer_source_inflight_count ASC, reducer_source_fair_rank ASC, reducer_domain_fair_rank ASC, updated_at ASC, work_item_id ASC
    LIMIT $8
    FOR UPDATE SKIP LOCKED
),
claimed AS (
    UPDATE fact_work_items AS work
    SET status = 'claimed',
        attempt_count = CASE
            WHEN work.status = 'retrying' AND work.failure_class = 'secrets_iam_endpoint_not_ready' THEN work.attempt_count
            ELSE work.attempt_count + 1
        END,
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
