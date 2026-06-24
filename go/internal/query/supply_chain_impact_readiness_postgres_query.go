// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const listSupplyChainImpactReadinessQuery = listSupplyChainImpactReadinessQueryCore +
	listSupplyChainImpactReadinessQueryUnsupportedAndSource +
	listSupplyChainImpactReadinessQuerySelect

const listSupplyChainImpactReadinessQueryCore = `
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
scanner_worker_warning_active AS (
    SELECT fact.scope_id, fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'scanner_worker.warning'
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
    SELECT fact.scope_id, fact.payload, fact.observed_at
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
target_image_digests AS (
    SELECT DISTINCT NULLIF(TRIM($12), '') AS digest
    WHERE $12 <> ''
    UNION
    SELECT DISTINCT NULLIF(TRIM(identity.payload->>'digest'), '') AS digest
    FROM container_image_identity_active AS identity
    WHERE $14 <> ''
      AND identity.payload->>'image_ref' = $14
      AND NULLIF(TRIM(identity.payload->>'digest'), '') IS NOT NULL
),
target_vulnerability_source_ecosystems AS (
    SELECT DISTINCT NULLIF(LOWER(TRIM(payload->'entity_metadata'->>'package_manager')), '') AS ecosystem
    FROM package_manifest_active
    WHERE $11 <> ''
      AND payload->>'repo_id' = $11
    UNION
    SELECT DISTINCT NULLIF(LOWER(TRIM(consumption.payload->>'ecosystem')), '') AS ecosystem
    FROM package_consumption_correlation_active AS consumption
    WHERE ($11 <> '' AND consumption.payload->>'repository_id' = $11)
       OR ($10 <> '' AND consumption.payload->>'package_id' = $10)
    UNION
    SELECT DISTINCT NULLIF(LOWER(TRIM(registry.payload->>'package_manager')), '') AS ecosystem
    FROM package_registry_active AS registry
    WHERE $10 <> ''
      AND registry.payload->>'package_id' = $10
    UNION
    SELECT DISTINCT NULLIF(LOWER(TRIM(component.payload->>'ecosystem')), '') AS ecosystem
    FROM sbom_component_active AS component
    WHERE component.payload->>'subject_digest' IN (SELECT digest FROM target_image_digests)
    UNION
    SELECT DISTINCT NULLIF(LOWER(TRIM(
        CASE
            WHEN $10 LIKE 'pkg:%' THEN SPLIT_PART(SUBSTRING($10 FROM 5), '/', 1)
            WHEN POSITION('://' IN $10) > 0 THEN SPLIT_PART($10, '://', 1)
            ELSE ''
        END
    )), '') AS ecosystem
    WHERE $10 <> ''
),
target_vulnerability_source_scopes AS (
    SELECT DISTINCT NULLIF(TRIM($9), '') AS scope_id, NULL::text AS source, NULL::text AS ecosystem
    WHERE $9 <> ''
    UNION
    SELECT DISTINCT 'vuln-intel://nvd/cve' AS scope_id, 'nvd' AS source, NULL::text AS ecosystem
    WHERE $9 <> ''
    UNION
    SELECT DISTINCT 'vuln-intel://nvd/' || TRIM($9) AS scope_id, 'nvd' AS source, NULL::text AS ecosystem
    WHERE $9 <> ''
    UNION
    SELECT DISTINCT 'vuln-intel://cisa/kev' AS scope_id, 'cisa_kev' AS source, NULL::text AS ecosystem
    WHERE $9 <> ''
    UNION
    SELECT DISTINCT 'vuln-intel://first/epss' AS scope_id, 'first_epss' AS source, NULL::text AS ecosystem
    WHERE $9 <> ''
    UNION
    SELECT DISTINCT NULL::text AS scope_id, 'osv' AS source, ecosystem
    FROM target_vulnerability_source_ecosystems
    WHERE ecosystem IS NOT NULL
    UNION
    SELECT DISTINCT NULL::text AS scope_id, 'glad' AS source, ecosystem
    FROM target_vulnerability_source_ecosystems
    WHERE ecosystem IS NOT NULL
),
target_advisory_packages AS (
    SELECT DISTINCT NULLIF(TRIM($10), '') AS package_id
    WHERE $10 <> ''
    UNION
    SELECT DISTINCT NULLIF(TRIM(consumption.payload->>'package_id'), '') AS package_id
    FROM package_consumption_correlation_active AS consumption
    WHERE $11 <> ''
      AND consumption.payload->>'repository_id' = $11
      AND NULLIF(TRIM(consumption.payload->>'package_id'), '') IS NOT NULL
    UNION
    SELECT DISTINCT NULLIF(TRIM(component.payload->>'package_id'), '') AS package_id
    FROM sbom_component_active AS component
    WHERE component.payload->>'subject_digest' IN (SELECT digest FROM target_image_digests)
      AND NULLIF(TRIM(component.payload->>'package_id'), '') IS NOT NULL
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
    WHERE (
          ($9 <> '' AND payload->>'cve_id' = $9)
          OR (($10 <> '' OR $11 <> '' OR EXISTS (SELECT 1 FROM target_image_digests))
              AND payload->>'package_id' IN (SELECT package_id FROM target_advisory_packages))
          OR ($9 = '' AND $10 = '' AND $11 = '' AND $12 = '' AND $14 = '')
      )
      AND (
          $13 = ''
          OR payload->>'advisory_id' = $13
          OR payload->>'ghsa_id' = $13
          OR payload->>'osv_id' = $13
      )
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
    WHERE $13 = ''
      AND ($9 = '' OR payload->>'cve_id' = $9)
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
    WHERE payload->>'subject_digest' IN (SELECT digest FROM target_image_digests)
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
    WHERE payload->>'subject_digest' IN (SELECT digest FROM target_image_digests)
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
    WHERE payload->>'digest' IN (SELECT digest FROM target_image_digests)
       OR ($14 <> '' AND payload->>'image_ref' = $14)
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
                'source', payload->>'source', 'ecosystem', payload->>'ecosystem',
                'cache_artifact_version', payload->>'cache_artifact_version',
                'snapshot_digest', payload->>'cache_snapshot_digest',
                'last_updated_at', payload->>'cache_updated_at', 'freshness', payload->>'cache_freshness',
                'complete', payload @> '{"complete": true}'::jsonb,
                'warning_code', payload->>'warning_code', 'warning_message', payload->>'warning_message'
            ))) FILTER (WHERE payload IS NOT NULL),
            '[]'::jsonb
        )::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM vulnerability_source_snapshot_active AS snapshot
    WHERE EXISTS (
        SELECT 1
        FROM target_vulnerability_source_scopes AS target
        WHERE (target.scope_id IS NOT NULL AND snapshot.scope_id = target.scope_id)
           OR (
               target.scope_id IS NULL
               AND target.source = snapshot.payload->>'source'
               AND target.ecosystem = NULLIF(LOWER(TRIM(snapshot.payload->>'ecosystem')), '')
           )
    )
),
`
