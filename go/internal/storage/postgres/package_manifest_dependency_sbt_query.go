// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

// listActivePackageManifestDependencyFactsWithSBTQuery is the Maven-family
// read (#8961). Its first branch is the existing query with package_manager
// widened to the Maven family, so Gradle and sbt `%` rows (whose name is the
// exact coordinate) join through entity_name. Its second branch reaches sbt
// `%%` rows, whose entity_name is the unsuffixed organization:name and whose
// published artifact rides in sbt_binary_artifact. The branches are disjoint
// (the second excludes entity_name values the first already matches), each is
// keyset-bounded and limited on its own, and the outer sort restores the one
// ordering and page size the cursor depends on.
//
// Both branches over-select by design: the reducer's
// packageidentity.ManifestConsumptionKeys is the accuracy gate, so a row this
// query admits but the helper rejects (a `%%%` row, say) joins nothing.
//
// Performance Evidence (throwaway Postgres 18, 1.3M git dependency rows plus
// 0.4M other content_entity rows, 13,000 sbt rows, 4,000 active scopes, 400
// names in $2, LIMIT 5000, cursor NULL then set; psql EXPLAIN (ANALYZE,
// BUFFERS) before and after): the sbt branch scans only the package_manager
// = 'sbt' prefix of fact_records_active_package_dependency_entity_idx (13,000
// entries, about 14,000 buffers, no sequential scan, no new index). With the
// planner left alone both shapes took the same scope-driven nested loop the
// current query already takes on this synthetic data: 3,644 ms before and
// 6,599 ms after, because widening package_manager from one value to three
// roughly doubles the index searches (1.47M to 2.90M) and reaches 1,949 rows
// instead of 1,117. With nested loops disabled the same query is 5.1 ms
// (about 2,300 buffers) before and
// 29.2 ms (about 17,400 buffers) after; with the cursor set 8.9 ms after.
// The cost the sbt branch adds is linear in the number of sbt dependency
// rows, not in the corpus.
//
// No-Observability-Change: same FactStore read path, instrumentation, and
// row shape as listActivePackageManifestDependencyFactsQuery.
const listActivePackageManifestDependencyFactsWithSBTQuery = `
SELECT
    u.fact_id, u.scope_id, u.generation_id, u.fact_kind, u.stable_fact_key,
    u.schema_version, u.collector_kind, u.fencing_token, u.source_confidence,
    u.source_system, u.source_fact_key, u.source_uri, u.source_record_id,
    u.observed_at, u.is_tombstone, u.payload
FROM (
  (SELECT
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
      COALESCE(fact.source_uri, '') AS source_uri,
      COALESCE(fact.source_record_id, '') AS source_record_id,
      fact.observed_at,
      fact.is_tombstone,
      fact.payload
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
  WHERE fact.fact_kind = 'content_entity'
    AND fact.source_system = 'git'
    AND fact.payload->>'entity_type' = 'Variable'
    AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
    AND fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])
    AND fact.payload->>'entity_name' = ANY($2::text[])
    AND generation.status = 'active'
    AND fact.is_tombstone = FALSE
    AND (
      $3::timestamptz IS NULL
      OR (fact.observed_at, fact.fact_id) > ($3::timestamptz, $4::text)
    )
  ORDER BY fact.observed_at ASC, fact.fact_id ASC
  LIMIT $5)
  UNION ALL
  (SELECT
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
      COALESCE(fact.source_uri, '') AS source_uri,
      COALESCE(fact.source_record_id, '') AS source_record_id,
      fact.observed_at,
      fact.is_tombstone,
      fact.payload
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
  WHERE fact.fact_kind = 'content_entity'
    AND fact.source_system = 'git'
    AND fact.payload->>'entity_type' = 'Variable'
    AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
    AND fact.payload->'entity_metadata'->>'package_manager' = 'sbt'
    AND fact.payload->'entity_metadata'->>'sbt_binary_artifact' = ANY($2::text[])
    AND fact.payload->>'entity_name' <> ALL($2::text[])
    AND generation.status = 'active'
    AND fact.is_tombstone = FALSE
    AND (
      $3::timestamptz IS NULL
      OR (fact.observed_at, fact.fact_id) > ($3::timestamptz, $4::text)
    )
  ORDER BY fact.observed_at ASC, fact.fact_id ASC
  LIMIT $5)
) AS u
ORDER BY u.observed_at ASC, u.fact_id ASC
LIMIT $5
`

// activePackageManifestDependencyQuery returns the package_manager values and
// the query one ListActivePackageManifestDependencyFacts call uses. When any
// ecosystem is Maven, the list expands to the Maven family and the sbt-aware
// query runs; otherwise the list and the original query pass through
// unchanged.
func activePackageManifestDependencyQuery(ecosystems []string) ([]string, string) {
	hasMaven := slices.ContainsFunc(ecosystems, func(ecosystem string) bool {
		return packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(ecosystem)) == packageidentity.EcosystemMaven
	})
	if !hasMaven {
		return ecosystems, listActivePackageManifestDependencyFactsQuery
	}
	expanded := make([]string, 0, len(ecosystems)+2)
	for _, ecosystem := range ecosystems {
		expanded = append(expanded, packageidentity.ManifestPackageManagersForEcosystem(ecosystem)...)
	}
	slices.Sort(expanded)
	return slices.Compact(expanded), listActivePackageManifestDependencyFactsWithSBTQuery
}
