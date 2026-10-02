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

// readinessPackageManifestActiveCTE reads the active dependency-variable
// content_entity facts of the anchored repository, in both payload shapes:
// entity_metadata.config_kind (current) and the legacy top-level config_kind
// (#7301). It is only scanned when $11 (repository id) is non-empty: every
// consumer requires a non-empty $11 or NOT $20, and an empty $11 always
// arrives with a target anchor that sets $20.
//
// #7088: each arm starts from the repository's own scopes
// (ingestion_scopes.source_key = $11, the repository id the git collector
// stamps on every repository and repository_ref scope; see
// TestBuildScopeRepositorySourceKeyMatchesMetadataRepoID) and probes
// fact_records once per scope through a repo-leading partial index with the
// repository, scope and active generation all in its Index Cond. The
// OFFSET 0 keeps the LATERAL subquery from being flattened, so the planner
// cannot start from the fact index and heap-fetch every generation's rows
// (8,832 fetched to keep 552 on ops-qa) or, for the legacy arm, probe every
// active scope (819 loops, 9.4 s warm / >30 s cold on ops-qa). The former
// empty-$11 escape is gone: it was unreachable, and under a generic plan it
// forced that per-scope probe. Arm 1 uses migration 121's index; the legacy
// arm uses migration 159's. Do not reuse this CTE without a repository
// anchor.
const readinessPackageManifestActiveCTE = `package_manifest_active AS (
    SELECT fact.fact_id, fact.payload, fact.observed_at
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS generation
      ON generation.scope_id = scope.scope_id
     AND generation.generation_id = scope.active_generation_id
    CROSS JOIN LATERAL (
        SELECT dependency.fact_id, dependency.payload, dependency.observed_at
        FROM fact_records AS dependency
        WHERE dependency.scope_id = scope.scope_id
          AND dependency.generation_id = scope.active_generation_id
          AND dependency.fact_kind = 'content_entity'
          AND dependency.source_system = 'git'
          AND dependency.is_tombstone = FALSE
          AND dependency.payload->>'entity_type' = 'Variable'
          AND dependency.payload->>'repo_id' = $11
          AND NULLIF(dependency.payload->>'config_kind', '') IS NULL
          AND dependency.payload->'entity_metadata'->>'config_kind' = 'dependency'
        OFFSET 0
    ) AS fact
    WHERE scope.source_key = $11
      AND generation.status = 'active'
    UNION ALL
    SELECT fact.fact_id, fact.payload, fact.observed_at
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS generation
      ON generation.scope_id = scope.scope_id
     AND generation.generation_id = scope.active_generation_id
    CROSS JOIN LATERAL (
        SELECT dependency.fact_id, dependency.payload, dependency.observed_at
        FROM fact_records AS dependency
        WHERE dependency.scope_id = scope.scope_id
          AND dependency.generation_id = scope.active_generation_id
          AND dependency.fact_kind = 'content_entity'
          AND dependency.source_system = 'git'
          AND dependency.is_tombstone = FALSE
          AND dependency.payload->>'entity_type' = 'Variable'
          AND dependency.payload->>'repo_id' = $11
          AND dependency.payload->>'config_kind' = 'dependency'
        OFFSET 0
    ) AS fact
    WHERE scope.source_key = $11
      AND generation.status = 'active'
),
`
