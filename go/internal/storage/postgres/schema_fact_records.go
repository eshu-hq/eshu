// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const factRecordSchemaSQL = factRecordBaseSchemaSQL + documentationFactRecordReadIndexesSQL + factRecordReadIndexesSQL + vulnerabilityFactRecordReadIndexesSQL + incidentFactRecordReadIndexesSQL + incidentRuntimeFactRecordReadIndexesSQL + incidentWorkItemFactRecordReadIndexesSQL

const factRecordBaseSchemaSQL = `
CREATE TABLE IF NOT EXISTS fact_records (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    fact_kind TEXT NOT NULL,
    stable_fact_key TEXT NOT NULL,
    schema_version TEXT NOT NULL DEFAULT '0.0.0',
    collector_kind TEXT NOT NULL DEFAULT 'unknown',
    fencing_token BIGINT NOT NULL DEFAULT 0,
    source_confidence TEXT NOT NULL DEFAULT 'unknown',
    source_system TEXT NOT NULL,
    source_fact_key TEXT NOT NULL,
    source_uri TEXT NULL,
    source_record_id TEXT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

ALTER TABLE fact_records
    ADD COLUMN IF NOT EXISTS schema_version TEXT NOT NULL DEFAULT '0.0.0';

ALTER TABLE fact_records
    ADD COLUMN IF NOT EXISTS collector_kind TEXT NOT NULL DEFAULT 'unknown';

ALTER TABLE fact_records
    ADD COLUMN IF NOT EXISTS fencing_token BIGINT NOT NULL DEFAULT 0;

ALTER TABLE fact_records
    ADD COLUMN IF NOT EXISTS source_confidence TEXT NOT NULL DEFAULT 'unknown';
CREATE INDEX IF NOT EXISTS fact_records_scope_generation_idx
    ON fact_records (scope_id, generation_id, fact_kind, observed_at DESC);
CREATE INDEX IF NOT EXISTS fact_records_collector_status_active_idx
    ON fact_records (
        scope_id,
        generation_id,
        source_system,
        fact_kind,
        observed_at DESC,
        ingested_at DESC
    )
    WHERE is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_active_repository_idx
    ON fact_records (observed_at ASC, fact_id ASC, generation_id)
    WHERE fact_kind = 'repository'
      AND source_system = 'git';

CREATE INDEX IF NOT EXISTS fact_records_framework_routes_repo_path_idx
    ON fact_records ((payload->>'repo_id'), (payload->>'relative_path'))
    WHERE fact_kind = 'file'
      AND payload->'parsed_file_data'->'framework_semantics' IS NOT NULL
      AND jsonb_array_length(
          COALESCE(payload->'parsed_file_data'->'framework_semantics'->'frameworks', '[]'::jsonb)
      ) > 0;

CREATE INDEX IF NOT EXISTS fact_records_jvm_reachability_repo_file_idx
    ON fact_records ((payload->>'repo_id'), observed_at ASC, fact_id ASC, generation_id)
    WHERE fact_kind = 'file'
      AND source_system = 'git'
      AND is_tombstone = FALSE
      AND (
          LOWER(COALESCE(payload->'parsed_file_data'->>'lang', '')) IN ('java', 'kotlin', 'scala')
          OR payload->>'relative_path' LIKE '%.java'
          OR payload->>'relative_path' LIKE '%.kt'
          OR payload->>'relative_path' LIKE '%.kts'
          OR payload->>'relative_path' LIKE '%.scala'
          OR payload->>'relative_path' LIKE '%.sc'
      );
`

