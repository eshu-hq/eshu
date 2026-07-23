// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const listSupplyChainImpactReadinessQueryUnsupportedAndSource = `
package_dependency_gap_active AS (
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
      AND fact.payload->'entity_metadata'->>'config_kind' IN (
          'vcs_dependency',
          'path_dependency',
          'url_dependency',
          'editable_dependency',
          'unsupported_dependency'
      )
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
          ('npm', 'nuget', 'maven', 'cargo', 'pypi', 'swift', 'composer', 'go', 'rubygems', 'hex')
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
    -- VCS, path, URL, editable, and other provenance-only dependency rows
    -- identify real owned dependency inputs but do not prove a registry
    -- package version. Keep them as unsupported target evidence instead of
    -- admitting them as clean or silently dropping them from readiness.
    SELECT
        'dependency_source' AS target_kind,
        CASE payload->'entity_metadata'->>'config_kind'
            WHEN 'vcs_dependency' THEN 'vcs_dependency_unsupported'
            WHEN 'path_dependency' THEN 'path_dependency_unsupported'
            WHEN 'url_dependency' THEN 'url_dependency_unsupported'
            WHEN 'editable_dependency' THEN 'editable_dependency_unsupported'
            ELSE 'unsupported_dependency_unsupported'
        END AS reason,
        NULLIF(LOWER(TRIM(payload->'entity_metadata'->>'package_manager')), '') AS ecosystem,
        NULLIF(TRIM(payload->'entity_metadata'->>'package_manager_flavor'), '') AS lockfile_flavor,
        NULLIF(TRIM(payload->'entity_metadata'->>'config_kind'), '') AS feature_token
    FROM package_dependency_gap_active
    WHERE $11 <> ''
      AND payload->>'repo_id' = $11
    UNION ALL
    -- SBOM targets where the document parser recorded an unsupported field
    -- or a malformed document. Joined to sbom.document so the requested digest
    -- or image_ref-derived digest scope filter applies; sbom.warning payloads
    -- only carry document_id. Bounded to an explicit image anchor so a
    -- repository_id-only scope cannot collect SBOM warnings from unrelated
    -- images.
    SELECT
        'sbom_target' AS target_kind,
        NULLIF(TRIM(warn.payload->>'reason'), '') AS reason,
        NULL::text AS ecosystem,
        NULL::text AS lockfile_flavor,
        NULL::text AS feature_token
    FROM sbom_warning_active AS warn
    JOIN sbom_document_active AS doc
      ON doc.payload->>'document_id' = warn.payload->>'document_id'
    WHERE doc.payload->>'subject_digest' IN (SELECT digest FROM target_image_digests)
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
    WHERE warn.payload->>'warning_code' IN (
        'unsupported_metadata_source',
        'registry_not_found',
        'metadata_too_large',
        'malformed_metadata',
        'credentials_missing'
      )
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
    UNION ALL
    -- Scanner-worker image targets where an analyzer was not configured or
    -- cannot support the image shape. Bounded to an explicit digest, image_ref,
    -- or scope id so repository-scoped reads do not report image analyzer gaps
    -- globally.
    SELECT
        'image_target' AS target_kind,
        NULLIF(TRIM(warn.payload->>'reason'), '') AS reason,
        NULL::text AS ecosystem,
        NULL::text AS lockfile_flavor,
        NULL::text AS feature_token
    FROM scanner_worker_warning_active AS warn
    WHERE ($12 <> '' OR $14 <> '')
      AND warn.payload->>'target_kind' = 'image'
      AND warn.payload->>'reason' IN ('analyzer_not_configured', 'image_analyzer_unsupported_target')
      AND (
          (
              $12 <> ''
              AND (
                  warn.payload->>'image_digest' = $12
                  OR warn.scope_id = $12
                  OR RIGHT(warn.scope_id, LENGTH('@' || $12)) = '@' || $12
              )
          )
          OR (
              $14 <> ''
              AND (
                  warn.payload->>'image_ref' = $14
                  OR warn.scope_id = $14
                  OR RIGHT(warn.scope_id, LENGTH('@' || $14)) = '@' || $14
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
    FROM vulnerability_source_states AS state
    WHERE EXISTS (
        SELECT 1
        FROM target_vulnerability_source_scopes AS target
        WHERE (target.scope_id IS NOT NULL AND state.scope_id = target.scope_id)
           OR (
               target.scope_id IS NULL
               AND target.source = state.source
               AND target.ecosystem = NULLIF(LOWER(TRIM(state.ecosystem)), '')
           )
    )
    ORDER BY CASE WHEN scope_id IN ($9, $10, $11, $12, $14) THEN 0 ELSE 1 END,
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
),
os_package_active AS (
    -- vulnerability.os_package facts carry no image identity of their own
    -- (distro/package identity only); the scanned image is resolved through
    -- the sibling scanner_worker.analysis fact in the SAME scan scope below,
    -- the identical join the reducer uses to stamp SubjectDigest (issue
    -- #5463, go/internal/reducer/supply_chain_impact.go).
    SELECT fact.payload, fact.observed_at, fact.scope_id, fact.generation_id
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($15::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
scanner_worker_analysis_active AS (
    SELECT fact.payload, fact.observed_at, fact.scope_id, fact.generation_id
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($16::text[])
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
scanner_worker_analysis AS (
    -- Present whenever a scan completed for the requested image, whether or
    -- not it found any installed OS packages. This is the "was this image
    -- ever scanned" signal (issue #5467).
    SELECT
        'scanner_worker.analysis' AS family,
        COUNT(*)::int AS fact_count,
        MAX(observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM scanner_worker_analysis_active
    WHERE payload->>'image_digest' IN (SELECT digest FROM target_image_digests)
       OR ($14 <> '' AND payload->>'image_reference' = $14)
),
vulnerability_os_package AS (
    -- Counted only for os_package facts whose sibling scanner_worker.analysis
    -- (same scope_id + generation_id) resolves to the requested image, so an
    -- unrelated scan scope's installed-package rows never count toward this
    -- request's evidence. Deliberately a semi-join (EXISTS), not a JOIN: a
    -- JOIN's COUNT(*) is COUNT(os_package rows) * COUNT(matching analysis
    -- rows), which is only correct because exactly one active
    -- scanner_worker.analysis fact exists per scope+generation today (one
    -- analyzer, one active generation). A second analyzer emitting into the
    -- same scope, or any other source of more than one matching analysis row,
    -- would silently inflate fact_count under a JOIN. EXISTS counts each
    -- os_package fact at most once regardless of how many analysis rows
    -- match.
    SELECT
        'vulnerability.os_package' AS family,
        COUNT(*)::int AS fact_count,
        MAX(os_package.observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM os_package_active AS os_package
    WHERE EXISTS (
        SELECT 1
        FROM scanner_worker_analysis_active AS analysis
        WHERE analysis.scope_id = os_package.scope_id
          AND analysis.generation_id = os_package.generation_id
          AND (
              analysis.payload->>'image_digest' IN (SELECT digest FROM target_image_digests)
              OR ($14 <> '' AND analysis.payload->>'image_reference' = $14)
          )
      )
)
`
