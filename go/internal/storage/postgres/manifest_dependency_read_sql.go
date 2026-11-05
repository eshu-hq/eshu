// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// listActivePackageManifestDependencyFactsQuery is the plain manifest
// dependency read; see ListActivePackageManifestDependencyFacts.
//
// It probes each requested (package manager, folded name) pair on its own
// (#9611). PostgreSQL ignores the statistics of an expression that only a
// partial index covers (selfuncs.c uses index expression statistics only when
// indpred is NIL), so a flat "package_manager = ANY($1) AND folded name =
// ANY($2)" read was planned on default selectivities and scanned the whole
// package-manager prefix of fact_records_active_package_dependency_entity_idx
// on every page, filtering it by name. Here the de-duplicated managers and
// names drive a LATERAL probe whose equality on both leading columns of
// migration 247's fact_records_active_package_dependency_folded_entity_idx is
// its Index Cond. Each probe returns its own first $5 rows past the keyset
// cursor in (observed_at, fact_id) order, and the outer sort keeps the first
// $5 of those. Every row matches exactly one pair, so the page equals the
// flat read's page: same rows, same order, same cursor. A page reads at most
// $5 rows per pair, so its cost follows the requested names and their rows,
// not the whole manager's rows. deriveDeltaOverlayBaseQuery swaps the delta
// base joins inside the probe, so the base pages take the same shape.
//
// Performance Evidence: docs/internal/evidence/9611-manifest-read-name-probe.md.
// No-Observability-Change: the read_seconds and read_pages histograms keep
// their plain and maven_family labels, because the constants keep their names.
const listActivePackageManifestDependencyFactsQuery = `
SELECT
    page.fact_id,
    page.scope_id,
    page.generation_id,
    page.fact_kind,
    page.stable_fact_key,
    page.schema_version,
    page.collector_kind,
    page.fencing_token,
    page.source_confidence,
    page.source_system,
    page.source_fact_key,
    page.source_uri,
    page.source_record_id,
    page.observed_at,
    page.is_tombstone,
    page.payload
FROM (SELECT DISTINCT manager FROM unnest($1::text[]) AS manager) AS requested_manager
CROSS JOIN (SELECT DISTINCT name FROM unnest($2::text[]) AS name) AS requested_name
CROSS JOIN LATERAL (
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
      AND fact.payload->'entity_metadata'->>'package_manager' = requested_manager.manager
      AND (LOWER(TRIM(fact.payload->>'entity_name') COLLATE "C") COLLATE "default") = requested_name.name
      AND generation.status = 'active'
      AND fact.is_tombstone = FALSE
      AND (
        $3::timestamptz IS NULL
        OR (fact.observed_at, fact.fact_id) > ($3::timestamptz, $4::text)
      )
    ORDER BY fact.observed_at ASC, fact.fact_id ASC
    LIMIT $5
) AS page
ORDER BY page.observed_at ASC, page.fact_id ASC
LIMIT $5
`

// listActivePackageManifestDependencyFactsWithSBTQuery is the Maven-family
// read (#8961). Its first branch is the plain per-(package manager, name)
// probe (#9611) over the Maven family's managers, so Gradle and sbt `%` rows
// (whose name is the exact coordinate) join through entity_name. Its second branch reaches sbt
// `%%` rows, whose entity_name is the unsuffixed organization:name and whose
// published artifact rides in sbt_binary_artifact. The branches are disjoint
// (the second excludes entity_name values the first already matches), each is
// keyset-bounded and limited on its own, and the outer sort restores the one
// ordering and page size the cursor depends on.
//
// Both branches over-select by design: the reducer's
// packageidentity.ManifestConsumptionKeys is the accuracy gate, so a row this
// query admits but the helper rejects (a `%%%` row, say) joins nothing. Both
// branches fold entity_name with LOWER(TRIM(entity_name) COLLATE "C") (#9568):
// branch 1 so mixed-case manifest rows load, branch 2's exclusion so it stays
// disjoint from branch 1 (an exact name in $2 is also a folded name in $2).
// The sbt_binary_artifact comparison stays exact; artifacts ride the parser's
// case the same way the Maven coordinate does (#9286).
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
// rows, not in the corpus. Those figures predate #9611, which replaced the
// first branch's flat scan with the per-name probe; the #9611 evidence note
// measures the whole Maven-family page after it.
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
      page.fact_id,
      page.scope_id,
      page.generation_id,
      page.fact_kind,
      page.stable_fact_key,
      page.schema_version,
      page.collector_kind,
      page.fencing_token,
      page.source_confidence,
      page.source_system,
      page.source_fact_key,
      page.source_uri,
      page.source_record_id,
      page.observed_at,
      page.is_tombstone,
      page.payload
  FROM (SELECT DISTINCT manager FROM unnest($1::text[]) AS manager) AS requested_manager
  CROSS JOIN (SELECT DISTINCT name FROM unnest($2::text[]) AS name) AS requested_name
  CROSS JOIN LATERAL (
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
        AND fact.payload->'entity_metadata'->>'package_manager' = requested_manager.manager
        AND (LOWER(TRIM(fact.payload->>'entity_name') COLLATE "C") COLLATE "default") = requested_name.name
        AND generation.status = 'active'
        AND fact.is_tombstone = FALSE
        AND (
          $3::timestamptz IS NULL
          OR (fact.observed_at, fact.fact_id) > ($3::timestamptz, $4::text)
        )
      ORDER BY fact.observed_at ASC, fact.fact_id ASC
      LIMIT $5
  ) AS page
  ORDER BY page.observed_at ASC, page.fact_id ASC
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
    AND (LOWER(TRIM(fact.payload->>'entity_name') COLLATE "C") COLLATE "default") <> ALL($2::text[])
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
