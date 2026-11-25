-- Shape: legacy selector read: a finding id that misses with a package (the shape that stays on legacy). Variant: plain (the session's plan cache mode).
-- Rendered from the C1 head by explain_shapes_render_test.go; read-only. Override a value with psql -v name=value.
\set ON_ERROR_STOP on
\if :{?cve}
\else
\set cve 'CVE-2026-33349'
\endif
\if :{?advisory}
\else
\set advisory 'GHSA-xxxx-xxxx-xxxx'
\endif
\if :{?package}
\else
\set package 'pkg:npm/axios'
\endif
\if :{?repository}
\else
\set repository 'repository:example'
\endif
\if :{?digest}
\else
\set digest 'sha256:0000000000000000000000000000000000000000000000000000000000000000'
\endif
\if :{?fact_id}
\else
\set fact_id 'fact:example'
\endif
\if :{?finding_id}
\else
\set finding_id 'finding:example'
\endif
\if :{?canonical_key}
\else
\set canonical_key 'example-canonical-key'
\endif
BEGIN TRANSACTION READ ONLY;
SET statement_timeout = '25s';
PREPARE q AS
WITH
runtime_workload_identity_matches AS MATERIALIZED (
  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE $8 <> ''
    AND fact_kind = 'reducer_workload_identity'
    AND is_tombstone = FALSE
    AND (
          (
            jsonb_typeof(payload->'workload_id') = 'string'
            AND payload->>'workload_id' = $8
          )
          OR (
               $8 LIKE 'workload:%'
               AND jsonb_typeof(payload->'entity_keys') IN ('array', 'string')
               AND payload->'entity_keys' ? $8
             )
        )

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE $8 <> ''
    AND fact_kind = 'reducer_workload_identity'
    AND is_tombstone = FALSE
    AND NOT COALESCE(
          (
            jsonb_typeof(payload->'workload_id') = 'string'
            AND payload->>'workload_id' = $8
          )
          OR (
               $8 LIKE 'workload:%'
               AND jsonb_typeof(payload->'entity_keys') IN ('array', 'string')
               AND payload->'entity_keys' ? $8
             ),
          FALSE
        )
    AND jsonb_typeof(payload->'workload_id') = 'string'
    AND payload->>'workload_id' IS DISTINCT FROM BTRIM(payload->>'workload_id')
    AND BTRIM(payload->>'workload_id') = $8

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE $8 <> ''
    AND fact_kind = 'reducer_workload_identity'
    AND is_tombstone = FALSE
    AND NOT COALESCE(
          (
            jsonb_typeof(payload->'workload_id') = 'string'
            AND payload->>'workload_id' = $8
          )
          OR (
               $8 LIKE 'workload:%'
               AND jsonb_typeof(payload->'entity_keys') IN ('array', 'string')
               AND payload->'entity_keys' ? $8
             ),
          FALSE
        )
    AND NOT COALESCE(
          jsonb_typeof(payload->'workload_id') = 'string'
          AND BTRIM(payload->>'workload_id') = $8,
          FALSE
        )
    AND jsonb_path_exists(payload->'entity_keys',
          'lax $[*] ? (@.type() == "string" && @ like_regex "(^ | $)")')
    AND $8 LIKE 'workload:%'
    AND EXISTS (
          SELECT 1
          FROM jsonb_array_elements_text(
            CASE
              WHEN jsonb_typeof(payload->'entity_keys') = 'array'
                THEN payload->'entity_keys'
              WHEN jsonb_typeof(payload->'entity_keys') = 'string'
                THEN jsonb_build_array(payload->'entity_keys')
              ELSE '[]'::jsonb
            END
          ) AS entity_key(value)
          WHERE BTRIM(entity_key.value) = $8
            AND BTRIM(entity_key.value) LIKE 'workload:%'
        )
),
runtime_workload_catalog_matches AS MATERIALIZED (
  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE $8 <> ''
    AND fact_kind = 'reducer_service_catalog_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'workload_id') = 'string'
    AND payload->>'workload_id' = $8

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE $8 <> ''
    AND fact_kind = 'reducer_service_catalog_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'workload_id') = 'string'
    AND payload->>'workload_id' IS DISTINCT FROM $8
    AND payload->>'workload_id' IS DISTINCT FROM BTRIM(payload->>'workload_id')
    AND BTRIM(payload->>'workload_id') = $8
),
runtime_service_matches AS MATERIALIZED (
  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE $9 <> ''
    AND fact_kind = 'reducer_service_catalog_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'service_id') = 'string'
    AND payload->>'service_id' = $9

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE $9 <> ''
    AND fact_kind = 'reducer_service_catalog_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'service_id') = 'string'
    AND payload->>'service_id' IS DISTINCT FROM $9
    AND payload->>'service_id' IS DISTINCT FROM BTRIM(payload->>'service_id')
    AND BTRIM(payload->>'service_id') = $9
),
runtime_environment_matches AS MATERIALIZED (
  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE '' <> ''
    AND fact_kind = 'reducer_ci_cd_run_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'environment') = 'string'
    AND payload->>'environment' = ''

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE '' <> ''
    AND fact_kind = 'reducer_ci_cd_run_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'environment') = 'string'
    AND payload->>'environment' IS DISTINCT FROM ''
    AND payload->>'environment' IS DISTINCT FROM BTRIM(payload->>'environment')
    AND BTRIM(payload->>'environment') = ''
),
runtime_filter_candidates AS NOT MATERIALIZED (
  SELECT 'workload'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM runtime_workload_identity_matches AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE runtime_generation.status = 'active'

  UNION ALL

  SELECT 'workload'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM runtime_workload_catalog_matches AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE runtime_generation.status = 'active'

  UNION ALL

  SELECT 'service'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM runtime_service_matches AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE runtime_generation.status = 'active'

  UNION ALL

  SELECT 'environment'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM runtime_environment_matches AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE runtime_generation.status = 'active'
),
runtime_filter_repository_candidates AS (
  SELECT runtime_fact.filter_kind,
         runtime_fact.scope_id,
         runtime_repository.repository_id
  FROM runtime_filter_candidates AS runtime_fact

LEFT JOIN LATERAL (
  SELECT COALESCE(
           NULLIF(BTRIM(
             CASE
               WHEN jsonb_typeof(runtime_fact.payload->'repository_id') = 'string'
                 THEN runtime_fact.payload->>'repository_id'
             END
           ), ''),
           NULLIF(BTRIM(
             CASE
               WHEN jsonb_typeof(runtime_fact.payload->'repo_id') = 'string'
                 THEN runtime_fact.payload->>'repo_id'
             END
           ), ''),
           CASE
             WHEN selected_scope.value LIKE 'repository:%'
               THEN selected_scope.value
             WHEN selected_scope.value LIKE 'git-repository-scope:%'
               THEN NULLIF(BTRIM(SUBSTRING(selected_scope.value FROM 22)), '')
           END,
           (
             SELECT decoded_related.repository_id
             FROM (
               SELECT related.ordinal,
                      CASE
                        WHEN BTRIM(related.value) LIKE 'repository:%'
                          THEN BTRIM(related.value)
                        WHEN BTRIM(related.value) LIKE 'git-repository-scope:%'
                          THEN NULLIF(BTRIM(SUBSTRING(related.value FROM 22)), '')
                      END AS repository_id
               FROM jsonb_array_elements_text(
                 CASE
                   WHEN jsonb_typeof(runtime_fact.payload->'related_scope_ids') = 'array'
                     THEN runtime_fact.payload->'related_scope_ids'
                   WHEN jsonb_typeof(runtime_fact.payload->'related_scope_ids') = 'string'
                     THEN jsonb_build_array(runtime_fact.payload->'related_scope_ids')
                   ELSE '[]'::jsonb
                 END
               ) WITH ORDINALITY AS related(value, ordinal)
             ) AS decoded_related
             WHERE COALESCE(decoded_related.repository_id, '') <> ''
             ORDER BY decoded_related.ordinal
             LIMIT 1
           ),
           selected_scope.value
         ) AS repository_id
  FROM (
    SELECT COALESCE(
             NULLIF(BTRIM(
               CASE
                 WHEN jsonb_typeof(runtime_fact.payload->'scope_id') = 'string'
                   THEN runtime_fact.payload->>'scope_id'
               END
             ), ''),
             NULLIF(BTRIM(runtime_fact.scope_id), '')
           ) AS value
  ) AS selected_scope
) AS runtime_repository ON TRUE
),
runtime_filter_repositories AS MATERIALIZED (
  SELECT DISTINCT filter_kind, repository_id
  FROM runtime_filter_repository_candidates
  WHERE COALESCE(repository_id, '') <> ''
    AND (
          (
            COALESCE(cardinality($11::text[]), 0) = 0
            AND COALESCE(cardinality($12::text[]), 0) = 0
          )
          OR runtime_filter_repository_candidates.repository_id = ANY($11::text[])
          OR runtime_filter_repository_candidates.scope_id = ANY($12::text[])
        )
),
authorized_source_candidates AS NOT MATERIALIZED (
  SELECT fact.fact_id,
         fact.scope_id,
         COALESCE(
         NULLIF(fact.payload->>'finding_id', ''),
         CONCAT_WS('|',
         COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
         COALESCE(fact.payload->>'advisory_id', ''),
         COALESCE(fact.payload->>'package_id', ''),
         COALESCE(fact.payload->>'purl', ''),
         COALESCE(fact.payload->>'product_criteria', ''),
         COALESCE(fact.payload->>'match_criteria_id', ''),
         COALESCE(fact.payload->>'observed_version', ''),
         COALESCE(fact.payload->>'requested_range', ''),
         COALESCE(fact.payload->>'impact_status', ''),
         COALESCE(fact.payload->>'repository_id', ''),
         COALESCE(fact.payload->>'subject_digest', '')
       )
       ) AS finding_id,
         fact.source_confidence,
         fact.payload,
         COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') AS suppression_state,
         COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) AS priority_score,
         CASE
         WHEN NULLIF(fact.payload->>'finding_id', '') IS NULL THEN 0
         ELSE 1
       END AS has_payload_finding_id,
         CASE
    WHEN finding_class.source_class = 'derived'
    THEN CONCAT_WS('|',
         COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
         COALESCE(fact.payload->>'advisory_id', ''),
         COALESCE(fact.payload->>'package_id', ''),
         COALESCE(fact.payload->>'purl', ''),
         COALESCE(fact.payload->>'product_criteria', ''),
         COALESCE(fact.payload->>'match_criteria_id', ''),
         COALESCE(fact.payload->>'observed_version', ''),
         COALESCE(fact.payload->>'requested_range', ''),
         COALESCE(fact.payload->>'impact_status', ''),
         COALESCE(fact.payload->>'repository_id', ''),
         COALESCE(fact.payload->>'subject_digest', '')
       ) || '|' || finding_association.scan_scope_id
    ELSE CONCAT_WS('|',
         COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
         COALESCE(fact.payload->>'advisory_id', ''),
         COALESCE(fact.payload->>'package_id', ''),
         COALESCE(fact.payload->>'purl', ''),
         COALESCE(fact.payload->>'product_criteria', ''),
         COALESCE(fact.payload->>'match_criteria_id', ''),
         COALESCE(fact.payload->>'observed_version', ''),
         COALESCE(fact.payload->>'requested_range', ''),
         COALESCE(fact.payload->>'impact_status', ''),
         COALESCE(fact.payload->>'repository_id', ''),
         COALESCE(fact.payload->>'subject_digest', '')
       )
  END AS canonical_key
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id

  LEFT JOIN scanner_image_result_fact_classifications AS finding_class
    ON finding_class.finding_fact_id = fact.fact_id
  LEFT JOIN scanner_image_result_fact_associations AS finding_association
    ON finding_association.finding_fact_id = fact.fact_id
  WHERE fact.fact_kind = $1
    AND fact.is_tombstone = FALSE
    AND generation.status = 'active'
    AND fact.scope_id <> 'operator:vulnerability_suppressions'

    AND (
      (((NOT FALSE)
        OR (finding_class.source_class = 'static' AND finding_class.lifecycle = 'active'
            AND EXISTS (
  SELECT 1
  FROM scanner_static_impact_finding_authority_revisions AS static_revision
  JOIN eshu_security.role_policy_state AS static_role
    ON static_role.singleton AND static_role.active
   AND static_role.policy_revision = static_revision.pinned_role_policy_revision
   AND static_role.database_identity = static_revision.pinned_database_identity
   AND static_role.database_oid = (SELECT oid FROM pg_database WHERE datname = current_database())
  WHERE static_revision.finding_fact_id = fact.fact_id
    AND static_revision.impact_scope_id = fact.scope_id
    AND static_revision.impact_generation_id = fact.generation_id
    AND static_revision.impact_fencing_token = fact.fencing_token
    AND static_revision.finding_payload_sha256 = encode(sha256(convert_to(fact.payload::text,'UTF8')),'hex')
    AND static_revision.advisory_dependency_count = (
      SELECT count(*) FROM scanner_static_impact_public_advisory_evidence AS static_child
      WHERE static_child.finding_fact_id = static_revision.finding_fact_id
        AND static_child.impact_fencing_token = static_revision.impact_fencing_token)
    AND NOT EXISTS (
      SELECT 1
      FROM scanner_static_impact_public_advisory_evidence AS static_child
      WHERE static_child.finding_fact_id = static_revision.finding_fact_id
        AND static_child.impact_fencing_token = static_revision.impact_fencing_token
        AND NOT COALESCE((
          EXISTS (
            SELECT 1
            FROM scanner_advisory_public_registrations AS registration
            JOIN scanner_advisory_public_generations AS approval
              ON approval.registration_id = registration.registration_id
             AND approval.active
             AND approval.approved_configuration_sha256 = registration.configuration_sha256
             AND approval.approved_authority_epoch = registration.authority_epoch
             AND approval.protected_activation_epoch = static_child.activation_epoch
             AND approval.pinned_advisory_scope_epoch = static_child.activation_epoch
            JOIN scanner_advisory_public_activations AS activation
              ON activation.collector_instance_id = registration.collector_instance_id
             AND activation.advisory_scope_id = registration.advisory_scope_id
             AND activation.active_generation_id = approval.advisory_generation_id
             AND activation.activation_epoch = approval.protected_activation_epoch
             AND activation.active
            JOIN scanner_advisory_generation_receipts AS receipt
              ON receipt.receipt_id = static_child.generation_receipt_id
             AND receipt.receipt_id = approval.generation_receipt_id
             AND receipt.registration_id = registration.registration_id
             AND receipt.collector_instance_id = registration.collector_instance_id
             AND receipt.advisory_scope_id = registration.advisory_scope_id
             AND receipt.advisory_generation_id = approval.advisory_generation_id
             AND receipt.pinned_registration_epoch = registration.authority_epoch
             AND receipt.pinned_collector_instance_epoch = registration.pinned_collector_instance_epoch
             AND receipt.pinned_configuration_sha256 = registration.configuration_sha256
             AND receipt.producer_principal_oid = approval.producer_principal_oid
             AND receipt.pinned_producer_grant_epoch = approval.producer_grant_epoch
             AND receipt.fact_set_sha256 = approval.fact_set_sha256
             AND receipt.status = 'published'
            JOIN scanner_advisory_generation_facts AS member
              ON member.receipt_id = receipt.receipt_id
             AND member.fact_id = static_child.evidence_fact_id
            JOIN fact_records AS evidence
              ON evidence.fact_id = member.fact_id
             AND evidence.scope_id = receipt.advisory_scope_id
             AND evidence.generation_id = receipt.advisory_generation_id
             AND evidence.fact_kind = member.fact_kind
             AND evidence.fencing_token = receipt.claim_fencing_token
             AND NOT evidence.is_tombstone
             AND evidence.source_system = 'vulnerability_intelligence'
             AND evidence.collector_kind = 'vulnerability_intelligence'
             AND encode(sha256(convert_to(evidence.payload::text,'UTF8')),'hex') = member.payload_sha256
             AND member.payload_sha256 = static_child.evidence_payload_sha256
            JOIN scanner_advisory_producer_grants AS producer_grant
              ON producer_grant.principal_oid = receipt.producer_principal_oid
             AND producer_grant.collector_instance_id = registration.collector_instance_id
             AND producer_grant.advisory_scope_id = registration.advisory_scope_id
             AND producer_grant.grant_epoch = receipt.pinned_producer_grant_epoch
             AND producer_grant.active
            JOIN collector_instances AS advisory_collector
              ON advisory_collector.instance_id = registration.collector_instance_id
             AND advisory_collector.collector_kind = 'vulnerability_intelligence'
             AND advisory_collector.enabled AND advisory_collector.claims_enabled
             AND encode(sha256(convert_to(advisory_collector.configuration::text,'UTF8')),'hex') = registration.configuration_sha256
            JOIN scanner_authority_epochs AS collector_epoch
              ON collector_epoch.authority_kind = 'collector_instance'
             AND collector_epoch.authority_key = jsonb_build_array(registration.collector_instance_id)::text
             AND collector_epoch.authority_epoch = registration.pinned_collector_instance_epoch
             AND collector_epoch.active
            WHERE registration.registration_id = static_child.registration_id
              AND registration.authority_epoch = static_child.registration_epoch
              AND registration.active
              AND registration.content_profile = 'supply_chain_impact_v1'
              AND registration.allowed_fact_kinds ? evidence.fact_kind
              AND approval.pinned_collector_instance_epoch = registration.pinned_collector_instance_epoch
              AND approval.approved_max_age_seconds = registration.max_age_seconds
              AND receipt.pinned_max_age_seconds = registration.max_age_seconds
              AND approval.expires_at > $13::timestamptz
              AND approval.approved_observed_at + make_interval(secs => approval.approved_max_age_seconds) = approval.expires_at
              AND approval.pinned_role_policy_revision = static_revision.pinned_role_policy_revision
              AND approval.pinned_database_identity = static_revision.pinned_database_identity
              AND receipt.pinned_role_policy_revision = static_revision.pinned_role_policy_revision
              AND receipt.pinned_database_identity = static_revision.pinned_database_identity
              AND fact.payload->'evidence_fact_ids' ? static_child.evidence_fact_id
          )
        ), FALSE))
    AND NOT EXISTS (
      SELECT 1
      FROM jsonb_array_elements_text(
        CASE WHEN jsonb_typeof(fact.payload->'evidence_fact_ids') = 'array'
             THEN fact.payload->'evidence_fact_ids' ELSE '[]'::jsonb END
      ) AS payload_evidence(fact_id)
      JOIN fact_records AS advisory_evidence ON advisory_evidence.fact_id = payload_evidence.fact_id
      WHERE advisory_evidence.fact_kind IN (
        'vulnerability.cve','vulnerability.affected_package','vulnerability.affected_product',
        'vulnerability.epss_score','vulnerability.known_exploited')
        AND NOT EXISTS (
          SELECT 1 FROM scanner_static_impact_public_advisory_evidence AS static_child
          WHERE static_child.finding_fact_id = static_revision.finding_fact_id
            AND static_child.impact_fencing_token = static_revision.impact_fencing_token
            AND static_child.evidence_fact_id = advisory_evidence.fact_id))
)))
       AND finding_association.finding_fact_id IS NULL
       AND fact.scope_id NOT LIKE 'image://derived/%'
       AND (finding_class.finding_fact_id IS NULL
            OR (finding_class.source_class = 'static' AND finding_class.lifecycle = 'active')))
      OR (finding_class.source_class = 'derived'
          AND finding_class.lifecycle = 'active'
          AND finding_association.finding_fact_id IS NOT NULL
          AND
    EXISTS (
      SELECT 1
      FROM scanner_image_result_publications AS publication
      JOIN scanner_image_targets AS target
        ON target.scan_scope_id = publication.scan_scope_id
      JOIN LATERAL (
        SELECT original_binding.*, NULL::text AS reuse_receipt_id
        FROM scanner_image_target_sources AS original_binding
        WHERE original_binding.binding_id = publication.binding_id
          AND publication.status = 'current'
        UNION ALL
        SELECT new_binding.*, receipt.reuse_receipt_id
        FROM scanner_image_result_reuse_receipts AS receipt
        JOIN scanner_image_target_sources AS new_binding
          ON new_binding.binding_id = receipt.new_binding_id
        JOIN scanner_image_target_attempts AS new_attempt
          ON new_attempt.work_item_id = receipt.new_work_item_id
         AND new_attempt.binding_id = new_binding.binding_id
         AND new_attempt.scan_scope_id = publication.scan_scope_id
         AND new_attempt.status = 'completed'
         AND new_attempt.claim_id = receipt.new_claim_id
         AND new_attempt.claim_fencing_token = receipt.new_claim_fencing_token
         AND new_attempt.claim_owner_id = receipt.new_claim_owner_id
         AND new_attempt.completed_authority_vector = receipt.authority_vector
        WHERE receipt.publication_id = publication.publication_id
          AND publication.status IN ('current','revoked')
          AND new_binding.binding_id <> publication.binding_id
          AND new_binding.scan_scope_id = publication.scan_scope_id
          AND receipt.result_set_sha256 = publication.result_set_sha256
          AND receipt.advisory_set_sha256 = publication.advisory_set_sha256
          AND receipt.analysis_version = publication.analysis_version
          AND receipt.authority_vector = jsonb_build_object(
            'source_activation_epoch',new_binding.pinned_source_activation_epoch,
            'producer_grant_epoch',new_binding.pinned_producer_grant_epoch,
            'source_instance_epoch',new_binding.pinned_source_instance_epoch,
            'tenant_epoch',new_binding.pinned_tenant_epoch,
            'workspace_epoch',new_binding.pinned_workspace_epoch,
            'scope_grant_epoch',new_binding.pinned_scope_grant_epoch,
            'registry_epoch',new_binding.pinned_registry_epoch,
            'role_policy_revision',new_binding.pinned_role_policy_revision,
            'database_identity',new_binding.pinned_database_identity)
      ) AS binding ON binding.scan_scope_id = target.scan_scope_id
      JOIN scanner_image_target_attempts AS attempt
        ON attempt.work_item_id = publication.work_item_id
       AND attempt.scan_scope_id = target.scan_scope_id
       AND attempt.binding_id = publication.binding_id
       AND attempt.status = 'completed'
      JOIN scanner_image_scan_results AS scan_result
        ON scan_result.result_receipt_id = publication.scan_result_receipt_id
       AND scan_result.work_item_id = publication.work_item_id
       AND scan_result.binding_id = publication.binding_id
       AND scan_result.scan_scope_id = publication.scan_scope_id
       AND scan_result.scan_generation_id = publication.scan_generation_id
       AND scan_result.claim_id = attempt.claim_id
       AND scan_result.claim_fencing_token = attempt.claim_fencing_token
       AND scan_result.claim_owner_id = attempt.claim_owner_id
       AND scan_result.completed_authority_vector = attempt.completed_authority_vector
      JOIN scanner_running_image_observations AS observation
        ON observation.observation_id = binding.canonical_observation_id
       AND observation.producer_instance_id = binding.source_collector_instance_id
       AND observation.source_scope_id = binding.source_scope_id
       AND observation.source_generation_id = binding.source_generation_id
       AND observation.registry = binding.registry
       AND observation.repository = binding.repository
       AND observation.runtime_digest = binding.runtime_digest
       AND observation.runtime_digest_kind = binding.runtime_digest_kind
       AND observation.runtime_image_reference = binding.runtime_image_reference
      JOIN scanner_running_image_generations AS source_generation
        ON source_generation.producer_instance_id = observation.producer_instance_id
       AND source_generation.source_scope_id = observation.source_scope_id
       AND source_generation.source_generation_id = observation.source_generation_id
       AND source_generation.status = 'published'
       AND source_generation.observation_class = observation.observation_class
       AND source_generation.producer_grant_epoch = binding.pinned_producer_grant_epoch
       AND source_generation.publication_epoch = binding.pinned_source_activation_epoch
      JOIN scanner_running_image_active_scopes AS source_activation
        ON source_activation.producer_instance_id = observation.producer_instance_id
       AND source_activation.source_scope_id = observation.source_scope_id
       AND source_activation.published_generation_id = observation.source_generation_id
       AND source_activation.activation_epoch = binding.pinned_source_activation_epoch
       AND source_activation.active
      JOIN scanner_running_image_producer_grants AS producer_grant
        ON producer_grant.principal_oid = source_generation.principal_oid
       AND producer_grant.producer_instance_id = observation.producer_instance_id
       AND producer_grant.source_scope_id = observation.source_scope_id
       AND producer_grant.producer_class = observation.producer_class
       AND producer_grant.observation_class = observation.observation_class
       AND producer_grant.grant_epoch = binding.pinned_producer_grant_epoch
       AND producer_grant.active
      JOIN scanner_authority_epochs AS source_instance_epoch
        ON source_instance_epoch.authority_kind = 'collector_instance'
       AND source_instance_epoch.authority_key = (jsonb_build_array(binding.source_collector_instance_id))::text
       AND source_instance_epoch.authority_epoch = binding.pinned_source_instance_epoch
       AND source_instance_epoch.active
      JOIN collector_instances AS source_instance
        ON source_instance.instance_id = binding.source_collector_instance_id
       AND source_instance.enabled AND source_instance.claims_enabled
      JOIN tenants AS tenant
        ON tenant.tenant_id = target.tenant_id
       AND tenant.status = 'active' AND tenant.tombstoned_at IS NULL
       AND tenant.policy_revision_hash = binding.policy_revision_hash
      JOIN scanner_authority_epochs AS tenant_epoch
        ON tenant_epoch.authority_kind = 'tenant'
       AND tenant_epoch.authority_key = (jsonb_build_array(target.tenant_id))::text
       AND tenant_epoch.authority_epoch = binding.pinned_tenant_epoch
       AND tenant_epoch.active
      JOIN workspaces AS workspace
        ON workspace.tenant_id = target.tenant_id
       AND workspace.workspace_id = target.workspace_id
       AND workspace.status = 'active' AND workspace.tombstoned_at IS NULL
       AND workspace.policy_revision_hash = binding.policy_revision_hash
      JOIN scanner_authority_epochs AS workspace_epoch
        ON workspace_epoch.authority_kind = 'workspace'
       AND workspace_epoch.authority_key = (jsonb_build_array(target.tenant_id,target.workspace_id))::text
       AND workspace_epoch.authority_epoch = binding.pinned_workspace_epoch
       AND workspace_epoch.active
      JOIN tenant_scope_grants AS source_grant
        ON source_grant.tenant_id = target.tenant_id
       AND source_grant.workspace_id = target.workspace_id
       AND source_grant.scope_id = binding.source_scope_id
       AND source_grant.subject_class = target.subject_class
       AND source_grant.policy_revision_hash = binding.policy_revision_hash
       AND source_grant.tombstoned_at IS NULL
       AND source_grant.effective_at <= $13::timestamptz
       AND (source_grant.expires_at IS NULL OR source_grant.expires_at > $13::timestamptz)
      JOIN scanner_authority_epochs AS source_grant_epoch
        ON source_grant_epoch.authority_kind = 'scope_grant'
       AND source_grant_epoch.authority_key = (jsonb_build_array(
             target.tenant_id,target.workspace_id,binding.source_scope_id,target.subject_class))::text
       AND source_grant_epoch.authority_epoch = binding.pinned_scope_grant_epoch
       AND source_grant_epoch.active
      JOIN collector_instances AS scanner
        ON scanner.instance_id = binding.scanner_collector_instance_id
       AND scanner.collector_kind = 'scanner_worker'
       AND scanner.enabled AND scanner.claims_enabled
       AND scanner.configuration->>'derive_from_running_images' = 'true'
      JOIN scanner_authority_epochs AS registry_epoch
        ON registry_epoch.authority_kind = 'collector_instance'
       AND registry_epoch.authority_key = (jsonb_build_array(binding.scanner_collector_instance_id))::text
       AND registry_epoch.authority_epoch = binding.pinned_registry_epoch
       AND registry_epoch.active
      JOIN eshu_security.role_policy_state AS role_policy
        ON role_policy.singleton AND role_policy.active
       AND role_policy.policy_revision = binding.pinned_role_policy_revision
       AND role_policy.database_identity = binding.pinned_database_identity
       AND role_policy.database_oid = (SELECT oid FROM pg_database WHERE datname = current_database())
      JOIN ingestion_scopes AS scan_scope
        ON scan_scope.scope_id = target.scan_scope_id
       AND scan_scope.active_generation_id = publication.scan_generation_id
       AND scan_scope.status = 'active'
      JOIN scope_generations AS scan_generation
        ON scan_generation.scope_id = target.scan_scope_id
       AND scan_generation.generation_id = publication.scan_generation_id
       AND scan_generation.status = 'active'
      WHERE publication.publication_id = finding_association.publication_id
        AND publication.scan_scope_id = finding_association.scan_scope_id
        AND publication.scan_generation_id = finding_association.scan_generation_id
        AND publication.status IN ('current','revoked')
        AND (binding.reuse_receipt_id IS NOT NULL
             OR publication.role_policy_revision = role_policy.policy_revision)
        AND publication.database_identity = role_policy.database_identity
        AND target.scan_scope_id = finding_association.scan_scope_id
        AND (binding.reuse_receipt_id IS NOT NULL
             OR attempt.completed_authority_vector = jsonb_build_object(
            'source_activation_epoch',binding.pinned_source_activation_epoch,
            'producer_grant_epoch',binding.pinned_producer_grant_epoch,
            'source_instance_epoch',binding.pinned_source_instance_epoch,
            'tenant_epoch',binding.pinned_tenant_epoch,
            'workspace_epoch',binding.pinned_workspace_epoch,
            'scope_grant_epoch',binding.pinned_scope_grant_epoch,
            'registry_epoch',binding.pinned_registry_epoch,
            'role_policy_revision',binding.pinned_role_policy_revision,
            'database_identity',binding.pinned_database_identity))
        AND target.authorization_namespace = 'hosted'
        AND target.tenant_id = finding_association.tenant_id
        AND target.workspace_id = finding_association.workspace_id

        AND (COALESCE(cardinality($12::text[]),0) = 0
             OR binding.source_scope_id = ANY($12::text[]))
        AND (NOT $14::boolean
             OR (target.tenant_id = $15
                 AND target.workspace_id = $16
                 AND target.subject_class = $17
                 AND binding.policy_revision_hash = $18
                 AND binding.source_scope_id = ANY($19::text[])))
        AND binding.runtime_digest_kind = 'manifest'
        AND binding.runtime_digest = target.manifest_digest
        AND binding.pinned_source_activation_epoch IS NOT NULL
        AND binding.pinned_source_instance_epoch IS NOT NULL
        AND 1 = (SELECT COUNT(*)
                 FROM jsonb_array_elements(COALESCE(scanner.configuration->'registry_domains','[]'::jsonb)) AS candidate(value)
                 WHERE candidate.value->>'authorization_domain' = target.authorization_domain)
        AND EXISTS (
          SELECT 1
          FROM jsonb_array_elements(COALESCE(scanner.configuration->'registry_domains','[]'::jsonb)) AS domain(value)
          WHERE domain.value->>'authorization_domain' = target.authorization_domain
            AND domain.value->>'registry' = binding.registry
            AND encode(sha256(convert_to(domain.value::text,'UTF8')),'hex') = binding.registry_domain_config_sha256
        )

        AND EXISTS (
          SELECT 1 FROM scanner_image_result_advisory_dependencies AS dependency
          WHERE dependency.publication_id = publication.publication_id
        )
        AND NOT EXISTS (
          SELECT 1
          FROM scanner_image_result_advisory_dependencies AS dependency
          WHERE dependency.publication_id = publication.publication_id
            AND NOT COALESCE((
                (dependency.authority_kind = 'public_registration'
                 AND EXISTS (
                   SELECT 1
                   FROM scanner_advisory_public_registrations AS registration
                   JOIN scanner_advisory_public_generations AS approval
                     ON approval.registration_id = registration.registration_id
                    AND approval.advisory_generation_id = dependency.advisory_generation_id
                    AND approval.approved_configuration_sha256 = registration.configuration_sha256
                    AND approval.approved_authority_epoch = registration.authority_epoch
                    AND approval.pinned_advisory_scope_epoch = dependency.advisory_scope_epoch
                    AND approval.protected_activation_epoch = dependency.advisory_scope_epoch
                    AND approval.active
                   JOIN scanner_advisory_public_activations AS activation
                     ON activation.collector_instance_id = registration.collector_instance_id
                    AND activation.advisory_scope_id = registration.advisory_scope_id
                    AND activation.active_generation_id = approval.advisory_generation_id
                    AND activation.activation_epoch = approval.protected_activation_epoch
                    AND activation.active
                   JOIN scanner_advisory_generation_receipts AS receipt
                     ON receipt.receipt_id = approval.generation_receipt_id
                    AND receipt.registration_id = registration.registration_id
                    AND receipt.collector_instance_id = registration.collector_instance_id
                    AND receipt.advisory_scope_id = registration.advisory_scope_id
                    AND receipt.advisory_generation_id = approval.advisory_generation_id
                    AND receipt.pinned_registration_epoch = registration.authority_epoch
                    AND receipt.pinned_collector_instance_epoch = registration.pinned_collector_instance_epoch
                    AND receipt.pinned_configuration_sha256 = registration.configuration_sha256
                    AND receipt.producer_principal_oid = approval.producer_principal_oid
                    AND receipt.pinned_producer_grant_epoch = approval.producer_grant_epoch
                    AND receipt.fact_set_sha256 = approval.fact_set_sha256
                    AND receipt.status = 'published'
                   JOIN scanner_advisory_producer_grants AS producer_grant
                     ON producer_grant.principal_oid = receipt.producer_principal_oid
                    AND producer_grant.collector_instance_id = registration.collector_instance_id
                    AND producer_grant.advisory_scope_id = registration.advisory_scope_id
                    AND producer_grant.grant_epoch = receipt.pinned_producer_grant_epoch
                    AND producer_grant.active
                   JOIN eshu_security.role_policy_state AS role_policy
                     ON role_policy.singleton AND role_policy.active
                    AND role_policy.policy_revision = approval.pinned_role_policy_revision
                    AND role_policy.database_identity = approval.pinned_database_identity
                    AND role_policy.database_oid = (SELECT oid FROM pg_database WHERE datname = current_database())
                   JOIN collector_instances AS advisory_collector
                     ON advisory_collector.instance_id = registration.collector_instance_id
                    AND advisory_collector.collector_kind = 'vulnerability_intelligence'
                    AND advisory_collector.enabled AND advisory_collector.claims_enabled
                   JOIN scanner_authority_epochs AS advisory_collector_epoch
                     ON advisory_collector_epoch.authority_kind = 'collector_instance'
                    AND advisory_collector_epoch.authority_key = (jsonb_build_array(registration.collector_instance_id))::text
                    AND advisory_collector_epoch.authority_epoch = registration.pinned_collector_instance_epoch
                    AND advisory_collector_epoch.active
                   WHERE registration.registration_id = dependency.authority_id
                     AND registration.advisory_scope_id = dependency.advisory_scope_id
                     AND registration.authority_epoch = dependency.authority_epoch
                     AND registration.trusted_source_class = dependency.trusted_source_class
                     AND registration.provider_id = dependency.provider_id
                     AND registration.content_profile = dependency.content_profile
                     AND registration.active
                     AND approval.pinned_collector_instance_epoch = registration.pinned_collector_instance_epoch
                     AND approval.approved_max_age_seconds = registration.max_age_seconds
                     AND receipt.pinned_max_age_seconds = registration.max_age_seconds
                     AND receipt.pinned_role_policy_revision = approval.pinned_role_policy_revision
                     AND receipt.pinned_database_identity = approval.pinned_database_identity
                     AND approval.expires_at > $13::timestamptz
                     AND approval.approved_observed_at + make_interval(secs => approval.approved_max_age_seconds) = approval.expires_at
                     AND receipt.fact_count = (SELECT count(*) FROM scanner_advisory_generation_facts AS member
                                               WHERE member.receipt_id = receipt.receipt_id)
                     AND receipt.fact_count = (SELECT count(*) FROM fact_records AS generation_fact
                                               WHERE generation_fact.scope_id = receipt.advisory_scope_id
                                                 AND generation_fact.generation_id = receipt.advisory_generation_id)
                     AND NOT EXISTS (
                       SELECT 1 FROM scanner_advisory_generation_facts AS member
                       LEFT JOIN fact_records AS generation_fact ON generation_fact.fact_id = member.fact_id
                       WHERE member.receipt_id = receipt.receipt_id
                         AND (generation_fact.fact_id IS NULL
                           OR generation_fact.scope_id <> receipt.advisory_scope_id
                           OR generation_fact.generation_id <> receipt.advisory_generation_id
                           OR generation_fact.fact_kind <> member.fact_kind
                           OR generation_fact.fencing_token <> receipt.claim_fencing_token
                           OR generation_fact.is_tombstone
                           OR generation_fact.source_system <> 'vulnerability_intelligence'
                           OR generation_fact.collector_kind <> 'vulnerability_intelligence'
                           OR encode(sha256(convert_to(generation_fact.payload::text,'UTF8')),'hex') <> member.payload_sha256))
                     AND encode(sha256(convert_to(advisory_collector.configuration::text,'UTF8')),'hex') = registration.configuration_sha256
                 ))
                OR (dependency.authority_kind = 'private_binding'
                    AND EXISTS (
                      SELECT 1
                      FROM scanner_advisory_private_bindings AS private_binding
                      JOIN ingestion_scopes AS advisory_scope
                        ON advisory_scope.scope_id = private_binding.advisory_scope_id
                       AND advisory_scope.status = 'active'
                       AND advisory_scope.active_generation_id = private_binding.advisory_generation_id
                      JOIN scope_generations AS advisory_generation
                        ON advisory_generation.scope_id = private_binding.advisory_scope_id
                       AND advisory_generation.generation_id = private_binding.advisory_generation_id
                       AND advisory_generation.status = 'active'
                      JOIN scanner_authority_epochs AS advisory_epoch
                        ON advisory_epoch.authority_kind = 'advisory_scope'
                       AND advisory_epoch.authority_key = (jsonb_build_array(private_binding.advisory_scope_id))::text
                       AND advisory_epoch.authority_epoch = private_binding.pinned_advisory_scope_epoch
                       AND advisory_epoch.active
                      WHERE private_binding.advisory_binding_id = dependency.authority_id
                        AND FALSE -- private advisory approval receipt is not implemented
                        AND private_binding.scan_scope_id = target.scan_scope_id
                        AND private_binding.source_binding_id = binding.binding_id
                        AND private_binding.advisory_scope_id = dependency.advisory_scope_id
                        AND private_binding.advisory_generation_id = dependency.advisory_generation_id
                        AND private_binding.pinned_advisory_scope_epoch = dependency.advisory_scope_epoch
                        AND private_binding.authority_epoch = dependency.authority_epoch
                        AND private_binding.trusted_source_class = dependency.trusted_source_class
                        AND private_binding.provider_id = dependency.provider_id
                        AND private_binding.content_profile = dependency.content_profile
                        AND private_binding.active
                    ))
            ), FALSE)
        )
        AND EXISTS (
          SELECT 1 FROM scanner_image_result_evidence_associations AS evidence_link
          WHERE evidence_link.finding_fact_id = fact.fact_id
            AND evidence_link.publication_id = publication.publication_id
        )
        AND NOT EXISTS (
          SELECT 1
          FROM scanner_image_result_evidence_associations AS evidence_link
          LEFT JOIN fact_records AS evidence
            ON evidence.fact_id = evidence_link.evidence_fact_id
          WHERE evidence_link.finding_fact_id = fact.fact_id
            AND (
              evidence_link.publication_id <> publication.publication_id
              OR evidence_link.scan_scope_id <> target.scan_scope_id
              OR evidence_link.scan_generation_id <> publication.scan_generation_id
              OR evidence.fact_id IS NULL
              OR evidence.is_tombstone
              OR (evidence_link.evidence_class = 'scan_intrinsic'
                  AND (evidence.scope_id <> target.scan_scope_id
                       OR evidence.generation_id <> publication.scan_generation_id
                       OR NOT ((evidence.fact_kind IN ('scanner_worker.analysis', 'scanner_worker.warning')
                                AND evidence.source_system = 'scanner_worker')
                            OR (evidence.fact_kind IN ('vulnerability.os_package', 'vulnerability.warning')
                                AND evidence.source_system = 'scanner_worker'
                                AND evidence.collector_kind = 'os_package_vulnerability'))
                       OR NOT EXISTS (
                           SELECT 1 FROM scanner_image_scan_fact_associations AS scan_fact
                           WHERE scan_fact.fact_id = evidence.fact_id
                             AND scan_fact.result_receipt_id = scan_result.result_receipt_id
                             AND scan_fact.scan_scope_id = evidence.scope_id
                             AND scan_fact.scan_generation_id = evidence.generation_id
                             AND scan_fact.fact_kind = evidence.fact_kind
                             AND scan_fact.fencing_token = evidence.fencing_token
                             AND scan_fact.payload_sha256 = encode(sha256(convert_to(evidence.payload::text,'UTF8')),'hex'))))
              OR (evidence_link.evidence_class = 'advisory'
                  AND NOT EXISTS (
                    SELECT 1
                    FROM scanner_image_result_advisory_dependencies AS dependency
                    WHERE dependency.publication_id = publication.publication_id
                      AND dependency.authority_kind = evidence_link.authority_kind
                      AND dependency.authority_id = evidence_link.authority_id
                      AND dependency.authority_epoch = evidence_link.authority_epoch
                      AND evidence.scope_id = dependency.advisory_scope_id
                      AND evidence.generation_id = dependency.advisory_generation_id
                      AND (
                        (dependency.authority_kind = 'public_registration'
                         AND EXISTS (
                           SELECT 1 FROM scanner_advisory_public_registrations AS registration
                           JOIN scanner_advisory_public_generations AS approval
                             ON approval.registration_id = registration.registration_id
                            AND approval.advisory_generation_id = dependency.advisory_generation_id
                            AND approval.active
                           JOIN scanner_advisory_generation_receipts AS receipt
                             ON receipt.receipt_id = approval.generation_receipt_id
                            AND receipt.status = 'published'
                           JOIN scanner_advisory_generation_facts AS member
                             ON member.receipt_id = receipt.receipt_id
                            AND member.fact_id = evidence.fact_id
                            AND member.fact_kind = evidence.fact_kind
                            AND member.payload_sha256 = encode(sha256(convert_to(evidence.payload::text,'UTF8')),'hex')
                           WHERE registration.registration_id = dependency.authority_id
                             AND registration.allowed_fact_kinds ? evidence.fact_kind
                             AND evidence.fencing_token = receipt.claim_fencing_token
                             AND evidence.source_system = 'vulnerability_intelligence'
                             AND evidence.collector_kind = 'vulnerability_intelligence'
                         ))
                        OR (dependency.authority_kind = 'private_binding'
                            AND EXISTS (
                              SELECT 1 FROM scanner_advisory_private_bindings AS private_binding
                              WHERE private_binding.advisory_binding_id = dependency.authority_id
                                AND private_binding.allowed_fact_kinds ? evidence.fact_kind
                            ))
                      )
                  ))
            )
        )
        AND NOT EXISTS (
          SELECT 1
          FROM jsonb_array_elements_text(
            CASE WHEN jsonb_typeof(fact.payload->'evidence_fact_ids') = 'array'
                 THEN fact.payload->'evidence_fact_ids'
                 ELSE '[]'::jsonb END
          ) AS payload_evidence(fact_id)
          WHERE NOT EXISTS (
            SELECT 1 FROM scanner_image_result_evidence_associations AS evidence_link
            WHERE evidence_link.finding_fact_id = fact.fact_id
              AND evidence_link.publication_id = publication.publication_id
              AND evidence_link.evidence_fact_id = payload_evidence.fact_id
          )
        )
        AND NOT EXISTS (
          SELECT 1 FROM scanner_image_result_evidence_associations AS evidence_link
          WHERE evidence_link.finding_fact_id = fact.fact_id
            AND NOT COALESCE((fact.payload->'evidence_fact_ids' ? evidence_link.evidence_fact_id),FALSE)
        )
    )
          AND FALSE)
    )
    AND ($3 = '' OR fact.payload->>'advisory_id' = $3 OR fact.payload->>'cve_id' = $3)
    AND ($4 = '' OR fact.payload->>'cve_id' = $4)
    AND ($5 = '' OR fact.payload->>'package_id' = $5)
    AND ($6 = '' OR fact.payload->>'repository_id' = $6)
    AND ($7 = '' OR fact.payload->>'subject_digest' = $7)

  AND (
        $9 = ''
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'service'
               AND runtime_filter.repository_id = fact.payload->>'repository_id'
           )
      )
  AND (
        $8 = ''
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'workload'
               AND runtime_filter.repository_id = fact.payload->>'repository_id'
           )
      )
  AND (
        '' = ''
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'environment'
               AND runtime_filter.repository_id = fact.payload->>'repository_id'
           )
	)
    AND ($10 = '' OR fact.payload->>'image_ref' = $10)
    AND (
      (COALESCE(cardinality($11::text[]), 0) = 0 AND COALESCE(cardinality($12::text[]), 0) = 0)
      OR fact.payload->>'repository_id' = ANY($11::text[])
      OR fact.scope_id = ANY($12::text[])
      OR finding_class.source_class = 'derived'
    )

),
source_candidates AS MATERIALIZED (
  SELECT *
  FROM authorized_source_candidates

  WHERE $2 = ''
     OR fact_id = $2
     OR finding_id = $2
     OR canonical_key = $2
     OR canonical_key IN (
          SELECT canonical_key
          FROM authorized_source_candidates AS identity_candidate
          WHERE identity_candidate.fact_id = $2
        )
),

