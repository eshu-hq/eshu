// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

var advisoryEvidenceFactKinds = []string{
	"vulnerability.cve",
	"vulnerability.affected_package",
	"vulnerability.affected_product",
	"vulnerability.epss_score",
	"vulnerability.known_exploited",
	"vulnerability.reference",
}

const listAdvisoryEvidenceQuery = `
WITH explicit_lookup_ids AS MATERIALIZED (
    SELECT DISTINCT TRIM(value) AS value
    FROM unnest($2::text[]) AS input(value)
    WHERE NULLIF(TRIM(value), '') IS NOT NULL
),
explicit_package_ids AS MATERIALIZED (
    SELECT DISTINCT TRIM(value) AS value
    FROM unnest($3::text[]) AS input(value)
    WHERE NULLIF(TRIM(value), '') IS NOT NULL
),
impact_selector AS MATERIALIZED (
    SELECT
        COALESCE(NULLIF(TRIM($6::text), ''), '') AS repository_id,
        COALESCE(NULLIF(TRIM($7::text), ''), '') AS service_id,
        COALESCE(NULLIF(TRIM($8::text), ''), '') AS workload_id,
        NULLIF(TRIM($6::text), '') IS NOT NULL
        OR NULLIF(TRIM($7::text), '') IS NOT NULL
        OR NULLIF(TRIM($8::text), '') IS NOT NULL AS has_scope
),
scope_active_generations AS MATERIALIZED (
    SELECT scope.scope_id, scope.active_generation_id
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS generation
      ON generation.scope_id = scope.scope_id
     AND generation.generation_id = scope.active_generation_id
    WHERE generation.status = 'active'
),
impact_candidates AS MATERIALIZED (
    SELECT DISTINCT ON (fact.fact_id)
        fact.fact_id,
        fact.payload
    FROM impact_selector AS selector
    JOIN scope_active_generations AS scope
      ON selector.has_scope
    JOIN fact_records AS fact
      ON fact.scope_id = scope.scope_id
     AND scope.active_generation_id = fact.generation_id
    WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
      AND fact.is_tombstone = FALSE
      AND (selector.repository_id = '' OR fact.payload->>'repository_id' = selector.repository_id)
      AND (selector.service_id = '' OR fact.payload->'service_ids' ? selector.service_id)
      AND (selector.workload_id = '' OR fact.payload->'workload_ids' ? selector.workload_id)
      AND (
          COALESCE(cardinality($9::text[]), 0) = 0
          OR fact.payload->>'repository_id' = ANY($9::text[])
          OR fact.scope_id = ANY($9::text[])
      )
      AND (
          NOT EXISTS (SELECT 1 FROM explicit_lookup_ids)
          OR EXISTS (
              SELECT 1
              FROM explicit_lookup_ids AS lookup
              WHERE fact.payload->>'cve_id' = lookup.value
                 OR fact.payload->>'advisory_id' = lookup.value
          )
      )
      AND (
          NOT EXISTS (SELECT 1 FROM explicit_package_ids)
          OR EXISTS (
              SELECT 1
              FROM explicit_package_ids AS pkg
              WHERE fact.payload->>'package_id' = pkg.value
                 OR fact.payload->>'purl' = pkg.value
          )
      )
    ORDER BY fact.fact_id
    LIMIT $5
),
lookup_ids AS MATERIALIZED (
    SELECT DISTINCT value
    FROM (
        SELECT lookup.value
        FROM explicit_lookup_ids AS lookup
        WHERE NOT (SELECT selector.has_scope FROM impact_selector AS selector)
        UNION ALL
        SELECT payload->>'cve_id' AS value
        FROM impact_candidates
        UNION ALL
        SELECT payload->>'advisory_id' AS value
        FROM impact_candidates
    ) AS raw_lookup(value)
    WHERE NULLIF(TRIM(value), '') IS NOT NULL
),
package_ids AS MATERIALIZED (
    SELECT DISTINCT value
    FROM (
        SELECT pkg.value
        FROM explicit_package_ids AS pkg
        WHERE NOT (SELECT selector.has_scope FROM impact_selector AS selector)
        UNION ALL
        SELECT payload->>'package_id' AS value
        FROM impact_candidates
        WHERE NULLIF(payload->>'cve_id', '') IS NULL
          AND NULLIF(payload->>'advisory_id', '') IS NULL
        UNION ALL
        SELECT payload->>'purl' AS value
        FROM impact_candidates
        WHERE NULLIF(payload->>'cve_id', '') IS NULL
          AND NULLIF(payload->>'advisory_id', '') IS NULL
    ) AS raw_package(value)
    WHERE NULLIF(TRIM(value), '') IS NOT NULL
),
seed_candidates AS MATERIALIZED (
    SELECT DISTINCT ON (candidate.fact_id)
        candidate.fact_id,
        candidate.scope_id,
        candidate.generation_id,
        candidate.fact_kind,
        candidate.source_confidence,
        candidate.observed_at,
        candidate.payload
    FROM (
        SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload
        FROM lookup_ids AS lookup
        JOIN scope_active_generations AS scope ON TRUE
        JOIN fact_records AS fact
          ON fact.scope_id = scope.scope_id
         AND scope.active_generation_id = fact.generation_id
         AND fact.payload->>'cve_id' = lookup.value
        WHERE fact.fact_kind IN (
            'vulnerability.cve',
            'vulnerability.affected_package',
            'vulnerability.affected_product',
            'vulnerability.epss_score',
            'vulnerability.known_exploited',
            'vulnerability.reference'
        )
          AND fact.fact_kind = ANY($1::text[])
          AND fact.is_tombstone = FALSE
        UNION ALL
        SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload
        FROM lookup_ids AS lookup
        JOIN scope_active_generations AS scope ON TRUE
        JOIN fact_records AS fact
          ON fact.scope_id = scope.scope_id
         AND scope.active_generation_id = fact.generation_id
         AND fact.payload->>'advisory_id' = lookup.value
        WHERE fact.fact_kind IN (
            'vulnerability.cve',
            'vulnerability.affected_package',
            'vulnerability.affected_product',
            'vulnerability.epss_score',
            'vulnerability.known_exploited',
            'vulnerability.reference'
        )
          AND fact.fact_kind = ANY($1::text[])
          AND fact.is_tombstone = FALSE
        UNION ALL
        SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload
        FROM lookup_ids AS lookup
        JOIN scope_active_generations AS scope ON TRUE
        JOIN fact_records AS fact
          ON fact.scope_id = scope.scope_id
         AND scope.active_generation_id = fact.generation_id
         AND fact.payload->>'ghsa_id' = lookup.value
        WHERE fact.fact_kind IN (
            'vulnerability.cve',
            'vulnerability.affected_package',
            'vulnerability.affected_product',
            'vulnerability.epss_score',
            'vulnerability.known_exploited',
            'vulnerability.reference'
        )
          AND fact.fact_kind = ANY($1::text[])
          AND fact.is_tombstone = FALSE
        UNION ALL
        SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload
        FROM package_ids AS pkg
        JOIN scope_active_generations AS scope ON TRUE
        JOIN fact_records AS fact
          ON fact.scope_id = scope.scope_id
         AND scope.active_generation_id = fact.generation_id
        WHERE (fact.payload->>'package_id' = pkg.value OR fact.payload->>'purl' = pkg.value)
          AND fact.fact_kind IN (
              'vulnerability.cve',
              'vulnerability.affected_package',
              'vulnerability.affected_product',
              'vulnerability.epss_score',
              'vulnerability.known_exploited',
              'vulnerability.reference'
          )
          AND fact.fact_kind = ANY($1::text[])
          AND fact.is_tombstone = FALSE
    ) AS candidate
    ORDER BY candidate.fact_id
),
seed AS (
    SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload
    FROM seed_candidates AS fact
    ORDER BY COALESCE(
        NULLIF(fact.payload->>'cve_id', ''),
        NULLIF(fact.payload->>'advisory_id', ''),
        NULLIF(fact.payload->>'ghsa_id', ''),
        fact.fact_id
    ), fact.fact_kind, fact.fact_id
    LIMIT $5
),
seed_keys AS MATERIALIZED (
    SELECT ARRAY(
        SELECT DISTINCT key_value
        FROM (
            SELECT payload->>'cve_id' AS key_value, 'identity' AS key_source FROM seed
            UNION ALL SELECT payload->>'advisory_id', 'identity' FROM seed
            UNION ALL SELECT payload->>'ghsa_id', 'identity' FROM seed
            UNION ALL SELECT jsonb_array_elements_text(
                CASE WHEN jsonb_typeof(payload->'aliases') = 'array'
                     THEN payload->'aliases' ELSE '[]'::jsonb END
            ), 'alias' FROM seed
            UNION ALL SELECT jsonb_array_elements_text(
                CASE WHEN jsonb_typeof(payload->'correlation_anchors') = 'array'
                     THEN payload->'correlation_anchors' ELSE '[]'::jsonb END
            ), 'correlation_anchor' FROM seed
        ) AS raw_keys
        WHERE NULLIF(TRIM(key_value), '') IS NOT NULL
          AND (
              key_source = 'identity'
              OR (
                  (key_source = 'alias' OR key_source = 'correlation_anchor')
                  AND (
                      UPPER(TRIM(key_value)) LIKE 'CVE-%'
                      OR UPPER(TRIM(key_value)) LIKE 'GHSA-%'
                      OR UPPER(TRIM(key_value)) LIKE 'OSV-%'
                  )
              )
          )
    ) AS values
),
matched_candidates AS MATERIALIZED (
    SELECT DISTINCT ON (candidate.fact_id)
        candidate.fact_id,
        candidate.scope_id,
        candidate.generation_id,
        candidate.fact_kind,
        candidate.source_confidence,
        candidate.observed_at,
        candidate.payload
    FROM (
        SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload
        FROM seed_keys AS keys
        JOIN LATERAL unnest(keys.values) AS lookup(value) ON TRUE
        JOIN scope_active_generations AS scope ON TRUE
        JOIN fact_records AS fact
          ON fact.scope_id = scope.scope_id
         AND scope.active_generation_id = fact.generation_id
         AND fact.payload->>'cve_id' = lookup.value
        WHERE cardinality(keys.values) > 0
          AND fact.fact_kind IN (
              'vulnerability.cve',
              'vulnerability.affected_package',
              'vulnerability.affected_product',
              'vulnerability.epss_score',
              'vulnerability.known_exploited',
              'vulnerability.reference'
          )
          AND fact.fact_kind = ANY($1::text[])
          AND fact.is_tombstone = FALSE
          AND ($4 = '' OR LOWER(fact.payload->>'source') = $4)
        UNION ALL
        SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload
        FROM seed_keys AS keys
        JOIN LATERAL unnest(keys.values) AS lookup(value) ON TRUE
        JOIN scope_active_generations AS scope ON TRUE
        JOIN fact_records AS fact
          ON fact.scope_id = scope.scope_id
         AND scope.active_generation_id = fact.generation_id
         AND fact.payload->>'advisory_id' = lookup.value
        WHERE cardinality(keys.values) > 0
          AND fact.fact_kind IN (
              'vulnerability.cve',
              'vulnerability.affected_package',
              'vulnerability.affected_product',
              'vulnerability.epss_score',
              'vulnerability.known_exploited',
              'vulnerability.reference'
          )
          AND fact.fact_kind = ANY($1::text[])
          AND fact.is_tombstone = FALSE
          AND ($4 = '' OR LOWER(fact.payload->>'source') = $4)
        UNION ALL
        SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload
        FROM seed_keys AS keys
        JOIN LATERAL unnest(keys.values) AS lookup(value) ON TRUE
        JOIN scope_active_generations AS scope ON TRUE
        JOIN fact_records AS fact
          ON fact.scope_id = scope.scope_id
         AND scope.active_generation_id = fact.generation_id
         AND fact.payload->>'ghsa_id' = lookup.value
        WHERE cardinality(keys.values) > 0
          AND fact.fact_kind IN (
              'vulnerability.cve',
              'vulnerability.affected_package',
              'vulnerability.affected_product',
              'vulnerability.epss_score',
              'vulnerability.known_exploited',
              'vulnerability.reference'
          )
          AND fact.fact_kind = ANY($1::text[])
          AND fact.is_tombstone = FALSE
          AND ($4 = '' OR LOWER(fact.payload->>'source') = $4)
        UNION ALL
        SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload
        FROM package_ids AS pkg
        JOIN scope_active_generations AS scope ON TRUE
        JOIN fact_records AS fact
          ON fact.scope_id = scope.scope_id
         AND scope.active_generation_id = fact.generation_id
        WHERE (fact.payload->>'package_id' = pkg.value OR fact.payload->>'purl' = pkg.value)
          AND fact.fact_kind IN (
              'vulnerability.cve',
              'vulnerability.affected_package',
              'vulnerability.affected_product',
              'vulnerability.epss_score',
              'vulnerability.known_exploited',
              'vulnerability.reference'
          )
          AND fact.fact_kind = ANY($1::text[])
          AND fact.is_tombstone = FALSE
          AND ($4 = '' OR LOWER(fact.payload->>'source') = $4)
    ) AS candidate
    ORDER BY candidate.fact_id
),
matched_facts AS (
    SELECT fact.fact_id, fact.fact_kind, fact.source_confidence, fact.observed_at, fact.payload
    FROM matched_candidates AS fact
    ORDER BY COALESCE(
        NULLIF(fact.payload->>'cve_id', ''),
        NULLIF(fact.payload->>'advisory_id', ''),
        NULLIF(fact.payload->>'ghsa_id', ''),
        fact.fact_id
    ), fact.fact_kind, fact.fact_id
    LIMIT $5
)
SELECT fact_id, fact_kind, source_confidence, observed_at, payload
FROM matched_facts
`
