// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"strings"
)

const supplyChainImpactRuntimeFilterCTESQL = `
runtime_workload_identity_matches AS MATERIALIZED (
  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE %[2]s <> ''
    AND fact_kind = 'reducer_workload_identity'
    AND is_tombstone = FALSE
    AND (
          (
            jsonb_typeof(payload->'workload_id') = 'string'
            AND payload->>'workload_id' = %[2]s
          )
          OR (
               %[2]s LIKE 'workload:%%'
               AND jsonb_typeof(payload->'entity_keys') IN ('array', 'string')
               AND payload->'entity_keys' ? %[2]s
             )
        )

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE %[2]s <> ''
    AND fact_kind = 'reducer_workload_identity'
    AND is_tombstone = FALSE
    AND NOT COALESCE(
          (
            jsonb_typeof(payload->'workload_id') = 'string'
            AND payload->>'workload_id' = %[2]s
          )
          OR (
               %[2]s LIKE 'workload:%%'
               AND jsonb_typeof(payload->'entity_keys') IN ('array', 'string')
               AND payload->'entity_keys' ? %[2]s
             ),
          FALSE
        )
    AND jsonb_typeof(payload->'workload_id') = 'string'
    AND payload->>'workload_id' IS DISTINCT FROM BTRIM(payload->>'workload_id')
    AND BTRIM(payload->>'workload_id') = %[2]s

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE %[2]s <> ''
    AND fact_kind = 'reducer_workload_identity'
    AND is_tombstone = FALSE
    AND NOT COALESCE(
          (
            jsonb_typeof(payload->'workload_id') = 'string'
            AND payload->>'workload_id' = %[2]s
          )
          OR (
               %[2]s LIKE 'workload:%%'
               AND jsonb_typeof(payload->'entity_keys') IN ('array', 'string')
               AND payload->'entity_keys' ? %[2]s
             ),
          FALSE
        )
    AND NOT COALESCE(
          jsonb_typeof(payload->'workload_id') = 'string'
          AND BTRIM(payload->>'workload_id') = %[2]s,
          FALSE
        )
    AND jsonb_path_exists(payload->'entity_keys',
          'lax $[*] ? (@.type() == "string" && @ like_regex "(^ | $)")')
    AND %[2]s LIKE 'workload:%%'
    AND EXISTS (
          SELECT 1
          FROM jsonb_array_elements_text(
            CASE
              WHEN jsonb_typeof(payload->'entity_keys') = 'array'
                THEN payload->'entity_keys'
              WHEN jsonb_typeof(payload->'entity_keys') = 'string'
                THEN jsonb_build_array(payload->'entity_keys')
              ELSE '[]'::jsonb
            END
          ) AS entity_key(value)
          WHERE BTRIM(entity_key.value) = %[2]s
            AND BTRIM(entity_key.value) LIKE 'workload:%%'
        )
),
runtime_workload_catalog_matches AS MATERIALIZED (
  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE %[2]s <> ''
    AND fact_kind = 'reducer_service_catalog_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'workload_id') = 'string'
    AND payload->>'workload_id' = %[2]s

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE %[2]s <> ''
    AND fact_kind = 'reducer_service_catalog_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'workload_id') = 'string'
    AND payload->>'workload_id' IS DISTINCT FROM %[2]s
    AND payload->>'workload_id' IS DISTINCT FROM BTRIM(payload->>'workload_id')
    AND BTRIM(payload->>'workload_id') = %[2]s
),
runtime_service_matches AS MATERIALIZED (
  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE %[1]s <> ''
    AND fact_kind = 'reducer_service_catalog_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'service_id') = 'string'
    AND payload->>'service_id' = %[1]s

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE %[1]s <> ''
    AND fact_kind = 'reducer_service_catalog_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'service_id') = 'string'
    AND payload->>'service_id' IS DISTINCT FROM %[1]s
    AND payload->>'service_id' IS DISTINCT FROM BTRIM(payload->>'service_id')
    AND BTRIM(payload->>'service_id') = %[1]s
),
runtime_environment_matches AS MATERIALIZED (
  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE %[3]s <> ''
    AND fact_kind = 'reducer_ci_cd_run_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'environment') = 'string'
    AND payload->>'environment' = %[3]s

  UNION ALL

  SELECT scope_id, generation_id, payload
  FROM fact_records
  WHERE %[3]s <> ''
    AND fact_kind = 'reducer_ci_cd_run_correlation'
    AND is_tombstone = FALSE
    AND BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'
    AND jsonb_typeof(payload->'environment') = 'string'
    AND payload->>'environment' IS DISTINCT FROM %[3]s
    AND payload->>'environment' IS DISTINCT FROM BTRIM(payload->>'environment')
    AND BTRIM(payload->>'environment') = %[3]s
),
runtime_filter_candidates AS NOT MATERIALIZED (
  SELECT 'workload'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM runtime_workload_identity_matches AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE runtime_generation.status = 'active'

  UNION ALL

  SELECT 'workload'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM runtime_workload_catalog_matches AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE runtime_generation.status = 'active'

  UNION ALL

  SELECT 'service'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM runtime_service_matches AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE runtime_generation.status = 'active'

  UNION ALL

  SELECT 'environment'::text AS filter_kind, runtime_fact.scope_id, runtime_fact.payload
  FROM runtime_environment_matches AS runtime_fact
  JOIN ingestion_scopes AS runtime_scope
    ON runtime_scope.scope_id = runtime_fact.scope_id
   AND runtime_scope.active_generation_id = runtime_fact.generation_id
  JOIN scope_generations AS runtime_generation
    ON runtime_generation.scope_id = runtime_fact.scope_id
   AND runtime_generation.generation_id = runtime_fact.generation_id
  WHERE runtime_generation.status = 'active'
),
runtime_filter_repository_candidates AS (
  SELECT runtime_fact.filter_kind,
         runtime_fact.scope_id,
         runtime_repository.repository_id
  FROM runtime_filter_candidates AS runtime_fact
%[6]s
),
runtime_filter_repositories AS MATERIALIZED (
  SELECT DISTINCT filter_kind, repository_id
  FROM runtime_filter_repository_candidates
  WHERE COALESCE(repository_id, '') <> ''
    AND (
          (
            COALESCE(cardinality(%[4]s::text[]), 0) = 0
            AND COALESCE(cardinality(%[5]s::text[]), 0) = 0
          )
          OR runtime_filter_repository_candidates.repository_id = ANY(%[4]s::text[])
          OR runtime_filter_repository_candidates.scope_id = ANY(%[5]s::text[])
        )
)`

