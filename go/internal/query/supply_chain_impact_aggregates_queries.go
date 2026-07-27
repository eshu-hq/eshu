// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// SQL for the reducer-owned supply-chain impact aggregate read model. The
// shared canonical-facts CTE deduplicates impact findings by canonical key and
// applies every scope predicate — including scoped-token repository/scope
// grants ($18/$19) — before any count, grouping, ordering, limit, or offset so
// aggregate totals and inventory buckets never include unauthorized rows.

var supplyChainImpactAggregateCanonicalFactsCTE = `
WITH ` + supplyChainImpactRuntimeFilterCTE("$8", "$9", "$10", "$18", "$19") + `,
` + supplyChainImpactOperatorSuppressionKeysCTE + `,
raw_facts AS (
	SELECT fact.fact_id,
	       fact.scope_id,
	       fact.payload,
	       COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') AS suppression_state,
	       COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) AS priority_score,
	       ` + supplyChainImpactPayloadFindingIDPresentSQL + ` AS has_payload_finding_id,
	       ` + supplyChainImpactCanonicalFindingKeySQL + ` AS canonical_key
	FROM fact_records AS fact
	JOIN ingestion_scopes AS scope
	  ON scope.scope_id = fact.scope_id
	 AND scope.active_generation_id = fact.generation_id
	JOIN scope_generations AS generation
	  ON generation.scope_id = fact.scope_id
	 AND generation.generation_id = fact.generation_id
	WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
	  AND fact.is_tombstone = FALSE
	  AND generation.status = 'active'
	  AND ($1 = '' OR fact.payload->>'cve_id' = $1)
	  AND ($2 = '' OR fact.payload->>'package_id' = $2)
	  AND ($3 = '' OR fact.payload->>'repository_id' = $3)
	  AND ($4 = '' OR fact.payload->>'subject_digest' = $4)
	  AND ($5 = '' OR fact.payload->>'impact_status' = $5)
	  AND ($6 = '' OR fact.payload->>'advisory_id' = $6)
	  AND ($7 = '' OR LOWER(fact.payload->>'ecosystem') = LOWER($7))
` + supplyChainImpactRuntimeFilterPredicate(
	"fact.payload->>'repository_id'",
	"$8",
	"$9",
	"$10",
) + `
	  AND ($11 = '' OR ` + supplyChainImpactSeverityBucketFactSQL + ` = $11)
	  AND (
	        $12 = ''
	        OR fact.payload->>'detection_profile' = $12
	        OR (
	              $12 = 'comprehensive'
	              AND COALESCE(fact.payload->>'detection_profile', '') = ''
	           )
	        OR (
	              $12 = 'precise'
	              AND COALESCE(fact.payload->>'detection_profile', '') = ''
	              AND fact.payload->>'impact_status' IN (
	                    'affected_exact',
	                    'not_affected_known_fixed'
	                  )
	              AND COALESCE(fact.payload->>'observed_version', '') <> ''
	              AND fact.payload->>'match_reason' IN (
	                    'npm_semver_affected_range',
	                    'npm_semver_known_fixed',
	                    'nuget_semver_affected_range',
	                    'nuget_semver_known_fixed',
	                    'cargo_semver_affected_range',
	                    'cargo_semver_known_fixed',
	                    'hex_semver_affected_range',
	                    'hex_semver_known_fixed',
	                    'maven_range_match',
	                    'maven_known_fixed',
	                    'swift_semver_affected_range',
	                    'swift_semver_known_fixed'
	                  )
	           )
	      )
	  AND ($13 = '' OR fact.payload->>'priority_bucket' = $13)
	  AND ($14 = 0 OR COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) >= $14)
	  AND ($17 = '' OR fact.payload->>'image_ref' = $17)
	  AND (
	        (COALESCE(cardinality($18::text[]), 0) = 0 AND COALESCE(cardinality($19::text[]), 0) = 0)
	        OR fact.payload->>'repository_id' = ANY($18::text[])
	        OR fact.scope_id = ANY($19::text[])
	      )
),
eligible_facts AS (
	SELECT *
	FROM raw_facts AS fact
	WHERE fact.scope_id <> '` + supplyChainImpactOperatorSuppressionScopeID + `'
	  AND NOT EXISTS (
	        SELECT 1
	        FROM operator_suppression_keys AS authority
	        WHERE authority.canonical_key = fact.canonical_key
	      )
	UNION ALL
	SELECT *
	FROM raw_facts AS fact
	WHERE fact.scope_id = '` + supplyChainImpactOperatorSuppressionScopeID + `'
	  AND fact.suppression_state <> 'active'
),
scoped_facts AS (
	SELECT *
	FROM eligible_facts AS fact
	WHERE ($15 = '' OR fact.suppression_state = $15)
	  AND ($16::boolean OR fact.suppression_state NOT IN ('not_affected','accepted_risk','false_positive','ignored'))
),
ranked_facts AS (
	SELECT *,
	       ROW_NUMBER() OVER (
	         PARTITION BY canonical_key
	         ORDER BY priority_score DESC, has_payload_finding_id DESC, fact_id ASC
	       ) AS canonical_rank
	FROM scoped_facts
),
canonical_facts AS (
	SELECT payload
	FROM ranked_facts
	WHERE canonical_rank = 1
)
`

var supplyChainImpactAggregateCountQuery = supplyChainImpactAggregateCanonicalFactsCTE + `
SELECT
	COUNT(*) AS total,
	SUM(CASE WHEN payload->>'impact_status' IN ('affected_exact', 'affected_derived', 'possibly_affected') THEN 1 ELSE 0 END) AS affected,
	SUM(CASE WHEN payload->>'impact_status' = 'affected_exact' THEN 1 ELSE 0 END) AS affected_exact,
	SUM(CASE WHEN payload->>'impact_status' = 'affected_derived' THEN 1 ELSE 0 END) AS affected_derived,
	SUM(CASE WHEN payload->>'impact_status' = 'possibly_affected' THEN 1 ELSE 0 END) AS possibly_affected,
	SUM(CASE WHEN payload->>'impact_status' LIKE 'not_affected%' THEN 1 ELSE 0 END) AS not_affected
FROM canonical_facts;
`

var supplyChainImpactAggregatePriorityCountQuery = supplyChainImpactAggregateCanonicalFactsCTE + `
SELECT
	COALESCE(NULLIF(fact.payload->>'priority_bucket', ''), 'unknown') AS bucket,
	COUNT(*) AS bucket_count
FROM canonical_facts AS fact
GROUP BY bucket;
`

var supplyChainImpactAggregateSeverityCountQuery = supplyChainImpactAggregateCanonicalFactsCTE + `
SELECT
	CASE
		WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 9.0 THEN 'critical'
		WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 7.0 THEN 'high'
		WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 4.0 THEN 'medium'
		WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) > 0.0  THEN 'low'
		ELSE 'none'
	END AS bucket,
	COUNT(*) AS bucket_count
FROM canonical_facts AS fact
GROUP BY bucket;
`

const supplyChainImpactInventoryGroupExpressionPlaceholder = "__SUPPLY_CHAIN_IMPACT_GROUP_EXPRESSION__"

var supplyChainImpactInventoryQueryTemplate = supplyChainImpactAggregateCanonicalFactsCTE + `
SELECT ` + supplyChainImpactInventoryGroupExpressionPlaceholder + ` AS bucket, COUNT(*) AS bucket_count
FROM canonical_facts AS fact
GROUP BY bucket
ORDER BY bucket_count DESC, bucket
LIMIT $20 OFFSET $21;
`