operator_candidates AS MATERIALIZED (
  SELECT fact.fact_id,
         fact.scope_id,
         COALESCE(
         NULLIF(fact.payload->>'finding_id', ''),
         CONCAT_WS('|',
         COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
         COALESCE(fact.payload->>'advisory_id', ''),
         COALESCE(fact.payload->>'package_id', ''),
         COALESCE(fact.payload->>'purl', ''),
         COALESCE(fact.payload->>'product_criteria', ''),
         COALESCE(fact.payload->>'match_criteria_id', ''),
         COALESCE(fact.payload->>'observed_version', ''),
         COALESCE(fact.payload->>'requested_range', ''),
         COALESCE(fact.payload->>'impact_status', ''),
         COALESCE(fact.payload->>'repository_id', ''),
         COALESCE(fact.payload->>'subject_digest', '')
       )
       ) AS finding_id,
         fact.source_confidence,
         fact.payload,
         COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') AS suppression_state,
         COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) AS priority_score,
         CASE
         WHEN NULLIF(fact.payload->>'finding_id', '') IS NULL THEN 0
         ELSE 1
       END AS has_payload_finding_id,
         CONCAT_WS('|',
         COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
         COALESCE(fact.payload->>'advisory_id', ''),
         COALESCE(fact.payload->>'package_id', ''),
         COALESCE(fact.payload->>'purl', ''),
         COALESCE(fact.payload->>'product_criteria', ''),
         COALESCE(fact.payload->>'match_criteria_id', ''),
         COALESCE(fact.payload->>'observed_version', ''),
         COALESCE(fact.payload->>'requested_range', ''),
         COALESCE(fact.payload->>'impact_status', ''),
         COALESCE(fact.payload->>'repository_id', ''),
         COALESCE(fact.payload->>'subject_digest', '')
       ) AS canonical_key
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
  WHERE fact.fact_kind = $1
    AND fact.is_tombstone = FALSE
    AND generation.status = 'active'
    AND fact.scope_id = 'operator:vulnerability_suppressions'
    AND COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') <> 'active'
    AND CONCAT_WS('|',
         COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
         COALESCE(fact.payload->>'advisory_id', ''),
         COALESCE(fact.payload->>'package_id', ''),
         COALESCE(fact.payload->>'purl', ''),
         COALESCE(fact.payload->>'product_criteria', ''),
         COALESCE(fact.payload->>'match_criteria_id', ''),
         COALESCE(fact.payload->>'observed_version', ''),
         COALESCE(fact.payload->>'requested_range', ''),
         COALESCE(fact.payload->>'impact_status', ''),
         COALESCE(fact.payload->>'repository_id', ''),
         COALESCE(fact.payload->>'subject_digest', '')
       ) IN (
      SELECT canonical_key FROM source_candidates
    )
),