const factRecordReadIndexesSQL = `
CREATE INDEX IF NOT EXISTS fact_records_active_package_dependency_entity_idx
    ON fact_records (
        (payload->'entity_metadata'->>'package_manager'),
        (payload->>'entity_name'),
        observed_at ASC,
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'content_entity'
      AND source_system = 'git'
      AND payload->>'entity_type' = 'Variable'
      AND payload->'entity_metadata'->>'config_kind' = 'dependency';

CREATE INDEX IF NOT EXISTS fact_records_package_correlations_v2_lookup_idx
    ON fact_records (
        (payload->>'package_id'),
        (payload->>'repository_id'),
        (payload->>'relationship_kind'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind IN (
        'reducer_package_ownership_correlation',
        'reducer_package_consumption_correlation',
        'reducer_package_publication_correlation'
    )
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_package_correlations_v2_repository_lookup_idx
    ON fact_records (
        (payload->>'repository_id'),
        (payload->>'package_id'),
        (payload->>'relationship_kind'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind IN (
        'reducer_package_ownership_correlation',
        'reducer_package_consumption_correlation',
        'reducer_package_publication_correlation'
    )
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_ci_cd_run_correlations_lookup_idx
    ON fact_records (
        (payload->>'repository_id'),
        (payload->>'commit_sha'),
        (payload->>'artifact_digest'),
        (payload->>'environment'),
        (payload->>'outcome'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_ci_cd_run_correlation'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_ci_cd_run_correlations_run_lookup_idx
    ON fact_records (
        (payload->>'run_id'),
        (payload->>'provider'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_ci_cd_run_correlation'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_ci_cd_run_correlations_commit_lookup_idx
    ON fact_records (
        (payload->>'commit_sha'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_ci_cd_run_correlation'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_ci_cd_run_correlations_artifact_lookup_idx
    ON fact_records (
        (payload->>'artifact_digest'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_ci_cd_run_correlation'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_ci_cd_run_correlations_image_ref_idx
    ON fact_records (
        (payload->>'image_ref'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_ci_cd_run_correlation'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_ci_cd_run_correlations_environment_lookup_idx
    ON fact_records (
        (payload->>'environment'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_ci_cd_run_correlation'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_container_image_identity_digest_idx
    ON fact_records (
        (payload->>'digest'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_container_image_identity'
      AND is_tombstone = FALSE;
CREATE INDEX IF NOT EXISTS fact_records_container_image_identity_ref_idx
    ON fact_records (
        (payload->>'image_ref'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_container_image_identity'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_container_image_identity_repository_idx
    ON fact_records (
        (payload->>'repository_id'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_container_image_identity'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_container_image_identity_outcome_idx
    ON fact_records (
        (payload->>'outcome'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_container_image_identity'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_active_container_image_refs_idx
    ON fact_records (
        observed_at ASC,
        fact_id ASC,
        generation_id,
        source_system
    )
    WHERE is_tombstone = FALSE
      AND (
        (fact_kind IN ('oci_registry.image_tag_observation', 'oci_registry.image_manifest', 'oci_registry.image_index')
          AND source_system = 'oci_registry')
        OR (fact_kind = 'aws_image_reference'
          AND source_system = 'aws')
        OR (fact_kind = 'aws_relationship'
          AND source_system = 'aws'
          AND payload->>'target_type' = 'container_image')
        OR (fact_kind = 'content_entity'
          AND source_system = 'git'
          AND (
            payload->'entity_metadata' ? 'container_images'
            OR payload->'metadata' ? 'container_images'
          ))
      );

CREATE INDEX IF NOT EXISTS fact_records_supply_chain_impact_lookup_idx
    ON fact_records (
        (payload->>'cve_id'),
        (payload->>'impact_status'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_supply_chain_impact_status_lookup_idx
    ON fact_records (
        (payload->>'impact_status'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_supply_chain_impact_package_lookup_idx
    ON fact_records (
        (payload->>'package_id'),
        (payload->>'repository_id'),
        (payload->>'subject_digest'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_supply_chain_impact_priority_lookup_idx
    ON fact_records (
        (payload->>'priority_bucket'),
        (COALESCE(NULLIF(payload->>'priority_score', '')::int, 0)),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_supply_chain_impact_repository_lookup_idx
    ON fact_records (
        (payload->>'repository_id'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;

-- #3389: the impact aggregate (GET /api/v0/supply-chain/impact/findings/count)
-- enumerates every active reducer_supply_chain_impact_finding fact with no
-- payload anchor in the common "count everything" case. The payload-leading
-- indexes above cannot bound that no-anchor enumeration to the fact_kind, so the
-- planner falls back to a whole-table scan at collector scale. This partial
-- index's predicate bounds the scan to exactly this fact_kind's active
-- tuples, and its (scope_id, generation_id) leading keys resolve the
-- ingestion_scopes/scope_generations active-generation join straight from the
-- index (index-only when the heap is vacuum-fresh, a bounded index scan
-- otherwise).
CREATE INDEX IF NOT EXISTS fact_records_supply_chain_impact_active_scan_idx
    ON fact_records (
        scope_id,
        generation_id,
        fact_id ASC
    )
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_security_alert_repository_lookup_idx
    ON fact_records (
        (payload->>'repository_id'),
        (payload->>'provider'),
        (payload->>'package_id'),
        (payload->>'provider_state'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'security_alert.repository_alert'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_security_alert_cve_ids_idx
    ON fact_records USING GIN ((payload->'cve_ids'))
    WHERE fact_kind = 'security_alert.repository_alert'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_security_alert_ghsa_ids_idx
    ON fact_records USING GIN ((payload->'ghsa_ids'))
    WHERE fact_kind = 'security_alert.repository_alert'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_security_alert_reconciliation_lookup_idx
    ON fact_records (
        (payload->>'repository_id'),
        (payload->>'package_id'),
        (payload->>'reconciliation_status'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_security_alert_reconciliation'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_security_alert_reconciliation_provider_repository_idx
    ON fact_records (
        (payload->>'provider_repository_id'),
        (payload->>'package_id'),
        (payload->>'reconciliation_status'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_security_alert_reconciliation'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_security_alert_reconciliation_scope_idx
    ON fact_records (
        (payload->>'scope_id'),
        (payload->>'package_id'),
        (payload->>'reconciliation_status'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_security_alert_reconciliation'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_security_alert_reconciliation_provider_idx
    ON fact_records (
        (payload->>'provider'),
        (payload->>'provider_state'),
        (payload->>'reconciliation_status'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_security_alert_reconciliation'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_security_alert_reconciliation_cve_ids_idx
    ON fact_records USING GIN ((payload->'cve_ids'))
    WHERE fact_kind = 'reducer_security_alert_reconciliation'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_security_alert_reconciliation_ghsa_ids_idx
    ON fact_records USING GIN ((payload->'ghsa_ids'))
    WHERE fact_kind = 'reducer_security_alert_reconciliation'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_vulnerability_affected_package_lookup_idx
    ON fact_records (
        (payload->>'package_id'),
        (payload->>'cve_id'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'vulnerability.affected_package'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_vulnerability_affected_product_lookup_idx
    ON fact_records (
        (payload->>'cve_id'),
        (payload->>'criteria'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'vulnerability.affected_product'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_sbom_component_purl_idx
    ON fact_records (
        (payload->>'purl'),
        (payload->>'document_id'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'sbom.component'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_sbom_component_cpe_idx
    ON fact_records (
        (payload->>'cpe'),
        (payload->>'document_id'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'sbom.component'
      AND is_tombstone = FALSE;
`
