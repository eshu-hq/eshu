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
// The advisory spine is the active vulnerability.cve facts. vuln_facts reads the
// vulnerability.cve, vulnerability.affected_package, and
// vulnerability.known_exploited fact kinds as three per-kind UNION ALL legs,
// then a single GROUP BY advisory_key rolls them up with per-kind FILTERed
// aggregates. HAVING bool_or(fact_kind = 'vulnerability.cve') keeps the spine
// identity: an advisory is emitted only when it has a cve fact, matching the
// previous catalog/affected_rollup LEFT JOIN. Ordering is deterministic:
// descending cvss_score, then ascending advisory key. Pagination is keyset over
// that ordering.
//
// #3389: the original shape built three MATERIALIZED CTEs and LEFT JOINed two
// whole-fact-kind aggregates (catalog, affected_rollup) on a computed
// advisory_key. Postgres estimates that grouped, expression-keyed input at one
// row, so the rollup joins collapsed into an O(active_facts^2) nested-loop left
// join that did not finish within a 600s statement timeout at ~250k
// vulnerability facts. #3402 added per-fact_kind active_scan partial indexes
// that bound the per-kind scans, but an index cannot bound a join between two
// grouped aggregates, so the catalog still timed out once the advisory count
// grew. This shape removes the join: each per-kind leg keeps its single
// fact_kind + is_tombstone predicate (so the #3402 active_scan partial indexes
// stay eligible) and the legs feed one GROUP BY, leaving a single
// O(active vulnerability facts) aggregate pass with no nested-loop blowup.
// Output is byte-identical to the previous shape.
const listAdvisoryCatalogQuery = `
WITH vuln_facts AS (
    SELECT
        UPPER(TRIM(COALESCE(
            NULLIF(TRIM(fact.payload->>'cve_id'), ''),
            NULLIF(TRIM(fact.payload->>'advisory_id'), ''),
            NULLIF(TRIM(fact.payload->>'ghsa_id'), '')
        ))) AS advisory_key,
        fact.fact_kind AS fact_kind,
        NULLIF(TRIM(fact.payload->>'cve_id'), '') AS cve_id,
        NULLIF(TRIM(fact.payload->>'ghsa_id'), '') AS ghsa_id,
        NULLIF(TRIM(fact.payload->>'source'), '') AS source,
        NULLIF(TRIM(fact.payload->>'severity_label'), '') AS severity_label,
        NULLIF(TRIM(fact.payload->>'published_at'), '') AS published_at,
        CASE
            WHEN (fact.payload->>'cvss_score') ~ '^[0-9]+(\.[0-9]+)?$'
            THEN (fact.payload->>'cvss_score')::numeric
            ELSE 0
        END AS cvss_score,
        NULLIF(LOWER(TRIM(fact.payload->>'ecosystem')), '') AS ecosystem,
        NULLIF(TRIM(fact.payload->>'package_id'), '') AS package_id,
        NULLIF(TRIM(fact.payload->>'purl'), '') AS purl
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON fact.scope_id = scope.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'vulnerability.cve'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
    UNION ALL
    SELECT
        UPPER(TRIM(COALESCE(
            NULLIF(TRIM(fact.payload->>'cve_id'), ''),
            NULLIF(TRIM(fact.payload->>'advisory_id'), ''),
            NULLIF(TRIM(fact.payload->>'ghsa_id'), '')
        ))) AS advisory_key,
        fact.fact_kind AS fact_kind,
        NULLIF(TRIM(fact.payload->>'cve_id'), '') AS cve_id,
        NULLIF(TRIM(fact.payload->>'ghsa_id'), '') AS ghsa_id,
        NULLIF(TRIM(fact.payload->>'source'), '') AS source,
        NULLIF(TRIM(fact.payload->>'severity_label'), '') AS severity_label,
        NULLIF(TRIM(fact.payload->>'published_at'), '') AS published_at,
        CASE
            WHEN (fact.payload->>'cvss_score') ~ '^[0-9]+(\.[0-9]+)?$'
            THEN (fact.payload->>'cvss_score')::numeric
            ELSE 0
        END AS cvss_score,
        NULLIF(LOWER(TRIM(fact.payload->>'ecosystem')), '') AS ecosystem,
        NULLIF(TRIM(fact.payload->>'package_id'), '') AS package_id,
        NULLIF(TRIM(fact.payload->>'purl'), '') AS purl
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON fact.scope_id = scope.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'vulnerability.affected_package'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
    UNION ALL
    SELECT
        UPPER(TRIM(COALESCE(
            NULLIF(TRIM(fact.payload->>'cve_id'), ''),
            NULLIF(TRIM(fact.payload->>'advisory_id'), ''),
            NULLIF(TRIM(fact.payload->>'ghsa_id'), '')
        ))) AS advisory_key,
        fact.fact_kind AS fact_kind,
        NULLIF(TRIM(fact.payload->>'cve_id'), '') AS cve_id,
        NULLIF(TRIM(fact.payload->>'ghsa_id'), '') AS ghsa_id,
        NULLIF(TRIM(fact.payload->>'source'), '') AS source,
        NULLIF(TRIM(fact.payload->>'severity_label'), '') AS severity_label,
        NULLIF(TRIM(fact.payload->>'published_at'), '') AS published_at,
        CASE
            WHEN (fact.payload->>'cvss_score') ~ '^[0-9]+(\.[0-9]+)?$'
            THEN (fact.payload->>'cvss_score')::numeric
            ELSE 0
        END AS cvss_score,
        NULLIF(LOWER(TRIM(fact.payload->>'ecosystem')), '') AS ecosystem,
        NULLIF(TRIM(fact.payload->>'package_id'), '') AS package_id,
        NULLIF(TRIM(fact.payload->>'purl'), '') AS purl
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON fact.scope_id = scope.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'vulnerability.known_exploited'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
),
joined AS (
    SELECT
        advisory_key,
        MAX(cvss_score) FILTER (WHERE fact_kind = 'vulnerability.cve') AS cvss_score,
        (ARRAY_AGG(severity_label ORDER BY cvss_score DESC NULLS LAST)
            FILTER (WHERE fact_kind = 'vulnerability.cve' AND severity_label IS NOT NULL))[1] AS severity_label,
        (ARRAY_AGG(cve_id ORDER BY cve_id)
            FILTER (WHERE fact_kind = 'vulnerability.cve' AND cve_id IS NOT NULL))[1] AS cve_id,
        (ARRAY_AGG(ghsa_id ORDER BY ghsa_id)
            FILTER (WHERE fact_kind = 'vulnerability.cve' AND ghsa_id IS NOT NULL))[1] AS ghsa_id,
        (ARRAY_AGG(published_at ORDER BY published_at)
            FILTER (WHERE fact_kind = 'vulnerability.cve' AND published_at IS NOT NULL))[1] AS published_at,
        ARRAY(
            SELECT DISTINCT s
            FROM unnest(ARRAY_AGG(source) FILTER (WHERE fact_kind = 'vulnerability.cve' AND source IS NOT NULL)) AS s
            ORDER BY s
        ) AS sources,
        ARRAY(
            SELECT DISTINCT e
            FROM unnest(ARRAY_AGG(ecosystem) FILTER (WHERE fact_kind = 'vulnerability.affected_package')) AS e
            WHERE e IS NOT NULL ORDER BY e
        ) AS ecosystems,
        ARRAY(
            SELECT DISTINCT p
            FROM unnest(ARRAY_AGG(package_id) FILTER (WHERE fact_kind = 'vulnerability.affected_package')) AS p
            WHERE p IS NOT NULL ORDER BY p
        ) AS package_ids,
        ARRAY(
            SELECT DISTINCT u
            FROM unnest(ARRAY_AGG(purl) FILTER (WHERE fact_kind = 'vulnerability.affected_package')) AS u
            WHERE u IS NOT NULL ORDER BY u
        ) AS purls,
        bool_or(fact_kind = 'vulnerability.known_exploited' AND cve_id IS NOT NULL) AS kev
    FROM vuln_facts
    WHERE advisory_key IS NOT NULL
    GROUP BY advisory_key
    HAVING bool_or(fact_kind = 'vulnerability.cve')
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
