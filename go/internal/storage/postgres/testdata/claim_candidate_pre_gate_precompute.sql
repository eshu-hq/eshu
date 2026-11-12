-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- Frozen pre-change reference for #10565 step 1 (C-1 differential): the
-- read-only candidate SELECT derived from claimReducerWorkBatchQuery at
-- commit 8a37c6386d (before the precomputed-gate CTEs), via
-- deriveReadOnlyCandidateSelect. Machine-derived, never hand-copied: a
-- temporary dump test ran the derivation in a clean HEAD worktree and wrote
-- these bytes verbatim. The "new" side of the differential always derives
-- from the shipped query at test time, so this file only moves if history
-- is rewritten.
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

superseded_stale_reducer_locked AS (
    SELECT stale.work_item_id,
           stale.scope_id,
           stale.domain,
           stale.status,
           stale.generation_id,
           stale_generation.ingested_at AS generation_ingested_at
    FROM fact_work_items AS stale,
         ingestion_scopes AS scope,
         scope_generations AS stale_generation,
         scope_generations AS active_generation
    WHERE (stale.stage = 'reducer'
      AND stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')
      AND ($2::text[] IS NULL OR stale.domain = ANY($2::text[]))
      -- #10106/#10269: no domain is exempt from LOCKING here. Crossplane
      -- SATISFIED_BY rows are scope-keyed replays that rebind to the
      -- active generation at execute time, so terminalizing a sole stale
      -- row would drop debt a quiet delta never re-derives; the UPDATE
      -- below therefore supersedes a locked Crossplane row only when the
      -- locked set also contains a newer pending/retrying sibling for
      -- the same scope to carry that debt (see there). Locking every
      -- stale candidate in this one ordered pass is what makes that
      -- check race-free: a sibling claimed between snapshot and lock
      -- fails this predicate's row-self status qual on EvalPlanQual
      -- recheck and never enters the locked set.
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
)
       -- #10269 carrier arm: also lock a pending/retrying Crossplane row
       -- on the scope's ACTIVE generation when an older stale sibling is
       -- queued, so the UPDATE can coalesce that sibling into it. Stale-
       -- generation carriers enter through the predicate above already;
       -- only the active generation needs this arm, and the UPDATE's
       -- generation recheck keeps these rows themselves un-superseded.
       -- One scan, one LockRows pass in work_item_id order: the #8205
       -- global lock order is unchanged.
       OR (stale.stage = 'reducer'
           AND stale.domain = 'crossplane_satisfied_by_materialization'
           AND stale.status IN ('pending', 'retrying')
           AND scope.scope_id = stale.scope_id
           AND stale_generation.scope_id = stale.scope_id
           AND stale_generation.generation_id = stale.generation_id
           AND scope.active_generation_id = active_generation.generation_id
           AND active_generation.scope_id = stale.scope_id
           AND stale.generation_id = scope.active_generation_id
           AND EXISTS (
             SELECT 1
             FROM fact_work_items AS older_sibling
             JOIN scope_generations AS older_generation
               ON older_generation.scope_id = older_sibling.scope_id
              AND older_generation.generation_id = older_sibling.generation_id
             WHERE older_sibling.stage = 'reducer'
               AND older_sibling.domain = 'crossplane_satisfied_by_materialization'
               AND ($2::text[] IS NULL OR older_sibling.domain = ANY($2::text[]))
               AND older_sibling.scope_id = stale.scope_id
               AND older_sibling.work_item_id <> stale.work_item_id
               AND older_sibling.status IN ('pending', 'retrying', 'failed', 'dead_letter')
               AND (older_generation.ingested_at < stale_generation.ingested_at
                    OR (older_generation.ingested_at = stale_generation.ingested_at
                        AND older_generation.generation_id < stale_generation.generation_id))))
    ORDER BY stale.work_item_id COLLATE "C"
    FOR NO KEY UPDATE OF stale
),
-- Newest locked pending/retrying Crossplane carrier per scope, derived
-- once from the locked set so the UPDATE probes it per row instead of
-- re-scanning the locked set for every Crossplane candidate (measured:
-- a per-row EXISTS over the locked CTE cost ~0.13 ms x carrier
-- candidates on a 2,600-row sweep). AS MATERIALIZED is load-bearing:
-- the inlined form re-ran the DISTINCT ON as a per-row subquery scan
-- under the UPDATE's left join (same probe).
superseded_crossplane_newest_carrier AS MATERIALIZED (
    SELECT DISTINCT ON (carrier.scope_id)
           carrier.scope_id,
           carrier.generation_ingested_at,
           carrier.generation_id
    FROM superseded_stale_reducer_locked AS carrier
    WHERE carrier.domain = 'crossplane_satisfied_by_materialization'
      AND carrier.status IN ('pending', 'retrying')
    ORDER BY carrier.scope_id,
             carrier.generation_ingested_at DESC,
             carrier.generation_id DESC
),
superseded_stale_reducer_generations AS (
    UPDATE fact_work_items AS stale
    SET status = 'superseded',
        container_image_identity_v2_authorized_status = 'superseded',
        container_image_identity_v3_authorized_status = CASE
            WHEN stale.container_image_identity_v3_required THEN 'superseded'
            ELSE ''
        END,
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = NULL,
        next_attempt_at = NULL,
        updated_at = $1,
        failure_class = 'reducer_superseded_by_newer_active_generation',
        failure_message = 'reducer work superseded by newer active generation',
        failure_details = (jsonb_build_object(
            'reason', 'inactive_generation',
            'scope_id', stale.scope_id,
            'work_item_id', stale.work_item_id,
            'generation_id', stale.generation_id,
            'active_generation_id', scope.active_generation_id,
            'domain', stale.domain
        ) || (CASE
        WHEN stale.status IN ('failed', 'dead_letter')
          OR NULLIF(BTRIM(COALESCE(stale.failure_class, '')), '') IS NOT NULL
          OR NULLIF(BTRIM(COALESCE(stale.failure_message, '')), '') IS NOT NULL
          OR NULLIF(BTRIM(COALESCE(stale.failure_details, '')), '') IS NOT NULL
        THEN jsonb_build_object('prior_failure', jsonb_build_object(
            'status', stale.status,
            'failure_class', stale.failure_class,
            'failure_message', stale.failure_message,
            'failure_details', stale.failure_details,
            'updated_at', stale.updated_at
        ))
        ELSE '{}'::jsonb
    END))::text
    FROM ingestion_scopes AS scope
    WHERE stale.work_item_id = ANY(ARRAY(SELECT work_item_id FROM superseded_stale_reducer_locked))
      AND scope.scope_id = stale.scope_id
      AND stale.stage = 'reducer'
      AND stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')
      -- The lock step also locks active-generation Crossplane carriers
      -- (#10269 arm above); they are never themselves superseded. Every
      -- other locked row passed the lock predicate's older-generation
      -- guard, so this id check is the whole generation recheck.
      AND stale.generation_id <> scope.active_generation_id
      -- #10269: a Crossplane row is superseded only when the locked set
      -- holds a newer carrier for the same scope: same domain, strictly
      -- newer generation in activation order (ingested_at, then
      -- generation_id — never updated_at, which claim/retry stamps
      -- rewrite), with the status the EvalPlanQual recheck confirmed at
      -- lock time still pending/retrying. The carrier set is
      -- conservative by design: claimed/running siblings are never in
      -- the locked set (an in-flight carrier can still dead-letter, and
      -- superseded is unreplayable, so pruning against it could destroy
      -- the only fallback), and a failed/dead_letter newest sibling
      -- absorbs nothing, so the older row survives to claim and rebind
      -- (#10106). A snapshot EXISTS over the live table was proven live
      -- to destroy that fallback when the carrier was claimed between
      -- snapshot and lock; membership in the lock step's output is the
      -- race-free form of the same check. Chain transitivity: a middle
      -- row is both carrier (for older rows) and superseded (against a
      -- newer carrier) in this one update.
      AND (stale.domain <> 'crossplane_satisfied_by_materialization'
           OR EXISTS (
             SELECT 1
             FROM superseded_crossplane_newest_carrier AS carrier
             JOIN scope_generations AS carrier_stale_generation
               ON carrier_stale_generation.scope_id = stale.scope_id
              AND carrier_stale_generation.generation_id = stale.generation_id
             WHERE carrier.scope_id = stale.scope_id
               AND (carrier.generation_ingested_at > carrier_stale_generation.ingested_at
                    OR (carrier.generation_ingested_at = carrier_stale_generation.ingested_at
                        AND carrier.generation_id > carrier_stale_generation.generation_id))))
    RETURNING stale.work_item_id
)
,
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
-- base: every shared gate (readiness, projector drain, semantic caps)
-- evaluated exactly once per candidate row. Every predicate mirrors the
-- outer WHERE clauses these gates used to duplicate at each correlated call
-- site (fair_peer, fair_same, source_head).
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
        NOT EXISTS (
          SELECT 1
          FROM reducer_claim_readiness_requirements AS readiness_req
          WHERE readiness_req.domain = fact_work_items.domain
            AND NOT EXISTS (
                SELECT 1
                FROM graph_projection_phase_state AS readiness_phase
                WHERE readiness_phase.scope_id = fact_work_items.scope_id
                  AND readiness_phase.acceptance_unit_id = CASE readiness_req.acceptance_unit_source
                      WHEN 'payload_entity_key' THEN COALESCE(NULLIF(fact_work_items.payload->>'entity_key', ''), fact_work_items.scope_id)
                      WHEN 'scope_prefix' THEN readiness_req.acceptance_unit_prefix || fact_work_items.scope_id
                      ELSE fact_work_items.scope_id
                      END
                  AND readiness_phase.source_run_id = fact_work_items.generation_id
                  AND readiness_phase.generation_id = fact_work_items.generation_id
                  AND readiness_phase.keyspace = readiness_req.keyspace
                  AND readiness_phase.phase = readiness_req.phase
            )
      ) AS readiness_ok,
        ($5 = false OR NOT EXISTS (
            SELECT 1
            FROM fact_work_items AS projector_work
            WHERE projector_work.stage = 'projector'
              AND projector_work.scope_id = fact_work_items.scope_id
              AND projector_work.status IN ('pending', 'retrying', 'claimed', 'running')
        )) AS projector_ok,
        (
            ($5 = false OR fact_work_items.domain <> 'semantic_entity_materialization' OR NOT EXISTS (
                SELECT 1
                FROM fact_work_items AS projector_any
                WHERE projector_any.stage = 'projector'
                  AND projector_any.domain = 'source_local'
                  AND projector_any.status IN ('pending', 'retrying', 'claimed', 'running')
            ))
            AND ($5 = false OR fact_work_items.domain <> 'semantic_entity_materialization' OR $6 <= 0 OR (
                SELECT count(*)
                FROM fact_work_items AS projector_done
                WHERE projector_done.stage = 'projector'
                  AND projector_done.domain = 'source_local'
                  AND projector_done.status = 'succeeded'
            ) >= $6)
            AND ($5 = false OR fact_work_items.domain <> 'semantic_entity_materialization' OR $7 <= 0 OR (
                SELECT count(*)
                FROM fact_work_items AS semantic_inflight
                WHERE semantic_inflight.stage = 'reducer'
                  AND semantic_inflight.domain = 'semantic_entity_materialization'
                  AND semantic_inflight.work_item_id <> fact_work_items.work_item_id
                  AND semantic_inflight.status IN ('claimed', 'running')
                  AND semantic_inflight.claim_until > $1
            ) < $7)
            AND ($5 = false OR fact_work_items.domain <> 'semantic_entity_materialization' OR $7 <= 0 OR (
                SELECT count(*)
                FROM fact_work_items AS semantic_next
                WHERE semantic_next.stage = 'reducer'
                  AND semantic_next.domain = 'semantic_entity_materialization'
                  AND semantic_next.status IN ('pending', 'retrying', 'claimed', 'running')
                  -- #6828: stamp-clock visibility, like the base candidate filter below.
                  AND (semantic_next.visible_at IS NULL OR semantic_next.visible_at <= $1 OR (semantic_next.status = 'pending' AND semantic_next.visible_at <= clock_timestamp()))
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
                FROM fact_work_items AS semantic_inflight2
                WHERE semantic_inflight2.stage = 'reducer'
                  AND semantic_inflight2.domain = 'semantic_entity_materialization'
                  AND semantic_inflight2.status IN ('claimed', 'running')
                  AND semantic_inflight2.claim_until > $1
            ))
        ) AS semantic_ok
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND NOT EXISTS (
          SELECT 1
          FROM superseded_stale_reducer_generations AS superseded
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
)
SELECT work_item_id
FROM candidate
WHERE ($3::text IS NULL OR TRUE) AND ($4::timestamptz IS NULL OR TRUE)
ORDER BY reducer_domain_priority ASC, reducer_source_inflight_count ASC, reducer_source_fair_rank ASC, reducer_domain_fair_rank ASC, reducer_domain_inflight_count ASC, updated_at ASC, work_item_id ASC
LIMIT $8
