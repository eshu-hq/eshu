// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// SQL for the reducer-owned supply-chain impact aggregate read model. The
// shared canonical-facts CTE deduplicates impact findings by canonical key and
// applies every scope predicate — including scoped-token repository/scope
// grants ($18/$19) — before any count, grouping, ordering, limit, or offset so
// aggregate totals and inventory buckets never include unauthorized rows.

var SupplyChainImpactAggregateCanonicalFactsCTE = `
WITH ` + supplyChainImpactRuntimeFilterCTE("$8", "$9", "$10", "$18", "$19") + `,
source_candidates AS (
  SELECT fact.fact_id,
         fact.scope_id,
         COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') AS suppression_state,
         COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) AS priority_score,
         ` + supplyChainImpactPayloadFindingIDPresentSQL + ` AS has_payload_finding_id,
         ` + SupplyChainImpactCanonicalFindingKeySQL + ` AS canonical_key,
         COALESCE(fact.payload->>'impact_status', '') AS impact_status,
         COALESCE(fact.payload->>'priority_bucket', '') AS priority_bucket,
         ` + supplyChainImpactSeverityBucketFactSQL + ` AS severity_bucket,
         COALESCE(fact.payload->>'repository_id', '') AS repository_id,
         COALESCE(LOWER(fact.payload->>'ecosystem'), '') AS ecosystem
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
    AND fact.scope_id <> '` + supplyChainImpactOperatorSuppressionScopeID + `'
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
operator_candidates AS (
  SELECT fact.fact_id,
         COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') AS suppression_state,
         fact.payload #>> '{suppression,expires_at}' AS expires_at,
         COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) AS priority_score,
         ` + supplyChainImpactPayloadFindingIDPresentSQL + ` AS has_payload_finding_id,
         ` + SupplyChainImpactCanonicalFindingKeySQL + ` AS canonical_key
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
    AND fact.scope_id = '` + supplyChainImpactOperatorSuppressionScopeID + `'
    AND COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') <> 'active'
),
source_winners AS (
  SELECT DISTINCT ON (canonical_key) *
  FROM source_candidates
  ORDER BY canonical_key,
           priority_score DESC,
           has_payload_finding_id DESC,
           fact_id ASC
),
operator_overrides AS (
  SELECT DISTINCT ON (canonical_key) *
  FROM operator_candidates
  ORDER BY canonical_key,
           priority_score DESC,
           has_payload_finding_id DESC,
           fact_id ASC
),
joined_winners AS (
  SELECT source.impact_status,
         source.priority_bucket,
         source.severity_bucket,
         source.repository_id,
         source.ecosystem,
         CASE
           WHEN override.fact_id IS NULL THEN source.suppression_state
           ELSE ` + supplyChainImpactEffectiveDecisionStateSQL(
	"override.suppression_state",
	"override.expires_at",
	"$20::timestamptz",
) + `
         END AS suppression_state
  FROM source_winners AS source
  LEFT JOIN operator_overrides AS override
    ON override.canonical_key = source.canonical_key
),
canonical_facts AS (
  SELECT impact_status,
         priority_bucket,
         severity_bucket,
         repository_id,
         ecosystem
  FROM joined_winners AS fact
  WHERE ($15 = '' OR fact.suppression_state = $15)
    AND (
      $16::boolean
      OR fact.suppression_state NOT IN (` + supplyChainImpactHiddenSuppressionStatesSQL + `)
    )
)
`

var SupplyChainImpactAggregateCountQuery = SupplyChainImpactAggregateCanonicalFactsCTE + `
SELECT
	COUNT(*) AS total,
	SUM(CASE WHEN impact_status IN ('affected_exact', 'affected_derived', 'possibly_affected') THEN 1 ELSE 0 END) AS affected,
	SUM(CASE WHEN impact_status = 'affected_exact' THEN 1 ELSE 0 END) AS affected_exact,
	SUM(CASE WHEN impact_status = 'affected_derived' THEN 1 ELSE 0 END) AS affected_derived,
	SUM(CASE WHEN impact_status = 'possibly_affected' THEN 1 ELSE 0 END) AS possibly_affected,
	SUM(CASE WHEN impact_status LIKE 'not_affected%' THEN 1 ELSE 0 END) AS not_affected
FROM canonical_facts;
`

var SupplyChainImpactAggregatePriorityCountQuery = SupplyChainImpactAggregateCanonicalFactsCTE + `
SELECT
	COALESCE(NULLIF(fact.priority_bucket, ''), 'unknown') AS bucket,
	COUNT(*) AS bucket_count
FROM canonical_facts AS fact
GROUP BY bucket;
`

var SupplyChainImpactAggregateSeverityCountQuery = SupplyChainImpactAggregateCanonicalFactsCTE + `
SELECT
	COALESCE(NULLIF(fact.severity_bucket, ''), 'none') AS bucket,
	COUNT(*) AS bucket_count
FROM canonical_facts AS fact
GROUP BY bucket;
`

const supplyChainImpactInventoryGroupExpressionPlaceholder = "__SUPPLY_CHAIN_IMPACT_GROUP_EXPRESSION__"

var SupplyChainImpactInventoryQueryTemplate = SupplyChainImpactAggregateCanonicalFactsCTE + `
SELECT ` + supplyChainImpactInventoryGroupExpressionPlaceholder + ` AS bucket, COUNT(*) AS bucket_count
FROM canonical_facts AS fact
GROUP BY bucket
ORDER BY bucket_count DESC, bucket
LIMIT $21 OFFSET $22;
`
