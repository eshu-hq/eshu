-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- Frozen pre-change reference for #11234 (the differential and the
-- byte-identical guard): reducerClaimStaleGateCTEs() at commit 662083f413
-- (before the join-order fence). Machine-derived, never hand-copied: a
-- temporary dump test ran at that commit and wrote these bytes verbatim
-- (sha256 30a3fd23ee77b417d8c6cd5a981dfae7953c30749f8552953f41b766a38b3aeb,
-- checked by staleGate11234PreGate). The shipped side always derives from
-- the constant at test time, so this file only moves if history is
-- rewritten.
claim_active_generations AS MATERIALIZED (
    SELECT scope.scope_id,
           scope.active_generation_id,
           active_generation.ingested_at AS active_ingested_at
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS active_generation
      ON active_generation.generation_id = scope.active_generation_id
     AND active_generation.scope_id = scope.scope_id
),
stale_reducer_candidates AS MATERIALIZED (
    SELECT stale.work_item_id
    FROM fact_work_items AS stale
    JOIN claim_active_generations AS scope ON scope.scope_id = stale.scope_id
    JOIN scope_generations AS stale_generation
      ON stale_generation.scope_id = stale.scope_id
     AND stale_generation.generation_id = stale.generation_id
    WHERE stale.stage = 'reducer'
      AND stale.status IN ('pending', 'retrying')
      AND ($2::text[] IS NULL OR stale.domain = ANY($2::text[]))
      AND stale.generation_id <> scope.active_generation_id
      AND (
        stale_generation.ingested_at < scope.active_ingested_at
        OR (
          stale_generation.ingested_at = scope.active_ingested_at
          AND stale_generation.generation_id < scope.active_generation_id
        )
      )
      AND (
        NOT EXISTS (
          SELECT 1
          FROM reducer_claim_readiness_requirements AS supersede_readiness_req
          WHERE supersede_readiness_req.domain = stale.domain
            AND NOT EXISTS (
                SELECT 1
                FROM graph_projection_phase_state AS supersede_readiness_phase
                WHERE supersede_readiness_phase.scope_id = stale.scope_id
                  AND supersede_readiness_phase.acceptance_unit_id = CASE supersede_readiness_req.acceptance_unit_source
                      WHEN 'payload_entity_key' THEN COALESCE(NULLIF(stale.payload->>'entity_key', ''), stale.scope_id)
                      WHEN 'scope_prefix' THEN supersede_readiness_req.acceptance_unit_prefix || stale.scope_id
                      ELSE stale.scope_id
                      END
                  AND supersede_readiness_phase.source_run_id = stale.generation_id
                  AND supersede_readiness_phase.generation_id = stale.generation_id
                  AND supersede_readiness_phase.keyspace = supersede_readiness_req.keyspace
                  AND supersede_readiness_phase.phase = supersede_readiness_req.phase
            )
      )
        OR NOT EXISTS (
          SELECT 1
          FROM reducer_claim_readiness_requirements AS supersede_active_readiness_req
          WHERE supersede_active_readiness_req.domain = stale.domain
            AND NOT EXISTS (
                SELECT 1
                FROM graph_projection_phase_state AS supersede_active_readiness_phase
                WHERE supersede_active_readiness_phase.scope_id = stale.scope_id
                  AND supersede_active_readiness_phase.acceptance_unit_id = CASE supersede_active_readiness_req.acceptance_unit_source
                      WHEN 'payload_entity_key' THEN COALESCE(NULLIF(stale.payload->>'entity_key', ''), stale.scope_id)
                      WHEN 'scope_prefix' THEN supersede_active_readiness_req.acceptance_unit_prefix || stale.scope_id
                      ELSE stale.scope_id
                      END
                  AND supersede_active_readiness_phase.source_run_id = scope.active_generation_id
                  AND supersede_active_readiness_phase.generation_id = scope.active_generation_id
                  AND supersede_active_readiness_phase.keyspace = supersede_active_readiness_req.keyspace
                  AND supersede_active_readiness_phase.phase = supersede_active_readiness_req.phase
            )
      )
      )
      AND NOT (
        stale.domain = 'supply_chain_impact'
        AND COALESCE(stale.payload->>'trigger', '') <> 'manifest_dependency'
        AND (
          COALESCE(stale.payload->>'fanout_owed', '') = 'true'
          OR CASE
               WHEN jsonb_typeof(stale.payload->'fanout_remaining') = 'array'
               THEN jsonb_array_length(stale.payload->'fanout_remaining') > 0
               ELSE FALSE
             END
        )
      )
      AND (stale.domain <> 'crossplane_satisfied_by_materialization'
           OR EXISTS (
             SELECT 1
             FROM fact_work_items AS carrier
             JOIN scope_generations AS carrier_generation
               ON carrier_generation.scope_id = carrier.scope_id
              AND carrier_generation.generation_id = carrier.generation_id
             WHERE carrier.stage = 'reducer'
               AND carrier.domain = 'crossplane_satisfied_by_materialization'
               AND carrier.scope_id = stale.scope_id
               AND carrier.work_item_id <> stale.work_item_id
               AND carrier.status IN ('pending', 'retrying', 'claimed', 'running')
               AND (carrier_generation.ingested_at > stale_generation.ingested_at
                    OR (carrier_generation.ingested_at = stale_generation.ingested_at
                        AND carrier_generation.generation_id > stale_generation.generation_id))))
)