source_winners AS (
  SELECT DISTINCT ON (canonical_key)
         fact_id, scope_id, finding_id, source_confidence, payload,
         suppression_state, priority_score, has_payload_finding_id,
         canonical_key
  FROM source_candidates
  ORDER BY canonical_key,
           priority_score DESC,
           has_payload_finding_id DESC,
           fact_id ASC
),
operator_overrides AS (
  SELECT DISTINCT ON (canonical_key)
         fact_id, scope_id, payload, suppression_state, canonical_key
  FROM operator_candidates
  ORDER BY canonical_key,
           priority_score DESC,
           has_payload_finding_id DESC,
           fact_id ASC
),
joined_winners AS (
  SELECT source.fact_id,
         source.scope_id,
         source.finding_id,
         source.source_confidence,
         source.payload AS source_payload,
         override.payload AS override_payload,
         CASE
           WHEN override.fact_id IS NULL THEN source.suppression_state
           ELSE CASE
    WHEN override.scope_id <> 'operator:vulnerability_suppressions'
    THEN override.suppression_state
    ELSE CASE
    WHEN override.suppression_state NOT IN ('not_affected','accepted_risk','false_positive','ignored','advisory_retracted')
      OR NULLIF(override.payload #>> '{suppression,expires_at}', '') IS NULL
    THEN override.suppression_state
    WHEN NOT pg_input_is_valid(override.payload #>> '{suppression,expires_at}', 'timestamp with time zone')
    THEN 'expired'
    WHEN (override.payload #>> '{suppression,expires_at}')::timestamptz <= $13::timestamptz
    THEN 'expired'
    ELSE override.suppression_state
  END
  END
         END AS effective_suppression_state,
         source.priority_score,
         source.has_payload_finding_id,
         source.canonical_key
  FROM source_winners AS source
  LEFT JOIN operator_overrides AS override
    ON override.canonical_key = source.canonical_key
),
canonical_winners AS (
  SELECT fact_id,
         scope_id,
         finding_id,
         source_confidence,
         CASE
    WHEN override_payload IS NULL
    THEN (source_payload || jsonb_build_object(
    'suppression_state', effective_suppression_state,
    'suppression',
      COALESCE(
        CASE
          WHEN jsonb_typeof(source_payload->'suppression') = 'object'
          THEN source_payload->'suppression'
        END,
        '{}'::jsonb
      ) || jsonb_build_object('state', effective_suppression_state)
  ))
    ELSE source_payload || jsonb_build_object(
      'suppression_state', effective_suppression_state,
      'suppression',
        COALESCE(
          CASE
            WHEN jsonb_typeof(override_payload->'suppression') = 'object'
            THEN override_payload->'suppression'
          END,
          '{}'::jsonb
        ) || jsonb_build_object('state', effective_suppression_state)
    )
  END AS payload,
         effective_suppression_state AS suppression_state,
         priority_score,
         has_payload_finding_id,
         canonical_key
  FROM joined_winners
),
canonical_facts AS (
  SELECT finding_id, source_confidence, payload
  FROM canonical_winners
)
SELECT finding_id, source_confidence, payload
FROM canonical_facts
ORDER BY finding_id ASC
LIMIT 2;
-- for the plan without running the statement, drop ANALYZE from the line below
EXPLAIN (ANALYZE, BUFFERS) EXECUTE q('reducer_supply_chain_impact_finding', 'finding:no-such-finding', '', '', :'package', '', '', '', '', '', '{}'::text[], '{}'::text[], now(), false, '', '', '', '', '{}'::text[]);
DEALLOCATE q;
ROLLBACK;
