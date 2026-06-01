package query

const supplyChainImpactFindingFactKind = "reducer_supply_chain_impact_finding"

const supplyChainImpactCanonicalFindingKeySQL = `CONCAT_WS('|',
         COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
         COALESCE(fact.payload->>'advisory_id', ''),
         COALESCE(fact.payload->>'package_id', ''),
         COALESCE(fact.payload->>'purl', ''),
         COALESCE(fact.payload->>'product_criteria', ''),
         COALESCE(fact.payload->>'match_criteria_id', ''),
         COALESCE(fact.payload->>'observed_version', ''),
         COALESCE(fact.payload->>'requested_range', ''),
         COALESCE(fact.payload->>'impact_status', ''),
         COALESCE(fact.payload->>'repository_id', ''),
         COALESCE(fact.payload->>'subject_digest', '')
       )`

const supplyChainImpactPublicFindingIDSQL = `COALESCE(
         NULLIF(fact.payload->>'finding_id', ''),
         ` + supplyChainImpactCanonicalFindingKeySQL + `
       )`

const supplyChainImpactPayloadFindingIDPresentSQL = `CASE
         WHEN NULLIF(fact.payload->>'finding_id', '') IS NULL THEN 0
         ELSE 1
       END`

const listSupplyChainImpactFindingsQuery = `
WITH scoped_facts AS (
SELECT fact.fact_id,
       ` + supplyChainImpactPublicFindingIDSQL + ` AS finding_id,
       fact.source_confidence,
       fact.payload,
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
WHERE fact.fact_kind = $1
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($2 = '' OR fact.payload->>'cve_id' = $2)
  AND ($3 = '' OR fact.payload->>'package_id' = $3)
  AND ($4 = '' OR fact.payload->>'repository_id' = $4)
  AND ($5 = '' OR fact.payload->>'subject_digest' = $5)
  AND ($6 = '' OR fact.payload->>'impact_status' = $6)
  AND (
        $7 = ''
        OR fact.payload->>'detection_profile' = $7
        OR (
              $7 = 'comprehensive'
              AND COALESCE(fact.payload->>'detection_profile', '') = ''
           )
        OR (
              $7 = 'precise'
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
  AND ($8 = '' OR fact.payload->>'priority_bucket' = $8)
  AND ($9 = 0 OR COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) >= $9)
  AND ($13 = '' OR COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') = $13)
  AND ($14::boolean OR COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') NOT IN ('not_affected','accepted_risk','false_positive','ignored'))
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
SELECT fact_id, finding_id, source_confidence, payload, priority_score, has_payload_finding_id
FROM ranked_facts
WHERE canonical_rank = 1
)
SELECT finding_id, source_confidence, payload
FROM canonical_facts
WHERE $10 = ''
   OR ($11 = 'finding_id' AND finding_id > $10)
   OR (
      $11 = 'priority_score_desc'
      AND (
        priority_score < COALESCE((SELECT cursor.priority_score FROM canonical_facts AS cursor WHERE cursor.finding_id = $10), -1)
        OR (
          priority_score = COALESCE((SELECT cursor.priority_score FROM canonical_facts AS cursor WHERE cursor.finding_id = $10), -1)
          AND finding_id > $10
        )
      )
   )
   OR (
      $11 = 'priority_score_asc'
      AND (
        priority_score > COALESCE((SELECT cursor.priority_score FROM canonical_facts AS cursor WHERE cursor.finding_id = $10), 101)
        OR (
          priority_score = COALESCE((SELECT cursor.priority_score FROM canonical_facts AS cursor WHERE cursor.finding_id = $10), 101)
          AND finding_id > $10
        )
      )
   )
ORDER BY
  CASE WHEN $11 = 'priority_score_desc' THEN priority_score END DESC,
  CASE WHEN $11 = 'priority_score_asc' THEN priority_score END ASC,
  finding_id ASC
LIMIT $12
`
