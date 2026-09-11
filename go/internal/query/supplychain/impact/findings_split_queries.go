// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// ListSupplyChainImpactFindingsQuery ranks only narrow source/operator metadata,
// applies suppression and pagination, then fetches JSON payloads for the bounded
// page. Keeping payloads out of the canonical-key sorts avoids width-driven
// spills while preserving source-owned finding truth and operator-only overlays.
var ListSupplyChainImpactFindingsQuery = `
WITH ` + supplyChainImpactRuntimeFilterCTE("$9", "$10", "$11", "$22", "$23") + `,
source_candidates AS (
  SELECT fact.fact_id,
         fact.scope_id,
         ` + supplyChainImpactPublicFindingIDSQL + ` AS finding_id,
         fact.source_confidence,
         COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') AS suppression_state,
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
  WHERE fact.fact_kind = $1
    AND fact.is_tombstone = FALSE
    AND generation.status = 'active'
    AND fact.scope_id <> '` + supplyChainImpactOperatorSuppressionScopeID + `'
    AND ($2 = '' OR fact.payload->>'cve_id' = $2)
    AND ($3 = '' OR fact.payload->>'package_id' = $3)
    AND ($4 = '' OR fact.payload->>'repository_id' = $4)
    AND ($5 = '' OR fact.payload->>'subject_digest' = $5)
    AND ($6 = '' OR fact.payload->>'impact_status' = $6)
    AND ($7 = '' OR fact.payload->>'advisory_id' = $7)
    AND ($8 = '' OR LOWER(fact.payload->>'ecosystem') = LOWER($8))
` + supplyChainImpactRuntimeFilterPredicate(
	"fact.payload->>'repository_id'",
	"$9",
	"$10",
	"$11",
) + `
    AND ($12 = '' OR ` + supplyChainImpactSeverityBucketFactSQL + ` = $12)
    AND (
          $13 = ''
          OR fact.payload->>'detection_profile' = $13
          OR ($13 = 'comprehensive' AND COALESCE(fact.payload->>'detection_profile', '') = '')
          OR (
                $13 = 'precise'
                AND COALESCE(fact.payload->>'detection_profile', '') = ''
                AND fact.payload->>'impact_status' IN ('affected_exact', 'not_affected_known_fixed')
                AND COALESCE(fact.payload->>'observed_version', '') <> ''
                AND fact.payload->>'match_reason' IN (
                      'npm_semver_affected_range', 'npm_semver_known_fixed',
                      'nuget_semver_affected_range', 'nuget_semver_known_fixed',
                      'cargo_semver_affected_range', 'cargo_semver_known_fixed',
                      'hex_semver_affected_range', 'hex_semver_known_fixed',
                      'maven_range_match', 'maven_known_fixed',
                      'swift_semver_affected_range', 'swift_semver_known_fixed'
                    )
             )
        )
    AND ($14 = '' OR fact.payload->>'priority_bucket' = $14)
    AND ($15 = 0 OR COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) >= $15)
    AND ($16 = '' OR fact.payload->>'image_ref' = $16)
    AND (
          (COALESCE(cardinality($22::text[]), 0) = 0 AND COALESCE(cardinality($23::text[]), 0) = 0)
          OR fact.payload->>'repository_id' = ANY($22::text[])
          OR fact.scope_id = ANY($23::text[])
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
  WHERE fact.fact_kind = $1
    AND fact.is_tombstone = FALSE
    AND generation.status = 'active'
    AND fact.scope_id = '` + supplyChainImpactOperatorSuppressionScopeID + `'
    AND COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') <> 'active'
),
source_winners AS (
  SELECT DISTINCT ON (canonical_key) *
  FROM source_candidates
  ORDER BY canonical_key, priority_score DESC, has_payload_finding_id DESC, fact_id ASC
),
operator_overrides AS (
  SELECT DISTINCT ON (canonical_key) *
  FROM operator_candidates
  ORDER BY canonical_key, priority_score DESC, has_payload_finding_id DESC, fact_id ASC
),
joined_winners AS (
  SELECT source.*,
         override.fact_id AS override_fact_id,
         CASE
           WHEN override.fact_id IS NULL THEN source.suppression_state
           ELSE ` + supplyChainImpactEffectiveDecisionStateSQL(
	"override.suppression_state",
	"override.expires_at",
	"$24::timestamptz",
) + `
         END AS effective_suppression_state
  FROM source_winners AS source
  LEFT JOIN operator_overrides AS override
    ON override.canonical_key = source.canonical_key
),
canonical_facts AS (
  SELECT *
  FROM joined_winners
  WHERE ($20 = '' OR effective_suppression_state = $20)
    AND ($21::boolean OR effective_suppression_state NOT IN (` + supplyChainImpactHiddenSuppressionStatesSQL + `))
),
paged_facts AS (
  SELECT *
  FROM canonical_facts
  WHERE $17 = ''
     OR ($18 = 'finding_id' AND finding_id > $17)
     OR (
        $18 = 'priority_score_desc'
        AND (
          priority_score < COALESCE((SELECT cursor.priority_score FROM canonical_facts AS cursor WHERE cursor.finding_id = $17), -1)
          OR (
            priority_score = COALESCE((SELECT cursor.priority_score FROM canonical_facts AS cursor WHERE cursor.finding_id = $17), -1)
            AND finding_id > $17
          )
        )
     )
     OR (
        $18 = 'priority_score_asc'
        AND (
          priority_score > COALESCE((SELECT cursor.priority_score FROM canonical_facts AS cursor WHERE cursor.finding_id = $17), 101)
          OR (
            priority_score = COALESCE((SELECT cursor.priority_score FROM canonical_facts AS cursor WHERE cursor.finding_id = $17), 101)
            AND finding_id > $17
          )
        )
     )
  ORDER BY
    CASE WHEN $18 = 'priority_score_desc' THEN priority_score END DESC,
    CASE WHEN $18 = 'priority_score_asc' THEN priority_score END ASC,
    finding_id ASC
  LIMIT $19
)
SELECT page.finding_id,
       source.source_confidence,
       ` + supplyChainImpactPayloadWithSuppressionOverlaySQL(
	"source.payload",
	"override.payload",
	"page.effective_suppression_state",
) + ` AS payload
FROM paged_facts AS page
JOIN fact_records AS source
  ON source.fact_id = page.fact_id
LEFT JOIN fact_records AS override
  ON override.fact_id = page.override_fact_id
ORDER BY
  CASE WHEN $18 = 'priority_score_desc' THEN page.priority_score END DESC,
  CASE WHEN $18 = 'priority_score_asc' THEN page.priority_score END ASC,
  page.finding_id ASC
`
