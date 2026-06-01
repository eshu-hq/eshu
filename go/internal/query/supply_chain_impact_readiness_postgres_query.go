package query

const listSupplyChainImpactReadinessQuery = `
WITH advisory_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($1::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
sbom_document_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'sbom.document'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
sbom_warning_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'sbom.warning'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
exploitability_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($2::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
package_consumption_correlation_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($3::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
package_manifest_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'content_entity'
      AND fact.source_system = 'git'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND fact.payload->>'entity_type' = 'Variable'
      AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
),
package_registry_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($4::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
package_registry_warning_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'package_registry.warning'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
sbom_component_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($5::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
sbom_attestation_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($6::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
container_image_identity_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($7::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
vulnerability_source_snapshot_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($8::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
vulnerability_advisory AS (
    SELECT
        'vulnerability.advisory' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM advisory_active
    WHERE ($9 = '' OR payload->>'cve_id' = $9)
),
vulnerability_exploitability AS (
    SELECT
        'vulnerability.exploitability' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM exploitability_active
    WHERE ($9 = '' OR payload->>'cve_id' = $9)
),
package_consumption_correlation AS (
    SELECT
        'package.consumption' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM package_consumption_correlation_active
    WHERE ($11 = '' OR payload->>'repository_id' = $11)
      AND ($10 = '' OR payload->>'package_id' = $10)
),
package_manifest_dependency AS (
    SELECT
        'package.consumption' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM package_manifest_active
    WHERE ($11 = '' OR payload->>'repo_id' = $11)
),
package_registry_scope_packages AS (
    SELECT DISTINCT NULLIF(TRIM($10), '') AS package_id
    WHERE $10 <> ''
    UNION
    SELECT DISTINCT NULLIF(TRIM(consumption.payload->>'package_id'), '') AS package_id
    FROM package_consumption_correlation_active AS consumption
    WHERE $11 <> ''
      AND consumption.payload->>'repository_id' = $11
      AND NULLIF(TRIM(consumption.payload->>'package_id'), '') IS NOT NULL
),
package_registry_scoped AS (
    SELECT
        package_registry_scope_packages.package_id,
        COUNT(package_registry_active.payload)::int AS fact_count,
        MAX(package_registry_active.observed_at) AS latest_observed_at
    FROM package_registry_scope_packages
    LEFT JOIN package_registry_active
      ON package_registry_active.payload->>'package_id' = package_registry_scope_packages.package_id
    GROUP BY package_registry_scope_packages.package_id
),
package_registry AS (
    SELECT
        'package.registry' AS family,
        COALESCE(SUM(fact_count), 0)::int AS fact_count,
        CASE
            WHEN COUNT(*) = 0 THEN NULL::timestamptz
            WHEN BOOL_OR(fact_count = 0) THEN NULL::timestamptz
            ELSE MIN(latest_observed_at)
        END AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM package_registry_scoped
),
sbom_component AS (
    SELECT
        'sbom.component' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM sbom_component_active
    WHERE $12 <> '' AND payload->>'subject_digest' = $12
),
sbom_attestation AS (
    SELECT
        'sbom.attestation' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM sbom_attestation_active
    WHERE $12 <> '' AND payload->>'subject_digest' = $12
),
container_image_identity AS (
    SELECT
        'container_image.identity' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM container_image_identity_active
    WHERE $12 <> '' AND payload->>'digest' = $12
),
vulnerability_source_snapshot AS (
    SELECT
        'vulnerability.source_snapshot' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        BOOL_OR(payload @> '{"complete": false}'::jsonb) AS target_incomplete,
        ARRAY_REMOVE(
            ARRAY_AGG(DISTINCT NULLIF(TRIM(payload->>'warning_message'), ''))
                FILTER (WHERE payload @> '{"complete": false}'::jsonb),
            NULL
        ) AS incomplete_reasons,
        COALESCE(
            JSONB_AGG(DISTINCT JSONB_STRIP_NULLS(JSONB_BUILD_OBJECT(
                'source', payload->>'source',
                'ecosystem', payload->>'ecosystem',
                'cache_artifact_version', payload->>'cache_artifact_version',
                'snapshot_digest', payload->>'cache_snapshot_digest',
                'last_updated_at', payload->>'cache_updated_at',
                'freshness', payload->>'cache_freshness',
                'complete', payload @> '{"complete": true}'::jsonb,
                'warning_code', payload->>'warning_code',
                'warning_message', payload->>'warning_message'
            ))) FILTER (WHERE payload IS NOT NULL),
            '[]'::jsonb
        )::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM vulnerability_source_snapshot_active
),
unsupported_target_rows AS (
    -- Owned dependency rows in an ecosystem the supply-chain matcher cannot
    -- resolve (no precise version/range match available today). Reported as
    -- target_kind=ecosystem so callers see real observed coverage gaps.
    -- Bounded to an explicit repository_id anchor so a cve_id-only or
    -- subject_digest-only scope cannot trigger an unbounded global scan of
    -- every dependency row in the fact store.
    SELECT
        'ecosystem' AS target_kind,
        'unsupported_ecosystem' AS reason,
        NULLIF(LOWER(TRIM(payload->'entity_metadata'->>'package_manager')), '') AS ecosystem,
        NULL::text AS lockfile_flavor,
        NULL::text AS feature_token
    FROM package_manifest_active
    WHERE $11 <> ''
      AND payload->>'repo_id' = $11
      AND NULLIF(LOWER(TRIM(payload->'entity_metadata'->>'package_manager')), '') IS NOT NULL
      AND LOWER(TRIM(payload->'entity_metadata'->>'package_manager')) NOT IN
          ('npm', 'nuget', 'maven', 'cargo', 'pypi', 'swift', 'composer', 'go')
    UNION ALL
    -- Package-manager files Eshu parsed but where the lockfile recorded an
    -- unsupported feature (e.g., Yarn Berry patch directives). The row was
    -- admitted so the dependency identity is preserved, but the lockfile
    -- chain cannot prove exact-version impact for this entry. Bounded to
    -- an explicit repository_id anchor for the same reason.
    SELECT
        'package_manager_file' AS target_kind,
        'lockfile_unsupported_feature' AS reason,
        NULL::text AS ecosystem,
        NULLIF(TRIM(payload->'entity_metadata'->>'package_manager_flavor'), '') AS lockfile_flavor,
        NULLIF(TRIM(payload->'entity_metadata'->>'lockfile_unsupported_feature'), '') AS feature_token
    FROM package_manifest_active
    WHERE $11 <> ''
      AND payload->>'repo_id' = $11
      AND NULLIF(TRIM(payload->'entity_metadata'->>'lockfile_unsupported_feature'), '') IS NOT NULL
    UNION ALL
    -- SBOM targets where the document parser recorded an unsupported field
    -- or a malformed document. Joined to sbom.document so the subject digest
    -- scope filter applies; sbom.warning payloads only carry document_id.
    -- Bounded to an explicit subject_digest anchor so a repository_id-only
    -- scope cannot collect SBOM warnings from unrelated images.
    SELECT
        'sbom_target' AS target_kind,
        NULLIF(TRIM(warn.payload->>'reason'), '') AS reason,
        NULL::text AS ecosystem,
        NULL::text AS lockfile_flavor,
        NULL::text AS feature_token
    FROM sbom_warning_active AS warn
    JOIN sbom_document_active AS doc
      ON doc.payload->>'document_id' = warn.payload->>'document_id'
    WHERE $12 <> ''
      AND doc.payload->>'subject_digest' = $12
      AND warn.payload->>'reason' IN ('unsupported_field', 'malformed_document')
    UNION ALL
    -- Package-registry metadata documents Eshu observed but skipped because
    -- the source body exceeded the configured byte limit. Bounded to a
    -- package_id anchor, or to a repository_id with an already-materialized
    -- package consumption correlation for the same package_id, so cve-only
    -- and subject-only scopes cannot scan warning facts globally.
    SELECT
        'package_registry_metadata' AS target_kind,
        NULLIF(TRIM(warn.payload->>'warning_code'), '') AS reason,
        NULLIF(LOWER(TRIM(warn.payload->>'ecosystem')), '') AS ecosystem,
        NULL::text AS lockfile_flavor,
        NULL::text AS feature_token
    FROM package_registry_warning_active AS warn
    WHERE warn.payload->>'warning_code' = 'metadata_too_large'
      AND (
          ($10 <> '' AND warn.payload->>'package_id' = $10)
          OR (
              $11 <> ''
              AND EXISTS (
                  SELECT 1
                  FROM package_consumption_correlation_active AS consumption
                  WHERE consumption.payload->>'repository_id' = $11
                    AND consumption.payload->>'package_id' = warn.payload->>'package_id'
              )
          )
      )
),
unsupported_target AS (
    SELECT
        'vulnerability.unsupported_target' AS family,
        COUNT(*)::int AS fact_count,
        NULL::timestamptz AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        COALESCE(
            (
                SELECT JSONB_AGG(entry ORDER BY entry->>'target_kind', entry->>'reason', entry->>'ecosystem', entry->>'lockfile_flavor', entry->>'feature_token')
                FROM (
                    SELECT JSONB_STRIP_NULLS(JSONB_BUILD_OBJECT(
                        'target_kind', target_kind,
                        'reason', reason,
                        'ecosystem', ecosystem,
                        'lockfile_flavor', lockfile_flavor,
                        'feature_token', feature_token,
                        'count', COUNT(*)::int
                    )) AS entry
                    FROM unsupported_target_rows
                    GROUP BY target_kind, reason, ecosystem, lockfile_flavor, feature_token
                ) AS grouped_targets
            ),
            '[]'::jsonb
        )::text AS unsupported_targets_json
    FROM unsupported_target_rows
),
vulnerability_source_state_candidates AS (
    SELECT
        collector_instance_id,
        scope_id,
        source,
        ecosystem,
        window_start,
        window_end,
        last_attempt_at,
        last_success_at,
        next_retry_at,
        last_error_class,
        freshness_state,
        terminal_status,
        result_count,
        warning_count,
        updated_at
    FROM vulnerability_source_states
    WHERE scope_id IN ($9, $10, $11, $12)
       OR scope_id NOT LIKE 'vuln-intel://osv/%/%?version=%'
    ORDER BY CASE WHEN scope_id IN ($9, $10, $11, $12) THEN 0 ELSE 1 END,
        updated_at DESC,
        source ASC,
        scope_id ASC
    LIMIT 200
),
vulnerability_source_state AS (
    SELECT
        'vulnerability.source_state' AS family,
        0::int AS fact_count,
        MAX(updated_at) AS latest_observed_at,
        BOOL_OR(freshness_state IN ('pending', 'stale', 'rate_limited', 'failed', 'partial')) AS target_incomplete,
        ARRAY_REMOVE(
            ARRAY_AGG(DISTINCT source || ':' || freshness_state)
                FILTER (WHERE freshness_state IN ('pending', 'stale', 'rate_limited', 'failed', 'partial')),
            NULL
        ) AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        COALESCE(
            JSONB_AGG(DISTINCT JSONB_STRIP_NULLS(JSONB_BUILD_OBJECT(
                'collector_instance_id', collector_instance_id,
                'scope_id', scope_id,
                'source', source,
                'ecosystem', ecosystem,
                'collection_window', JSONB_STRIP_NULLS(JSONB_BUILD_OBJECT(
                    'start', TO_CHAR(window_start AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
                    'end', TO_CHAR(window_end AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
                )),
                'last_attempt_at', TO_CHAR(last_attempt_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
                'last_success_at', TO_CHAR(last_success_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
                'next_retry_at', TO_CHAR(next_retry_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
                'last_error_class', last_error_class,
                'freshness_state', freshness_state,
                'terminal_status', terminal_status,
                'result_count', result_count,
                'warning_count', warning_count,
                'updated_at', TO_CHAR(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
            ))) FILTER (WHERE scope_id IS NOT NULL),
            '[]'::jsonb
        )::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM vulnerability_source_state_candidates
)
SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM vulnerability_advisory
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM vulnerability_exploitability
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM package_consumption_correlation
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM package_manifest_dependency
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM package_registry
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM sbom_component
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM sbom_attestation
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM container_image_identity
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM vulnerability_source_snapshot
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM vulnerability_source_state
UNION ALL SELECT family, fact_count, latest_observed_at, target_incomplete, incomplete_reasons, source_snapshots_json, source_states_json, unsupported_targets_json FROM unsupported_target
`