// supplyChainImpactRuntimeFilterCTE resolves only the runtime facts selected by
// the requested service, workload, or environment. The materialized
// dimension-first matches make Postgres use the service-catalog, CI/CD, and
// workload-identity indexes before active-generation joins and repository
// decoding. The resulting repository set is current-state query truth; it does
// not mutate or backfill finding payloads.
//
// The arguments are internal SQL parameter expressions such as "$9", never
// request values. Callers retain parameterized queries.
//
// A dimension whose parameter expression is the literal empty string has no
// rows by definition (every leg guards on `<param> <> ”`), so the builder
// replaces that whole dimension CTE with an empty stub selecting identical
// columns. The only build-time-known empty dimension today is the explain
// literal "”" environment (eshu-hq/eshu#11493, arbiter ruling 7178944958):
// bind expressions such as "$10" stay exactly as shipped because their
// emptiness is unknowable at SQL-build time. The stub keeps the dimension's
// runtime_filter_candidates leg, so the $N placeholder set, the arg slices,
// the filter_kind values, and the outer per-dimension AND semantics are
// unchanged. The NOT COALESCE guards, MATERIALIZED, and collation are
// untouched.
func supplyChainImpactRuntimeFilterCTE(
	serviceParam string,
	workloadParam string,
	environmentParam string,
	allowedRepositoriesParam string,
	allowedScopesParam string,
) string {
	full := fmt.Sprintf(
		supplyChainImpactRuntimeFilterCTESQL,
		serviceParam,
		workloadParam,
		environmentParam,
		allowedRepositoriesParam,
		allowedScopesParam,
		supplyChainRuntimeRepositoryDecoderJoin(
			"runtime_fact.payload",
			"runtime_fact.scope_id",
			"runtime_repository",
		),
	)
	return pruneLiteralEmptyRuntimeFilterDimensions(full, serviceParam, workloadParam, environmentParam)
}

