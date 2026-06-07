package query

// listAdvisoryCatalogQuery lists a bounded, browsable page of canonical
// vulnerability advisories from active vulnerability source facts.
//
// Parameters:
//
//	$1 severity label filter, '' for any (compared case-insensitively)
//	$2 ecosystem filter, '' for any (compared case-insensitively)
//	$3 query prefix filter, '' for any (matched against canonical id, cve id,
//	   ghsa id, package id, and purl, case-insensitively)
//	$4 kev_only flag; when true only advisories present in CISA KEV are returned
//	$5 cursor cvss score (descending keyset anchor); ignored on the first page
//	$6 cursor advisory key (ascending keyset tie-break); '' for the first page
//	$7 page limit (page size plus one for truncation detection)
//
// The advisory spine is the active vulnerability.cve facts. The query keeps the
// per-advisory aggregation bounded by grouping on the canonical key and reads
// only the vulnerability.cve, vulnerability.affected_package, and
// vulnerability.known_exploited fact kinds, each covered by a partial active
// read index. Ordering is deterministic: descending cvss_score, then ascending
// advisory key. Pagination is keyset over that ordering.
const listAdvisoryCatalogQuery = `
WITH scope_active_generations AS MATERIALIZED (
    SELECT scope.scope_id, scope.active_generation_id
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS generation
      ON generation.scope_id = scope.scope_id
     AND generation.generation_id = scope.active_generation_id
    WHERE generation.status = 'active'
),
cve_facts AS MATERIALIZED (
    SELECT
        UPPER(TRIM(COALESCE(
            NULLIF(TRIM(fact.payload->>'cve_id'), ''),
            NULLIF(TRIM(fact.payload->>'advisory_id'), ''),
            NULLIF(TRIM(fact.payload->>'ghsa_id'), '')
        ))) AS advisory_key,
        NULLIF(TRIM(fact.payload->>'cve_id'), '') AS cve_id,
        NULLIF(TRIM(fact.payload->>'ghsa_id'), '') AS ghsa_id,
        NULLIF(TRIM(fact.payload->>'source'), '') AS source,
        NULLIF(TRIM(fact.payload->>'severity_label'), '') AS severity_label,
        NULLIF(TRIM(fact.payload->>'published_at'), '') AS published_at,
        CASE
            WHEN (fact.payload->>'cvss_score') ~ '^[0-9]+(\.[0-9]+)?$'
            THEN (fact.payload->>'cvss_score')::numeric
            ELSE 0
        END AS cvss_score
    FROM scope_active_generations AS scope
    JOIN fact_records AS fact
      ON fact.scope_id = scope.scope_id
     AND scope.active_generation_id = fact.generation_id
    WHERE fact.fact_kind = 'vulnerability.cve'
      AND fact.is_tombstone = FALSE
      AND COALESCE(
            NULLIF(TRIM(fact.payload->>'cve_id'), ''),
            NULLIF(TRIM(fact.payload->>'advisory_id'), ''),
            NULLIF(TRIM(fact.payload->>'ghsa_id'), '')
          ) IS NOT NULL
),
catalog AS MATERIALIZED (
    SELECT
        advisory_key,
        MAX(cvss_score) AS cvss_score,
        (ARRAY_AGG(severity_label ORDER BY cvss_score DESC NULLS LAST)
            FILTER (WHERE severity_label IS NOT NULL))[1] AS severity_label,
        (ARRAY_AGG(cve_id ORDER BY cve_id)
            FILTER (WHERE cve_id IS NOT NULL))[1] AS cve_id,
        (ARRAY_AGG(ghsa_id ORDER BY ghsa_id)
            FILTER (WHERE ghsa_id IS NOT NULL))[1] AS ghsa_id,
        (ARRAY_AGG(published_at ORDER BY published_at)
            FILTER (WHERE published_at IS NOT NULL))[1] AS published_at,
        ARRAY(
            SELECT DISTINCT s
            FROM unnest(ARRAY_AGG(source) FILTER (WHERE source IS NOT NULL)) AS s
            ORDER BY s
        ) AS sources
    FROM cve_facts
    GROUP BY advisory_key
),
affected AS MATERIALIZED (
    SELECT
        UPPER(TRIM(COALESCE(
            NULLIF(TRIM(fact.payload->>'cve_id'), ''),
            NULLIF(TRIM(fact.payload->>'advisory_id'), ''),
            NULLIF(TRIM(fact.payload->>'ghsa_id'), '')
        ))) AS advisory_key,
        NULLIF(LOWER(TRIM(fact.payload->>'ecosystem')), '') AS ecosystem,
        NULLIF(TRIM(fact.payload->>'package_id'), '') AS package_id,
        NULLIF(TRIM(fact.payload->>'purl'), '') AS purl
    FROM scope_active_generations AS scope
    JOIN fact_records AS fact
      ON fact.scope_id = scope.scope_id
     AND scope.active_generation_id = fact.generation_id
    WHERE fact.fact_kind = 'vulnerability.affected_package'
      AND fact.is_tombstone = FALSE
),
affected_rollup AS MATERIALIZED (
    SELECT
        advisory_key,
        ARRAY(SELECT DISTINCT e FROM unnest(ARRAY_AGG(ecosystem)) AS e WHERE e IS NOT NULL ORDER BY e) AS ecosystems,
        ARRAY(SELECT DISTINCT p FROM unnest(ARRAY_AGG(package_id)) AS p WHERE p IS NOT NULL ORDER BY p) AS package_ids,
        ARRAY(SELECT DISTINCT u FROM unnest(ARRAY_AGG(purl)) AS u WHERE u IS NOT NULL ORDER BY u) AS purls
    FROM affected
    GROUP BY advisory_key
),
kev AS MATERIALIZED (
    SELECT DISTINCT UPPER(TRIM(NULLIF(TRIM(fact.payload->>'cve_id'), ''))) AS advisory_key
    FROM scope_active_generations AS scope
    JOIN fact_records AS fact
      ON fact.scope_id = scope.scope_id
     AND scope.active_generation_id = fact.generation_id
    WHERE fact.fact_kind = 'vulnerability.known_exploited'
      AND fact.is_tombstone = FALSE
      AND NULLIF(TRIM(fact.payload->>'cve_id'), '') IS NOT NULL
),
joined AS (
    SELECT
        catalog.advisory_key,
        catalog.cvss_score,
        catalog.severity_label,
        catalog.cve_id,
        catalog.ghsa_id,
        catalog.published_at,
        catalog.sources,
        COALESCE(affected_rollup.ecosystems, ARRAY[]::text[]) AS ecosystems,
        COALESCE(affected_rollup.package_ids, ARRAY[]::text[]) AS package_ids,
        COALESCE(affected_rollup.purls, ARRAY[]::text[]) AS purls,
        (kev.advisory_key IS NOT NULL) AS kev
    FROM catalog
    LEFT JOIN affected_rollup ON affected_rollup.advisory_key = catalog.advisory_key
    LEFT JOIN kev ON kev.advisory_key = catalog.advisory_key
)
SELECT
    advisory_key,
    cvss_score,
    severity_label,
    cve_id,
    ghsa_id,
    published_at,
    sources,
    ecosystems,
    package_ids,
    kev
FROM joined
WHERE ($1 = '' OR LOWER(COALESCE(severity_label, '')) = LOWER($1))
  AND ($4 = FALSE OR kev = TRUE)
  AND (
        $2 = ''
        OR EXISTS (
            SELECT 1 FROM unnest(ecosystems) AS e WHERE e = LOWER($2)
        )
      )
  AND (
        $3 = ''
        OR advisory_key LIKE UPPER($3) || '%'
        OR UPPER(COALESCE(cve_id, '')) LIKE UPPER($3) || '%'
        OR UPPER(COALESCE(ghsa_id, '')) LIKE UPPER($3) || '%'
        OR EXISTS (
            SELECT 1 FROM unnest(package_ids) AS p WHERE LOWER(p) LIKE LOWER($3) || '%'
        )
        OR EXISTS (
            SELECT 1 FROM unnest(purls) AS u WHERE LOWER(u) LIKE LOWER($3) || '%'
        )
      )
  AND (
        $6 = ''
        OR cvss_score < $5
        OR (cvss_score = $5 AND advisory_key > $6)
      )
ORDER BY cvss_score DESC, advisory_key ASC
LIMIT $7
`
