// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// activeWorkSummaryPreHistoryGroupsOracle is activeWorkSummaryQuery exactly as
// it shipped at origin/main 9bcca588f, before #7009 grouped succeeded and
// other history rows ahead of the generation join. It was rendered from that
// commit's constants by a temporary package test, not retyped, and its
// SHA-256 is the measured shim baseline
// (activeWorkSummaryPreHistoryGroupsOracleSHA256). The live differential in
// status_active_work_semantics_live_test.go runs it next to the shipped query
// on one snapshot and requires identical section rows. Never hand-edit it: a
// changed oracle could agree with a wrong query.
//
// Lifecycle: the oracle is bound to 9bcca588f and to the #7009
// transformation only. When a shared fragment changes (stageCountsSelect,
// domainBacklogCTEs, reducerConflictBlockageCTEs, the scope join, or
// latestQueueFailureSelect), TestActiveWorkSummaryDiffersFromOracleOnlyInHistoryGroups
// fails, and the live differential may fail on sections #7009 never touched.
// Then do one of two things. Either re-derive the oracle from the new query
// by the reverse substitutions in that test, render it by a temporary
// package test, and re-pin its SHA-256 with review sign-off; or retire the
// oracle tests and keep TestActiveWorkSummaryMatchesStandaloneReads as the
// surviving equivalence oracle.
const activeWorkSummaryPreHistoryGroupsOracle = `
WITH active_fact_work_items_scope_state AS MATERIALIZED (
  SELECT scope.scope_id,
         scope.active_generation_id,
         active_generation.generation_id AS present_active_generation_id,
         active_generation.ingested_at AS active_ingested_at
  FROM ingestion_scopes AS scope
  LEFT JOIN LATERAL (
    SELECT generation.generation_id, generation.ingested_at
    FROM scope_generations AS generation
    WHERE generation.generation_id = scope.active_generation_id
      AND generation.scope_id = scope.scope_id
    -- generation_id is the primary key; LIMIT 1 keeps this a per-scope
    -- index probe instead of letting the planner flatten it into a scan.
    LIMIT 1
  ) AS active_generation ON TRUE
),
active_fact_work_items AS MATERIALIZED (
  SELECT CASE WHEN work.status IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter') THEN work.work_item_id END AS work_item_id,
         CASE WHEN work.status IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter') THEN work.scope_id END AS scope_id,
         CASE WHEN work.status IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter') THEN work.generation_id END AS generation_id,
         work.stage,
         CASE WHEN work.status IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter') THEN work.domain END AS domain,
         work.status,
         CASE WHEN work.stage = 'reducer' AND work.status IN ('pending', 'claimed', 'running', 'retrying') THEN work.conflict_domain END AS conflict_domain,
         CASE WHEN work.stage = 'reducer' AND work.status IN ('pending', 'claimed', 'running', 'retrying') THEN work.conflict_key END AS conflict_key,
         work.visible_at,
         work.claim_until,
         work.created_at,
         work.updated_at,
         CASE WHEN work.stage = 'reducer'
                   AND work.status IN ('pending', 'retrying', 'claimed', 'running')
              THEN work.payload END AS payload,
         CASE WHEN work.status IN ('retrying', 'failed', 'dead_letter')
              THEN work.failure_class END AS failure_class,
         CASE WHEN work.status IN ('retrying', 'failed', 'dead_letter')
              THEN work.failure_message END AS failure_message,
         CASE WHEN work.status IN ('retrying', 'failed', 'dead_letter')
              THEN work.failure_details END AS failure_details,
         work.provenance_edge_identity_upgrade_required
  FROM (SELECT * FROM fact_work_items OFFSET 0) AS work
  JOIN active_fact_work_items_scope_state AS scope_state
    ON scope_state.scope_id = work.scope_id
  JOIN scope_generations AS stale_generation
    ON stale_generation.scope_id = work.scope_id
   AND stale_generation.generation_id = work.generation_id
  WHERE NOT (
    work.stage = 'reducer'
    AND work.status IN ('pending', 'retrying', 'failed', 'dead_letter')
    AND scope_state.present_active_generation_id IS NOT NULL
    AND work.generation_id <> scope_state.active_generation_id
    AND (
      stale_generation.ingested_at < scope_state.active_ingested_at
      OR (
        stale_generation.ingested_at = scope_state.active_ingested_at
        AND stale_generation.generation_id < scope_state.present_active_generation_id
      )
    )
  )
),
fact_domain_backlogs AS (
  SELECT domain,
         COUNT(*) FILTER (WHERE status IN ('pending', 'claimed', 'running', 'retrying')) AS outstanding_count,
         COUNT(*) FILTER (WHERE status IN ('claimed', 'running')) AS in_flight_count,
         COUNT(*) FILTER (WHERE status = 'retrying') AS retrying_count,
         COUNT(*) FILTER (WHERE status = 'dead_letter') AS dead_letter_count,
         COUNT(*) FILTER (WHERE status = 'failed') AS failed_count,
         GREATEST(
           COALESCE(
             EXTRACT(
               EPOCH FROM (
                 $1 - (
                   MIN(created_at)
                     FILTER (WHERE status IN ('pending', 'claimed', 'running', 'retrying'))
                 )
               )
             ),
             0
           ),
           0
         ) AS oldest_outstanding_age_seconds
  FROM active_fact_work_items
  WHERE status IN ('pending', 'claimed', 'running', 'retrying', 'dead_letter', 'failed')
  GROUP BY domain
  HAVING COUNT(*) FILTER (WHERE status IN ('pending', 'claimed', 'running', 'retrying', 'dead_letter', 'failed')) > 0
),
shared_projection_active_leases AS (
  SELECT projection_domain AS domain,
         COUNT(*) AS in_flight_count
  FROM shared_projection_partition_leases
  WHERE lease_owner IS NOT NULL
    AND lease_expires_at IS NOT NULL
    AND lease_expires_at > $1
  GROUP BY projection_domain
),
-- Only pending intents or a live lease can produce a shared projection
-- backlog row, so read pending intents once instead of joining every intent
-- row (completed rows accumulate into the hundreds of thousands; #6794).
shared_projection_pending AS (
  SELECT projection_domain AS domain,
         COUNT(*) AS outstanding_count,
         MIN(created_at) AS oldest_created_at
  FROM shared_projection_intents
  WHERE completed_at IS NULL
  GROUP BY projection_domain
),
shared_projection_domain_backlogs AS (
  SELECT COALESCE(pending.domain, active.domain) AS domain,
         -- A live-lease domain with no pending intent has zero outstanding
         -- intents. The pre-#6794 LEFT JOIN counted the null-extended row of
         -- a lease-only domain as one phantom pending intent.
         COALESCE(pending.outstanding_count, 0) AS outstanding_count,
         COALESCE(active.in_flight_count, 0) AS in_flight_count,
         0::BIGINT AS retrying_count,
         0::BIGINT AS dead_letter_count,
         0::BIGINT AS failed_count,
         GREATEST(
           COALESCE(EXTRACT(EPOCH FROM ($1 - pending.oldest_created_at)), 0),
           0
         ) AS oldest_outstanding_age_seconds
  FROM shared_projection_pending AS pending
  FULL OUTER JOIN shared_projection_active_leases AS active
    ON active.domain = pending.domain
),
reducer_claim_readiness_requirements(domain, keyspace, phase, acceptance_unit_source, acceptance_unit_prefix) AS (
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
eligible AS (
    SELECT work_item_id,
           scope_id,
           generation_id,
           domain,
           conflict_domain,
           COALESCE(conflict_key, scope_id) AS conflict_key,
           COALESCE(visible_at, created_at) AS available_at,
           payload
    FROM active_fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND (visible_at IS NULL OR visible_at <= $1)
      AND (claim_until IS NULL OR claim_until <= $1)
),
inflight_leases AS MATERIALIZED (
    SELECT work_item_id,
           conflict_domain,
           COALESCE(conflict_key, scope_id) AS conflict_key
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('claimed', 'running')
      AND claim_until > $1
),
blocked AS (
    SELECT work_item_id,
           domain,
           conflict_domain,
           conflict_key,
           available_at
    FROM eligible
    WHERE COALESCE(
        (conflict_domain, conflict_key) IN (SELECT conflict_domain, conflict_key FROM inflight_leases),
        FALSE)
),
readiness_blocked AS (
    -- Surface each missing readiness requirement as its own bounded blockage row.
    -- Multi-key domains such as security-group reachability and EC2 profile
    -- edges appear once per missing keyspace/entity-key requirement.
    SELECT eligible.work_item_id,
           eligible.domain,
           'readiness' AS conflict_domain,
           readiness_req.keyspace || ':' || readiness_req.phase || ':' ||
               CASE readiness_req.acceptance_unit_source
                      WHEN 'payload_entity_key' THEN COALESCE(NULLIF(eligible.payload->>'entity_key', ''), eligible.scope_id)
                      WHEN 'scope_prefix' THEN readiness_req.acceptance_unit_prefix || eligible.scope_id
                      ELSE eligible.scope_id
                      END AS conflict_key,
           eligible.available_at
    FROM eligible
    JOIN reducer_claim_readiness_requirements AS readiness_req
      ON readiness_req.domain = eligible.domain
      AND NOT EXISTS (
          SELECT 1
          FROM graph_projection_phase_state AS readiness_phase
          WHERE readiness_phase.scope_id = eligible.scope_id
            AND readiness_phase.acceptance_unit_id = CASE readiness_req.acceptance_unit_source
                      WHEN 'payload_entity_key' THEN COALESCE(NULLIF(eligible.payload->>'entity_key', ''), eligible.scope_id)
                      WHEN 'scope_prefix' THEN readiness_req.acceptance_unit_prefix || eligible.scope_id
                      ELSE eligible.scope_id
                      END
            AND readiness_phase.source_run_id = eligible.generation_id
            AND readiness_phase.generation_id = eligible.generation_id
            AND readiness_phase.keyspace = readiness_req.keyspace
            AND readiness_phase.phase = readiness_req.phase
      )
),
all_blocked AS (
    SELECT work_item_id, domain, conflict_domain, conflict_key, available_at FROM blocked
    UNION ALL
    SELECT work_item_id, domain, conflict_domain, conflict_key, available_at FROM readiness_blocked
),
blockage_rows AS (
    SELECT domain,
           conflict_domain,
           conflict_key,
           COUNT(DISTINCT work_item_id) AS blocked_count,
           COALESCE(EXTRACT(EPOCH FROM ($1 - MIN(available_at))), 0) AS oldest_blocked_age_seconds
    FROM all_blocked
    GROUP BY domain, conflict_domain, conflict_key
),
domain_blocked AS (
    SELECT domain,
           COUNT(DISTINCT work_item_id) AS domain_blocked_count
    FROM all_blocked
    GROUP BY domain
),
active_work_stage AS (
  SELECT ROW_NUMBER() OVER (ORDER BY stage, status) AS ordinal, to_jsonb(section_row) AS section_json
  FROM (
SELECT stage, status, COUNT(*) AS count
FROM active_fact_work_items
GROUP BY stage, status
  ) AS section_row
),
active_work_backlog AS (
  SELECT ROW_NUMBER() OVER (ORDER BY outstanding_count DESC, oldest_outstanding_age_seconds DESC, domain ASC) AS ordinal, to_jsonb(section_row) AS section_json
  FROM (
SELECT domain,
       SUM(outstanding_count) AS outstanding_count,
       SUM(in_flight_count) AS in_flight_count,
       SUM(retrying_count) AS retrying_count,
       SUM(dead_letter_count) AS dead_letter_count,
       SUM(failed_count) AS failed_count,
       MAX(oldest_outstanding_age_seconds) AS oldest_outstanding_age_seconds
FROM (
  SELECT * FROM fact_domain_backlogs
  UNION ALL
  SELECT * FROM shared_projection_domain_backlogs
) AS domain_backlogs
GROUP BY domain
HAVING SUM(outstanding_count) + SUM(in_flight_count) + SUM(retrying_count) + SUM(dead_letter_count) + SUM(failed_count) > 0
  ) AS section_row
),
active_work_queue AS (
  SELECT 1::BIGINT AS ordinal, to_jsonb(section_row) AS section_json
  FROM (
SELECT (SELECT COUNT(*) FROM fact_work_items) AS total_count,
       COUNT(*) FILTER (WHERE status IN ('pending', 'claimed', 'running', 'retrying')) AS outstanding_count,
       COUNT(*) FILTER (WHERE status = 'pending') AS pending_count,
       COUNT(*) FILTER (WHERE status IN ('claimed', 'running')) AS in_flight_count,
       COUNT(*) FILTER (WHERE status = 'retrying') AS retrying_count,
       COUNT(*) FILTER (WHERE status = 'succeeded') AS succeeded_count,
       COUNT(*) FILTER (WHERE status = 'dead_letter') AS dead_letter_count,
       COUNT(*) FILTER (WHERE status = 'failed') AS failed_count,
	   EXISTS (
	     SELECT 1
	     FROM cross_scope_completion_upgrade_markers
	     WHERE marker_name = 'provenance_edge_identity_upgrade_096'
	   ) AS provenance_edge_identity_upgrade_applied,
	   COUNT(*) FILTER (
	     WHERE provenance_edge_identity_upgrade_required
	       AND stage = 'reducer'
	       AND domain IN ('package_source_correlation', 'container_image_identity')
	       AND status IN ('pending', 'claimed', 'running', 'retrying')
	   ) AS provenance_edge_identity_upgrade_required,
       GREATEST(
         COALESCE(
           EXTRACT(
             EPOCH FROM (
               $1 - (
                 MIN(created_at)
                   FILTER (WHERE status IN ('pending', 'claimed', 'running', 'retrying'))
               )
             )
           ),
           0
         ),
         0
       ) AS oldest_outstanding_age_seconds,
       COUNT(*) FILTER (
         WHERE status IN ('claimed', 'running')
           AND claim_until IS NOT NULL
           AND claim_until < $1
       ) AS overdue_claim_count
FROM active_fact_work_items
  ) AS section_row
),
active_work_blockage AS (
  SELECT ordinal, section_json
  FROM (
    SELECT ROW_NUMBER() OVER (ORDER BY blocked_count DESC, oldest_blocked_age_seconds DESC, domain ASC, conflict_key ASC) AS ordinal, to_jsonb(section_row) AS section_json
    FROM (
SELECT 'reducer' AS stage,
       domain,
       conflict_domain,
       conflict_key,
       domain_blocked_count AS blocked_count,
       oldest_blocked_age_seconds
FROM blockage_rows
JOIN domain_blocked USING (domain)
    ) AS section_row
  ) AS ranked
  WHERE ordinal <= 10
),
active_work_failure AS (
  SELECT ordinal, section_json
  FROM (
    SELECT ROW_NUMBER() OVER (ORDER BY updated_at DESC, work_item_id ASC) AS ordinal, to_jsonb(section_row) AS section_json
    FROM (
SELECT stage,
       domain,
       status,
       work_item_id,
       scope_id,
       generation_id,
       COALESCE(failure_class, '') AS failure_class,
       COALESCE(failure_message, '') AS failure_message,
       COALESCE(failure_details, '') AS failure_details,
       updated_at
FROM active_fact_work_items
WHERE status IN ('retrying', 'failed', 'dead_letter')
  AND (
    NULLIF(BTRIM(COALESCE(failure_class, '')), '') IS NOT NULL
    OR NULLIF(BTRIM(COALESCE(failure_message, '')), '') IS NOT NULL
    OR NULLIF(BTRIM(COALESCE(failure_details, '')), '') IS NOT NULL
  )
    ) AS section_row
  ) AS ranked
  WHERE ordinal = 1
)
SELECT 'stage' AS section, ordinal, section_json::text FROM active_work_stage
UNION ALL
SELECT 'backlog', ordinal, section_json::text FROM active_work_backlog
UNION ALL
SELECT 'queue', ordinal, section_json::text FROM active_work_queue
UNION ALL
SELECT 'blockage', ordinal, section_json::text FROM active_work_blockage
UNION ALL
SELECT 'failure', ordinal, section_json::text FROM active_work_failure
ORDER BY section, ordinal
`

// activeWorkSummaryPreHistoryGroupsOracleSHA256 pins the oracle text.
const activeWorkSummaryPreHistoryGroupsOracleSHA256 = "bef1078e2f5b971f6f35f576403b5c7197b59b6e37ccf13974d3240e4d432e2e"