// runtimeFilterEmptyLiteral is the only build-time-known empty dimension
// parameter expression. Anything else, including a "$N" bind that may bind
// an empty value per request, keeps the shipped dimension scan.
const runtimeFilterEmptyLiteral = "''"

// runtimeFilterDimensionSuccessor names each prunable whole-dimension CTE
// and the CTE that follows it in template order, so a literal-empty
// dimension is replaced block-by-block without paren counting. The workload
// dimension controls both workload CTEs: pruning one arm alone would drop
// padded-entity matches and break filter truth.
var runtimeFilterDimensionSuccessor = map[string]string{
	"runtime_workload_identity_matches": "runtime_workload_catalog_matches",
	"runtime_workload_catalog_matches":  "runtime_service_matches",
	"runtime_service_matches":           "runtime_environment_matches",
	"runtime_environment_matches":       "runtime_filter_candidates",
}

// pruneLiteralEmptyRuntimeFilterDimensions replaces every whole-dimension
// CTE whose parameter expression is the literal empty string with an empty
// stub. On a lost marker it returns the shipped text unchanged: answers
// never change silently, and the shape test fails loudly on the lost
// pruning.
func pruneLiteralEmptyRuntimeFilterDimensions(full, serviceParam, workloadParam, environmentParam string) string {
	pruned := map[string]bool{
		"runtime_workload_identity_matches": workloadParam == runtimeFilterEmptyLiteral,
		"runtime_workload_catalog_matches":  workloadParam == runtimeFilterEmptyLiteral,
		"runtime_service_matches":           serviceParam == runtimeFilterEmptyLiteral,
		"runtime_environment_matches":       environmentParam == runtimeFilterEmptyLiteral,
	}
	out := full
	for name, next := range runtimeFilterDimensionSuccessor {
		if !pruned[name] {
			continue
		}
		stubbed, ok := stubRuntimeFilterDimensionCTE(out, name, next)
		if !ok {
			return full
		}
		out = stubbed
	}
	return out
}

// stubRuntimeFilterDimensionCTE replaces one whole-dimension CTE block with
// an empty stub selecting identical columns from fact_records, preserving
// the trailing comma that opens the next CTE. The stub references no $N, so
// the statement's placeholder set is unchanged.
func stubRuntimeFilterDimensionCTE(full, name, next string) (string, bool) {
	startMarker := name + " AS MATERIALIZED ("
	nextMarker := next + " AS "
	start := strings.Index(full, startMarker)
	if start < 0 {
		return "", false
	}
	rest := strings.Index(full[start+len(startMarker):], nextMarker)
	if rest < 0 {
		return "", false
	}
	end := start + len(startMarker) + rest
	stub := name + " AS MATERIALIZED (\n" +
		"  SELECT scope_id, generation_id, payload\n" +
		"  FROM fact_records\n" +
		"  WHERE FALSE\n" +
		"),\n"
	return full[:start] + stub + full[end:], true
}

// supplyChainImpactRuntimeFilterPredicate accepts only repositories whose
// current active runtime facts match. Reducer-baked arrays are deliberately
// excluded: they become stale after redeploy, retraction, or promotion and
// would let filter truth disagree with the read-time runtime_context response.
// Each dimension checks its own candidate set so combined filters retain AND
// semantics.
func supplyChainImpactRuntimeFilterPredicate(
	repositoryExpr string,
	serviceParam string,
	workloadParam string,
	environmentParam string,
) string {
	return fmt.Sprintf(`
  AND (
        %[2]s = ''
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'service'
               AND runtime_filter.repository_id = %[1]s
           )
      )
  AND (
        %[3]s = ''
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'workload'
               AND runtime_filter.repository_id = %[1]s
           )
      )
  AND (
        %[4]s = ''
        OR EXISTS (
             SELECT 1
             FROM runtime_filter_repositories AS runtime_filter
             WHERE runtime_filter.filter_kind = 'environment'
               AND runtime_filter.repository_id = %[1]s
           )
	)`,
		repositoryExpr,
		serviceParam,
		workloadParam,
		environmentParam,
	)
}
