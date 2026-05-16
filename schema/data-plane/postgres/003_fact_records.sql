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

CREATE INDEX IF NOT EXISTS fact_records_stable_key_idx
    ON fact_records (stable_fact_key, generation_id);

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

CREATE INDEX IF NOT EXISTS fact_records_documentation_findings_visible_idx
    ON fact_records (
        (payload->>'finding_type'),
        (payload->>'source_id'),
        (payload->>'document_id'),
        (payload->>'status'),
        (payload->>'truth_level'),
        (payload->>'freshness_state'),
        observed_at DESC,
        fact_id DESC
    )
    WHERE fact_kind = 'documentation_finding'
      AND is_tombstone = FALSE
      AND (payload->'permissions'->>'viewer_can_read_source') = 'true'
      AND LOWER(COALESCE(payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'
      AND LOWER(COALESCE(payload->'states'->>'permission_decision', '')) <> 'denied';

CREATE INDEX IF NOT EXISTS fact_records_documentation_packets_finding_idx
    ON fact_records (
        COALESCE(payload->>'finding_id', payload->'finding'->>'finding_id'),
        observed_at DESC,
        fact_id DESC
    )
    WHERE fact_kind = 'documentation_evidence_packet'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_documentation_packets_packet_idx
    ON fact_records (
        (payload->>'packet_id'),
        observed_at DESC,
        fact_id DESC
    )
    WHERE fact_kind = 'documentation_evidence_packet'
      AND is_tombstone = FALSE;

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

CREATE INDEX IF NOT EXISTS fact_records_package_correlations_lookup_idx
    ON fact_records (
        (payload->>'package_id'),
        (payload->>'repository_id'),
        (payload->>'relationship_kind'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind IN (
        'reducer_package_ownership_correlation',
        'reducer_package_consumption_correlation'
    )
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_package_correlations_repository_lookup_idx
    ON fact_records (
        (payload->>'repository_id'),
        (payload->>'package_id'),
        (payload->>'relationship_kind'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind IN (
        'reducer_package_ownership_correlation',
        'reducer_package_consumption_correlation'
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
