// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

const listSupplyChainImpactReadinessPackageManifestQuery = `
package_manifest_dependency AS (
    SELECT
        'package.consumption' AS family,
        COUNT(DISTINCT manifest.fact_id)::int AS fact_count,
        MAX(manifest.observed_at) AS latest_observed_at,
        NULL::boolean AS target_incomplete,
        NULL::text[] AS incomplete_reasons,
        NULL::text AS source_snapshots_json,
        NULL::text AS source_states_json,
        NULL::text AS unsupported_targets_json
    FROM (
        SELECT fact_id, observed_at
        FROM package_manifest_active
        WHERE NOT $20
          AND ($11 = '' OR payload->>'repo_id' = $11)
        UNION ALL
        SELECT fact.fact_id, fact.observed_at
        FROM package_manifest_consumption_keys AS consumption
        JOIN fact_records AS fact
          ON fact.fact_id = consumption.fact_id
         AND fact.scope_id = consumption.scope_id
         AND fact.generation_id = consumption.generation_id
        JOIN ingestion_scopes AS scope
          ON scope.scope_id = consumption.scope_id
         AND scope.active_generation_id = consumption.generation_id
        JOIN scope_generations AS generation
          ON generation.scope_id = consumption.scope_id
         AND generation.generation_id = consumption.generation_id
        WHERE $20
          AND fact.is_tombstone = FALSE
          AND generation.status = 'active'
          AND ($11 = '' OR consumption.repository_id = $11)
          AND EXISTS (
              SELECT 1
              FROM target_consumption_keys AS target
              WHERE target.ecosystem = consumption.ecosystem
                AND target.package_name = consumption.package_name
          )
    ) AS manifest
),
`
