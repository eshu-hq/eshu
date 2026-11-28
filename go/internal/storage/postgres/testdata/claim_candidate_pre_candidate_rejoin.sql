-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- Frozen pre-change reference for #10916 (candidate rejoin removal): the
-- read-only candidate SELECT derived from claimReducerWorkBatchQuery at
-- the #10916 base commit (before the candidate CTE dropped its JOIN back
-- to fact_work_items), via deriveReadOnlyCandidateSelect.
-- Machine-derived, never hand-copied: a temporary dump test ran the
-- derivation in a clean base worktree and wrote these bytes verbatim.
-- The "new" side of the differential always derives from the shipped
-- query at test time, so only the part before the candidate CTE moves, when
-- the stale gate does.
-- #11234 re-derivation: the text before the candidate CTE (the stale-gate
-- CTEs) follows the shipped E2 gate (stale_gate_rows and the lateral scope
-- join), written by a temporary dump test that ran the derivation in this
-- tree; the candidate CTE and everything after it are still the frozen
-- pre-#10916 bytes with the rejoin intact.


WITH reducer_claim_readiness_requirements(domain, keyspace, phase, acceptance_unit_source, acceptance_unit_prefix) AS (
    VALUES
        ('aws_relationship_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('azure_relationship_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('gcp_relationship_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('workload_cloud_relationship_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('observability_coverage_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('iam_can_assume_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('iam_escalation_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('iam_can_perform_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('s3_logs_to_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('s3_external_principal_grant_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('rds_posture_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('iam_instance_profile_role_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('ec2_internet_exposure_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('ec2_instance_identity_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('s3_internet_exposure_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('kubernetes_correlation_materialization', 'kubernetes_workload_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('security_group_reachability_materialization', 'security_group_rule_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('security_group_reachability_materialization', 'security_group_endpoint_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('security_group_reachability_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', ''),
        ('ec2_uses_profile_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'scope_prefix', 'ec2_instance_node_materialization:'),
        ('ec2_uses_profile_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'scope_prefix', 'aws_resource_materialization:'),
        ('ec2_block_device_kms_posture_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'scope_prefix', 'ec2_instance_node_materialization:'),
        ('ec2_block_device_kms_posture_materialization', 'cloud_resource_uid', 'canonical_nodes_committed', 'scope_prefix', 'aws_resource_materialization:')
),
stale_gate_rows AS MATERIALIZED (
    SELECT work.work_item_id,
           work.scope_id,
           work.generation_id,
           work.stage,
           work.domain,
           work.status,
           work.payload
    FROM fact_work_items AS work
    JOIN ingestion_scopes AS work_scope ON work_scope.scope_id = work.scope_id
    WHERE work.stage = 'reducer'
      AND work.status IN ('pending', 'retrying')
      AND ($2::text[] IS NULL OR work.domain = ANY($2::text[]))
      AND work.generation_id IS DISTINCT FROM work_scope.active_generation_id
),
stale_reducer_candidates AS MATERIALIZED (
    SELECT stale.work_item_id
    FROM stale_gate_rows AS stale
    CROSS JOIN LATERAL (
        SELECT scope.scope_id,
               scope.active_generation_id,
               active_generation.generation_id AS present_active_generation_id,
               active_generation.ingested_at AS active_ingested_at
        FROM ingestion_scopes AS scope
        LEFT JOIN scope_generations AS active_generation
          ON active_generation.generation_id = scope.active_generation_id
         AND active_generation.scope_id = scope.scope_id
        WHERE scope.scope_id = stale.scope_id
        OFFSET 0
    ) AS scope
    JOIN scope_generations AS stale_generation
      ON stale_generation.scope_id = stale.scope_id
     AND stale_generation.generation_id = stale.generation_id
    WHERE stale.stage = 'reducer'
      AND stale.status IN ('pending', 'retrying')
      AND ($2::text[] IS NULL OR stale.domain = ANY($2::text[]))
      AND (
        (
          scope.present_active_generation_id IS NOT NULL
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
        )
        OR (
          scope.active_generation_id IS NULL
          AND stale_generation.status = 'superseded'
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
),
reducer_source_inflight AS MATERIALIZED (
    SELECT
        COALESCE(NULLIF(BTRIM(payload->>'source_system'), ''), 'unknown') AS reducer_source_system,
        count(*) AS reducer_source_inflight_count
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('claimed', 'running')
      AND claim_until > $1
    GROUP BY reducer_source_system
),
-- #8024: per-domain inflight tally feeding the idle-domain ordering key in
-- the locked CTE. Same scan shape as reducer_source_inflight above (claimed/running
-- with a live lease), grouped by domain instead of source_system, so a ready
-- row of a domain with no inflight work sorts ahead of an older same-source
-- backlog that is already being worked.
reducer_domain_inflight AS MATERIALIZED (
    SELECT
        domain AS reducer_domain,
        count(*) AS reducer_domain_inflight_count
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('claimed', 'running')
      AND claim_until > $1
    GROUP BY domain
),
projector_live_scopes AS MATERIALIZED (
    SELECT DISTINCT scope_id
    FROM fact_work_items
    WHERE stage = 'projector'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
),
semantic_scalars AS MATERIALIZED (
    SELECT
        EXISTS (
            SELECT 1 FROM fact_work_items
            WHERE stage = 'projector' AND domain = 'source_local'
              AND status IN ('pending', 'retrying', 'claimed', 'running')
        ) AS proj_any_live,
        (SELECT count(*) FROM fact_work_items
          WHERE stage = 'projector' AND domain = 'source_local'
            AND status = 'succeeded') AS proj_done,
        (SELECT count(*) FROM fact_work_items
          WHERE stage = 'reducer' AND domain = 'semantic_entity_materialization'
            AND status IN ('claimed', 'running') AND claim_until > $1) AS sem_inflight
),
semantic_ranked AS MATERIALIZED (
    SELECT work_item_id,
           row_number() OVER (ORDER BY updated_at ASC, work_item_id ASC) AS sem_rank
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND domain = 'semantic_entity_materialization'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND (visible_at IS NULL OR visible_at <= $1 OR (status = 'pending' AND visible_at <= clock_timestamp()))
      AND (claim_until IS NULL OR claim_until <= $1)
),
readiness_missing AS MATERIALIZED (
    SELECT w.work_item_id
    FROM fact_work_items AS w
    WHERE w.stage = 'reducer'
      AND w.status IN ('pending', 'retrying', 'claimed', 'running')
      AND EXISTS (
          SELECT 1
          FROM reducer_claim_readiness_requirements AS readiness_req
          WHERE readiness_req.domain = w.domain
            AND NOT EXISTS (
                SELECT 1
                FROM graph_projection_phase_state AS readiness_phase
                WHERE readiness_phase.scope_id = w.scope_id
                  AND readiness_phase.acceptance_unit_id = CASE readiness_req.acceptance_unit_source
                      WHEN 'payload_entity_key' THEN COALESCE(NULLIF(w.payload->>'entity_key', ''), w.scope_id)
                      WHEN 'scope_prefix' THEN readiness_req.acceptance_unit_prefix || w.scope_id
                      ELSE w.scope_id
                      END
                  AND readiness_phase.source_run_id = w.generation_id
                  AND readiness_phase.generation_id = w.generation_id
                  AND readiness_phase.keyspace = readiness_req.keyspace
                  AND readiness_phase.phase = readiness_req.phase
            )
      )
),
-- base: every shared gate (readiness, projector drain, semantic caps) read
-- through JOINs from the precomputed gate CTEs above (#10565 step 1), which
-- evaluate each gate once per claim. The per-row correlated SubPlans they
-- replace used to re-run once per candidate row.
base AS MATERIALIZED (
    SELECT
        fact_work_items.work_item_id,
        fact_work_items.domain,
        fact_work_items.scope_id,
        fact_work_items.generation_id,
        fact_work_items.conflict_domain,
        COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id) AS ckey,
        fact_work_items.updated_at,
        fact_work_items.status,
        COALESCE(NULLIF(BTRIM(fact_work_items.payload->>'source_system'), ''), 'unknown') AS reducer_source_system,
        CASE WHEN fact_work_items.domain = 'eshu_search_document' THEN 1 ELSE 0 END AS is_search_doc,
        CASE WHEN fact_work_items.status IN ('claimed', 'running') THEN 0 ELSE 1 END AS claimed_running_first,
        (readiness_missing.work_item_id IS NULL) AS readiness_ok,
        ($5 = false OR projector_live_scopes.scope_id IS NULL) AS projector_ok,
        CASE WHEN fact_work_items.domain <> 'semantic_entity_materialization' THEN true
        ELSE (
            ($5 = false OR NOT semantic_scalars.proj_any_live)
            AND ($5 = false OR $6 <= 0 OR semantic_scalars.proj_done >= $6)
            AND ($5 = false OR $7 <= 0 OR semantic_scalars.sem_inflight < $7)
            AND ($5 = false OR $7 <= 0 OR COALESCE(semantic_ranked.sem_rank, 0) <= $7 - semantic_scalars.sem_inflight)
        ) END AS semantic_ok
    FROM fact_work_items
    CROSS JOIN semantic_scalars
    LEFT JOIN projector_live_scopes
      ON projector_live_scopes.scope_id = fact_work_items.scope_id
    LEFT JOIN readiness_missing
      ON readiness_missing.work_item_id = fact_work_items.work_item_id
    LEFT JOIN semantic_ranked
      ON semantic_ranked.work_item_id = fact_work_items.work_item_id
    WHERE stage = 'reducer'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      -- #10863: stale-generation rows are fenced read-only here; the
      -- periodic sweep terminalizes them out of band, so a fenced row
      -- stays pending (diagnosable, replayable) until it runs.
      AND NOT EXISTS (
          SELECT 1
          FROM stale_reducer_candidates AS superseded
          WHERE superseded.work_item_id = fact_work_items.work_item_id
      )
      -- #6828: visibility runs on the clock that stamped the row (same fix
      -- as the single-claim query): the $1 arm covers app-stamped retry
      -- rows and simulated clocks, the pending arm covers trigger-stamped
      -- reopened rows on the database clock.
      AND (visible_at IS NULL OR visible_at <= $1 OR (status = 'pending' AND visible_at <= clock_timestamp()))
      AND (claim_until IS NULL OR claim_until <= $1)
      AND ($2::text[] IS NULL OR domain = ANY($2::text[]))
),
-- live_fence_keys: the once-per-claim live-lease conflict-key set
-- (#10630 step 2). Declared here, ahead of its only reader `candidate`,
-- because the gate precomputes above feed `base` while this set feeds
-- the fence below; WITH order only requires it before `candidate`.
live_fence_keys AS MATERIALIZED (
    SELECT DISTINCT conflict_domain, COALESCE(conflict_key, scope_id) AS ckey
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('claimed', 'running')
      AND claim_until > $1
),
-- reps_ranked / reps: representative and fairness windows computed once over
-- the whole readiness-gated candidate set, replacing the correlated
-- "same"/"fair_same"/"fair_peer" subqueries that used to re-derive this per
-- row. Two CTE levels are required: Postgres forbids nesting a window
-- function call (row_number()) inside another window aggregate in the same
-- SELECT list, so fair_same_rn must be materialized in reps_ranked before
-- reps can build the conditional running-sum peer_flag/fair-rank on top of
-- it.
reps_ranked AS MATERIALIZED (
    SELECT
        base.*,
        row_number() OVER w_same     AS same_rn,
        row_number() OVER w_fairsame AS fair_same_rn
    FROM base
    WHERE readiness_ok
    WINDOW
        w_same     AS (PARTITION BY conflict_domain, ckey
                       ORDER BY claimed_running_first ASC, is_search_doc ASC, updated_at ASC, work_item_id ASC),
        w_fairsame AS (PARTITION BY conflict_domain, ckey
                       ORDER BY is_search_doc ASC, updated_at ASC, work_item_id ASC)
),
reps AS MATERIALIZED (
    SELECT
        reps_ranked.*,
        CASE WHEN fair_same_rn = 1 AND projector_ok AND semantic_ok THEN 1 ELSE 0 END AS peer_flag,
        (
            sum(CASE WHEN fair_same_rn = 1 AND projector_ok AND semantic_ok THEN 1 ELSE 0 END) OVER (
                PARTITION BY domain
                ORDER BY updated_at ASC, work_item_id ASC
                ROWS UNBOUNDED PRECEDING
            )
            - CASE WHEN fair_same_rn = 1 AND projector_ok AND semantic_ok THEN 1 ELSE 0 END
        ) AS reducer_domain_fair_rank
    FROM reps_ranked
),
-- representative fence: the #4137 conflict-key representative is the
-- reps.same_rn = 1 row (one per (conflict_domain, ckey), ranked once by the
-- w_same window). The candidate CTE below expresses the "this row IS its
-- conflict key's representative" fence directly as reps.same_rn = 1 — no
-- separate same-representative CTE and no correlated ORDER BY ... LIMIT 1
-- subquery, which was the #3624 O(N^2) per-candidate re-scan and is removed.
-- The live-lease fence below it reads the once-per-claim live_fence_keys
-- set (#10630 step 2) instead of re-probing fact_work_items per row.
-- source_heads: unchanged in shape from the pre-rewrite query (cheap; one
-- row_number() pass over reps), kept as its own CTE so the planner does not
-- re-inline it into candidate.
source_heads AS MATERIALIZED (
    SELECT work_item_id
    FROM (
        SELECT
            work_item_id,
            row_number() OVER (
                PARTITION BY domain, reducer_source_system
                ORDER BY is_search_doc ASC, updated_at ASC, work_item_id ASC
            ) AS src_rn
        FROM reps
    ) sh
    WHERE sh.src_rn = 1
),
candidate AS (
    SELECT
        fact_work_items.work_item_id,
        CASE WHEN fact_work_items.domain = 'eshu_search_document' THEN 1 ELSE 0 END AS reducer_domain_priority,
        COALESCE(source_counts.reducer_source_inflight_count, 0) AS reducer_source_inflight_count,
        CASE WHEN fact_work_items.work_item_id = source_heads.work_item_id THEN 0 ELSE 1 END AS reducer_source_fair_rank,
        reps.reducer_domain_fair_rank AS reducer_domain_fair_rank,
        COALESCE(domain_counts.reducer_domain_inflight_count, 0) AS reducer_domain_inflight_count,
        fact_work_items.updated_at AS updated_at
    FROM reps
    JOIN fact_work_items ON fact_work_items.work_item_id = reps.work_item_id
    LEFT JOIN reducer_source_inflight AS source_counts
      ON source_counts.reducer_source_system = reps.reducer_source_system
    LEFT JOIN reducer_domain_inflight AS domain_counts
      ON domain_counts.reducer_domain = reps.domain
    LEFT JOIN source_heads
      ON source_heads.work_item_id = reps.work_item_id
    WHERE reps.same_rn = 1
      AND reps.projector_ok
      AND reps.semantic_ok
      -- #10630 step 2: the #4137 live-lease fence reads the once-per-claim
      -- live_fence_keys set instead of re-probing fact_work_items once per
      -- representative. The key comparison is the shipped one verbatim; the
      -- shipped work_item_id self-exclusion is vacuous here because base
      -- pre-filters to unleased rows (see reducerClaimLiveFenceKeysCTE).
      AND NOT EXISTS (
          SELECT 1
          FROM live_fence_keys
          WHERE live_fence_keys.conflict_domain = fact_work_items.conflict_domain
            AND live_fence_keys.ckey = COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id)
      )
)
SELECT work_item_id
FROM candidate
WHERE ($3::text IS NULL OR TRUE) AND ($4::timestamptz IS NULL OR TRUE)
ORDER BY reducer_domain_priority ASC, reducer_source_inflight_count ASC, reducer_source_fair_rank ASC, reducer_domain_fair_rank ASC, reducer_domain_inflight_count ASC, updated_at ASC, work_item_id ASC
LIMIT $8
