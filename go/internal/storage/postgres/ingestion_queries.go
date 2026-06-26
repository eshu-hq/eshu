// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const listRepositoryCatalogQuery = `
SELECT payload
FROM fact_records
WHERE fact_kind = 'repository'
ORDER BY observed_at DESC, fact_id DESC
`

const listLatestRelationshipFactRecordsQuery = latestGenerationCTE + `
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
JOIN latest_generations AS latest
  ON latest.scope_id = fact.scope_id
 AND latest.generation_id = fact.generation_id
WHERE latest.generation_id IS NOT NULL
  AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
ORDER BY fact.observed_at ASC, fact.fact_id ASC
`

const upsertIngestionScopeQuery = `
INSERT INTO ingestion_scopes (
    scope_id,
    scope_kind,
    source_system,
    source_key,
    parent_scope_id,
    collector_kind,
    partition_key,
    observed_at,
    ingested_at,
    status,
    active_generation_id,
    payload
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb
)
ON CONFLICT (scope_id) DO UPDATE SET
    scope_kind = EXCLUDED.scope_kind,
    source_system = EXCLUDED.source_system,
    source_key = EXCLUDED.source_key,
    parent_scope_id = EXCLUDED.parent_scope_id,
    collector_kind = EXCLUDED.collector_kind,
    partition_key = EXCLUDED.partition_key,
    observed_at = EXCLUDED.observed_at,
    ingested_at = EXCLUDED.ingested_at,
    status = CASE
        WHEN ingestion_scopes.active_generation_id IS NOT NULL
            AND EXCLUDED.active_generation_id IS NULL
            AND EXCLUDED.status = 'pending'
        THEN ingestion_scopes.status
        ELSE EXCLUDED.status
    END,
    active_generation_id = CASE
        WHEN EXCLUDED.active_generation_id IS NOT NULL THEN EXCLUDED.active_generation_id
        ELSE ingestion_scopes.active_generation_id
    END,
    payload = EXCLUDED.payload
`

const upsertScopeGenerationQuery = `
INSERT INTO scope_generations (
    generation_id,
    scope_id,
    trigger_kind,
    freshness_hint,
    source_commit_sha,
    is_delta,
    observed_at,
    ingested_at,
    status,
    activated_at,
    superseded_at,
    payload
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULL, '{}'::jsonb
)
ON CONFLICT (generation_id) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    trigger_kind = EXCLUDED.trigger_kind,
    freshness_hint = EXCLUDED.freshness_hint,
    source_commit_sha = EXCLUDED.source_commit_sha,
    is_delta = EXCLUDED.is_delta,
    observed_at = EXCLUDED.observed_at,
    ingested_at = EXCLUDED.ingested_at,
    status = EXCLUDED.status,
    activated_at = EXCLUDED.activated_at,
    payload = EXCLUDED.payload
`

const activeGenerationFreshnessQuery = `
SELECT generation.generation_id, COALESCE(generation.freshness_hint, '')
FROM scope_generations AS generation
WHERE generation.scope_id = $1
  AND generation.status IN ('pending', 'active')
  AND COALESCE(generation.freshness_hint, '') <> ''
ORDER BY generation.ingested_at DESC, generation.generation_id DESC
LIMIT 1
`

const activeRepositoryGenerationsQuery = latestGenerationCTE + `
SELECT DISTINCT ON (repo_id)
    repo_id,
    fact.scope_id,
    fact.generation_id
FROM (
    SELECT
        COALESCE(
            fact.payload->>'repo_id',
            fact.payload->>'graph_id',
            fact.payload->>'name',
            ''
        ) AS repo_id,
        fact.scope_id,
        fact.generation_id,
        fact.observed_at,
        fact.fact_id
    FROM fact_records AS fact
    JOIN latest_generations AS latest
      ON latest.scope_id = fact.scope_id
     AND latest.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'repository'
) AS fact
WHERE repo_id <> ''
ORDER BY repo_id, observed_at DESC, fact_id DESC
`

// activeScopeGenerationPartitionsQuery lists every (scope_id, generation_id) the
// shared latestGenerationCTE resolves as the latest generation for its scope. The
// deferred relationship backfill's per-scope fact load (issue #3710) partitions on
// this set rather than on activeRepositoryGenerationsQuery: the latter filters
// fact_kind = 'repository', so it covers only git scopes and silently omits cloud
// scopes (for example gcp:project:...:relationship:global) that carry
// gcp_cloud_relationship facts but no repository fact. Partitioning on those
// repository rows would drop the gcp_cloud_relationship arm of the deferred query
// entirely. This query instead covers EVERY scope with a latest generation — the
// exact set the deferred query already joins to through latest_generations — so no
// content/file/gcp_cloud_relationship fact's scope is missing from the partition
// map. Keying on the (scope_id, generation_id) PAIR also avoids the repo_id
// collapse: activeRepositoryGenerationsQuery keys DISTINCT ON (repo_id) over a
// COALESCE(repo_id, graph_id, name) value, so two distinct scopes whose COALESCE
// collides would share one map entry and one scope's facts would never load.
const activeScopeGenerationPartitionsQuery = latestGenerationCTE + `
SELECT scope_id, generation_id
FROM latest_generations
WHERE generation_id IS NOT NULL
ORDER BY scope_id, generation_id
`

const listSucceededDeploymentMappingWorkItemsQuery = `
SELECT work_item_id
FROM fact_work_items
WHERE stage = 'reducer'
  AND domain = 'deployment_mapping'
  AND status = 'succeeded'
ORDER BY updated_at ASC, work_item_id ASC
`

// listSucceededCodeImportRepoEdgeWorkItemsQuery selects the code-import
// repo-edge reducer work items that already succeeded. They are replayed after
// deferred maintenance so the projection re-runs once the cross-scope
// package-registry ownership facts it joins against are present — the same
// after-the-fact dependency the deployment_mapping reopen handles.
const listSucceededCodeImportRepoEdgeWorkItemsQuery = `
SELECT work_item_id
FROM fact_work_items
WHERE stage = 'reducer'
  AND domain = 'code_import_repo_edge'
  AND status = 'succeeded'
ORDER BY updated_at ASC, work_item_id ASC
`
