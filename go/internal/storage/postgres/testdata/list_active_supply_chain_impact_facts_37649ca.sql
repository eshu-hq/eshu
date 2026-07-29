SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind IN (
    'vulnerability.cve',
    'vulnerability.affected_package',
    'vulnerability.affected_product',
    'vulnerability.suppression',
    'security_alert.repository_alert',
    'package_registry.package_version',
    'package_registry.vulnerability_hint',
    'reducer_package_consumption_correlation',
    'sbom.component',
    'reducer_sbom_attestation_attachment',
    'reducer_container_image_identity',
    'reducer_ci_cd_run_correlation',
    'reducer_platform_materialization',
    'reducer_service_catalog_correlation',
    'reducer_workload_identity',
    'oci_registry.image_manifest',
    'oci_registry.image_index',
    'oci_registry.image_tag_observation',
    'oci_registry.image_referrer',
    'file',
    'vulnerability.epss_score',
    'vulnerability.known_exploited'
)
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
      fact.payload->>'package_id' = ANY($1::text[])
      OR fact.payload->'scope'->>'package_id' = ANY($1::text[])
      OR fact.payload->>'purl' = ANY($2::text[])
      OR fact.payload->'scope'->>'purl' = ANY($2::text[])
      OR fact.payload->>'cve_id' = ANY($3::text[])
      OR fact.payload->'scope'->>'cve_id' = ANY($3::text[])
      OR (
          cardinality($4::text[]) > 0
          AND (
              fact.payload->>'advisory_id' = ANY($4::text[])
              OR fact.payload->'scope'->>'advisory_id' = ANY($4::text[])
          )
      )
      OR fact.payload->>'subject_digest' = ANY($5::text[])
      OR fact.payload->'scope'->>'subject_digest' = ANY($5::text[])
      OR fact.payload->>'digest' = ANY($5::text[])
      OR fact.payload->>'artifact_digest' = ANY($5::text[])
      OR fact.payload->>'referrer_digest' = ANY($5::text[])
      OR fact.payload->>'resolved_digest' = ANY($5::text[])
      OR fact.payload->>'cpe' = ANY($6::text[])
      OR fact.payload->>'criteria' = ANY($6::text[])
      OR fact.payload->>'document_id' = ANY($7::text[])
      OR (
          fact.fact_kind IN (
              'vulnerability.suppression',
              'reducer_package_consumption_correlation',
              'reducer_container_image_identity',
              'reducer_ci_cd_run_correlation',
              'reducer_platform_materialization',
              'reducer_service_catalog_correlation',
              'reducer_workload_identity'
          )
          AND (
              fact.payload->>'repository_id' = ANY($8::text[])
              OR fact.payload->>'repo_id' = ANY($8::text[])
              OR fact.payload->'scope'->>'repository_id' = ANY($8::text[])
              OR fact.scope_id = ANY($8::text[])
              OR fact.payload->>'scope_id' = ANY($8::text[])
              OR scope.source_key = ANY($8::text[])
              OR scope.payload->>'repo_id' = ANY($8::text[])
              OR scope.payload->>'id' = ANY($8::text[])
          )
      )
      OR (
          fact.fact_kind = 'file'
          AND (
              fact.payload->>'repository_id' = ANY($10::text[])
              OR fact.payload->>'repo_id' = ANY($10::text[])
              OR fact.payload->'scope'->>'repository_id' = ANY($10::text[])
              OR fact.scope_id = ANY($10::text[])
              OR fact.payload->>'scope_id' = ANY($10::text[])
              OR scope.source_key = ANY($10::text[])
              OR scope.payload->>'repo_id' = ANY($10::text[])
              OR scope.payload->>'id' = ANY($10::text[])
          )
          AND LOWER(COALESCE(
              fact.payload->'parsed_file_data'->>'language',
              fact.payload->>'language',
              ''
          )) IN ('javascript', 'jsx', 'typescript', 'tsx')
      )
      OR fact.payload->>'image_ref' = ANY($9::text[])
  )
  AND ($11 = '' OR fact.fact_id > $11)
ORDER BY fact.fact_id ASC
LIMIT $12
