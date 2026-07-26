// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "fmt"

// supplyChainImpactRuntimeFilterCTE resolves only the runtime facts selected by
// the requested service, workload, or environment. The dimension-first
// branches let Postgres use the service-catalog, CI/CD, and workload-identity
// indexes before decoding repository anchors. The resulting repository set is
// current-state query truth; it does not mutate or backfill finding payloads.
//
// The arguments are internal SQL parameter expressions such as "$9", never
// request values. Callers retain parameterized queries.
func supplyChainImpactRuntimeFilterCTE(
	serviceParam string,
	workloadParam string,
	environmentParam string,
) string {
	return fmt.Sprintf(`
runtime_filter_candidates AS NOT MATERIALIZED (
  SELECT 'workload'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM fact_records AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE %[2]s <> ''
    AND runtime_fact.fact_kind = 'reducer_workload_identity'
    AND runtime_fact.is_tombstone = FALSE
    AND runtime_generation.status = 'active'
    AND (
          runtime_fact.payload->>'workload_id' = %[2]s
          OR (
               %[2]s LIKE 'workload:%%'
               AND runtime_fact.payload->'entity_keys' ? %[2]s
             )
        )

  UNION ALL

  SELECT 'workload'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM fact_records AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE %[2]s <> ''
    AND runtime_fact.fact_kind = 'reducer_service_catalog_correlation'
    AND runtime_fact.is_tombstone = FALSE
    AND runtime_generation.status = 'active'
    AND BTRIM(COALESCE(runtime_fact.payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND COALESCE(runtime_fact.payload->'provenance_only', 'false'::jsonb) <> 'true'::jsonb
    AND runtime_fact.payload->>'workload_id' = %[2]s

  UNION ALL

  SELECT 'service'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM fact_records AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE %[1]s <> ''
    AND runtime_fact.fact_kind = 'reducer_service_catalog_correlation'
    AND runtime_fact.is_tombstone = FALSE
    AND runtime_generation.status = 'active'
    AND BTRIM(COALESCE(runtime_fact.payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND COALESCE(runtime_fact.payload->'provenance_only', 'false'::jsonb) <> 'true'::jsonb
    AND runtime_fact.payload->>'service_id' = %[1]s

  UNION ALL

  SELECT 'environment'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM fact_records AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE %[3]s <> ''
    AND runtime_fact.fact_kind = 'reducer_ci_cd_run_correlation'
    AND runtime_fact.is_tombstone = FALSE
    AND runtime_generation.status = 'active'
    AND BTRIM(COALESCE(runtime_fact.payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND COALESCE(runtime_fact.payload->'provenance_only', 'false'::jsonb) <> 'true'::jsonb
    AND runtime_fact.payload->>'environment' = %[3]s
),
runtime_filter_repository_candidates AS (
  SELECT runtime_fact.filter_kind,
         COALESCE(
           NULLIF(BTRIM(runtime_fact.payload->>'repository_id'), ''),
           NULLIF(BTRIM(runtime_fact.payload->>'repo_id'), ''),
           CASE
             WHEN BTRIM(runtime_fact.payload->>'scope_id') LIKE 'repository:%%'
               THEN BTRIM(runtime_fact.payload->>'scope_id')
             WHEN BTRIM(runtime_fact.payload->>'scope_id') LIKE 'git-repository-scope:%%'
               THEN NULLIF(BTRIM(SUBSTRING(runtime_fact.payload->>'scope_id' FROM 22)), '')
           END,
           CASE
             WHEN BTRIM(runtime_fact.scope_id) LIKE 'repository:%%'
               THEN BTRIM(runtime_fact.scope_id)
             WHEN BTRIM(runtime_fact.scope_id) LIKE 'git-repository-scope:%%'
               THEN NULLIF(BTRIM(SUBSTRING(runtime_fact.scope_id FROM 22)), '')
           END,
           related_scope.repository_id
         ) AS repository_id
  FROM runtime_filter_candidates AS runtime_fact
  LEFT JOIN LATERAL (
    SELECT CASE
             WHEN BTRIM(related.value) LIKE 'repository:%%'
               THEN BTRIM(related.value)
             WHEN BTRIM(related.value) LIKE 'git-repository-scope:%%'
               THEN NULLIF(BTRIM(SUBSTRING(related.value FROM 22)), '')
           END AS repository_id
    FROM jsonb_array_elements_text(
      CASE
        WHEN jsonb_typeof(runtime_fact.payload->'related_scope_ids') = 'array'
          THEN runtime_fact.payload->'related_scope_ids'
        ELSE '[]'::jsonb
      END
    ) WITH ORDINALITY AS related(value, ordinal)
    WHERE BTRIM(related.value) LIKE 'repository:%%'
       OR BTRIM(related.value) LIKE 'git-repository-scope:%%'
    ORDER BY related.ordinal
    LIMIT 1
  ) AS related_scope ON TRUE
),
runtime_filter_repositories AS MATERIALIZED (
  SELECT DISTINCT filter_kind, repository_id
  FROM runtime_filter_repository_candidates
  WHERE COALESCE(repository_id, '') <> ''
)`, serviceParam, workloadParam, environmentParam)
}

// supplyChainImpactRuntimeFilterPredicate keeps the legacy baked values valid
// while also accepting a repository whose current runtime facts match. Each
// dimension checks its own candidate set so combined filters retain AND
// semantics.
func supplyChainImpactRuntimeFilterPredicate(
	repositoryExpr string,
	bakedServiceExpr string,
	bakedWorkloadExpr string,
	bakedEnvironmentExpr string,
	serviceParam string,
	workloadParam string,
	environmentParam string,
) string {
	return fmt.Sprintf(`
  AND (
        %[5]s = ''
        OR %[2]s ? %[5]s
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'service'
               AND runtime_filter.repository_id = %[1]s
           )
      )
  AND (
        %[6]s = ''
        OR %[3]s ? %[6]s
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'workload'
               AND runtime_filter.repository_id = %[1]s
           )
      )
  AND (
        %[7]s = ''
        OR %[4]s ? %[7]s
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'environment'
               AND runtime_filter.repository_id = %[1]s
           )
      )`,
		repositoryExpr,
		bakedServiceExpr,
		bakedWorkloadExpr,
		bakedEnvironmentExpr,
		serviceParam,
		workloadParam,
		environmentParam,
	)
}
