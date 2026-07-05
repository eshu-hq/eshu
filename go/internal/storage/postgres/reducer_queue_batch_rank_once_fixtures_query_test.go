// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// oldReducerBatchCandidateSelectQuery is the pre-rewrite (#3624 Track 2)
// candidate SELECT frozen verbatim as a read-only reference, with only the
// data-modifying tail (FOR UPDATE SKIP LOCKED claim lock and the "claimed"
// UPDATE) removed so it can run side-effect-free against the same fixture as
// the real batch-claim query. Every predicate and CTE above the removed tail
// is byte-identical to the query this package shipped before the rank-once
// window rewrite (see git history of reducer_queue_batch_query.go). This is
// the "OLD" side of the differential 0/0 proof
// (TestClaimBatchRankOnceRewriteMatchesPreRewriteCandidateSetAndOrder) that
// the rewrite selects the identical candidate set, in the identical order, as
// the correlated-subquery implementation it replaces.
var oldReducerBatchCandidateSelectQuery = `
WITH ` + reducerClaimReadinessRequirementsCTE() + `,
` + supersedeInactiveReducerGenerationsCTE + `,
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
        CASE WHEN fact_work_items.domain = 'eshu_search_document' THEN 1 ELSE 0 END AS reducer_domain_priority,
        COALESCE(source_counts.reducer_source_inflight_count, 0) AS reducer_source_inflight_count,
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
        (
            SELECT count(*)
            FROM fact_work_items AS fair_peer
            WHERE fair_peer.stage = 'reducer'
              AND fair_peer.domain = fact_work_items.domain
              AND fair_peer.status IN ('pending', 'retrying', 'claimed', 'running')
              AND NOT EXISTS (
                  SELECT 1
                  FROM superseded_stale_reducer_generations AS fair_superseded
                  WHERE fair_superseded.work_item_id = fair_peer.work_item_id
              )
              AND (fair_peer.visible_at IS NULL OR fair_peer.visible_at <= $1)
              AND (fair_peer.claim_until IS NULL OR fair_peer.claim_until <= $1)
              AND ($2::text[] IS NULL OR fair_peer.domain = ANY($2::text[]))
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
              AND ` + reducerClaimReadinessGateSQL("fair_peer", "fair_readiness_req", "fair_readiness_phase") + `
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
        ) AS reducer_domain_fair_rank,
        fact_work_items.updated_at AS updated_at
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
      AND ($5 = false OR NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS projector_work
          WHERE projector_work.stage = 'projector'
            AND projector_work.scope_id = fact_work_items.scope_id
            AND projector_work.status IN ('pending', 'retrying', 'claimed', 'running')
      ))
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS projector_any
          WHERE projector_any.stage = 'projector'
            AND projector_any.domain = 'source_local'
            AND projector_any.status IN ('pending', 'retrying', 'claimed', 'running')
      ))
      AND ($5 = false OR domain <> 'semantic_entity_materialization' OR $6 <= 0 OR (
          SELECT count(*)
          FROM fact_work_items AS projector_done
          WHERE projector_done.stage = 'projector'
            AND projector_done.domain = 'source_local'
            AND projector_done.status = 'succeeded'
      ) >= $6)
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
      AND ` + reducerClaimReadinessGateSQL("fact_work_items", "readiness_req", "readiness_phase") + `
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
            CASE WHEN same.status IN ('claimed', 'running') THEN 0 ELSE 1 END ASC,
            CASE WHEN same.domain = 'eshu_search_document' THEN 1 ELSE 0 END ASC,
            same.updated_at ASC,
            same.work_item_id ASC
          LIMIT 1
      )
)
SELECT work_item_id
FROM candidate
-- $3/$4 (lease_owner/claim_until) belong only to the claimed UPDATE this
-- read-only reference query intentionally omits; the harmless cast below
-- keeps the frozen $1..$8 positional parameter list identical to the real
-- claimReducerWorkBatchQuery call site (so this test calls both queries with
-- the exact same args slice) without giving Postgres an untyped parameter it
-- cannot infer a type for.
WHERE ($3::text IS NULL OR TRUE) AND ($4::timestamptz IS NULL OR TRUE)
ORDER BY reducer_domain_priority ASC, reducer_source_inflight_count ASC, reducer_source_fair_rank ASC, reducer_domain_fair_rank ASC, updated_at ASC, work_item_id ASC
LIMIT $8
`
