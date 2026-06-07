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
WITH lookup_ids AS MATERIALIZED (
    SELECT DISTINCT TRIM(value) AS value
    FROM unnest($2::text[]) AS input(value)
    WHERE NULLIF(TRIM(value), '') IS NOT NULL
),
scope_active_generations AS MATERIALIZED (
    SELECT scope.scope_id, scope.active_generation_id
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS generation
      ON generation.scope_id = scope.scope_id
     AND generation.generation_id = scope.active_generation_id
    WHERE generation.status = 'active'
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
        FROM scope_active_generations AS scope
        JOIN fact_records AS fact
          ON fact.scope_id = scope.scope_id
         AND scope.active_generation_id = fact.generation_id
        WHERE $3 <> ''
          AND (fact.payload->>'package_id' = $3 OR fact.payload->>'purl' = $3)
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
        UNION ALL
        SELECT impact.fact_id, impact.scope_id, impact.generation_id, impact.fact_kind, impact.source_confidence, impact.observed_at, impact.payload
        FROM scope_active_generations AS scope
        JOIN fact_records AS impact
          ON impact.scope_id = scope.scope_id
         AND scope.active_generation_id = impact.generation_id
        WHERE ($6 <> '' OR $7 <> '' OR $8 <> '')
          AND impact.fact_kind = 'reducer_supply_chain_impact_finding'
          AND impact.is_tombstone = FALSE
          AND ($6 = '' OR impact.payload->>'repository_id' = $6)
          AND ($7 = '' OR impact.payload->'workload_ids' ? $7)
          AND ($8 = '' OR impact.payload->'service_ids' ? $8)
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
        FROM scope_active_generations AS scope
        JOIN fact_records AS fact
          ON fact.scope_id = scope.scope_id
         AND scope.active_generation_id = fact.generation_id
        WHERE $3 <> ''
          AND (fact.payload->>'package_id' = $3 OR fact.payload->>'purl' = $3)
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
