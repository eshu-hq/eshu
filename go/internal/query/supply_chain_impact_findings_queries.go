// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const supplyChainImpactFindingFactKind = "reducer_supply_chain_impact_finding"

const supplyChainImpactOperatorSuppressionScopeID = "operator:vulnerability_suppressions"

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

const supplyChainImpactSeverityBucketFactSQL = `CASE
    WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 9.0 THEN 'critical'
    WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 7.0 THEN 'high'
    WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) >= 4.0 THEN 'medium'
    WHEN COALESCE(NULLIF(fact.payload->>'cvss_score', '')::numeric, 0) > 0.0  THEN 'low'
    ELSE 'none'
  END`

const supplyChainImpactHiddenSuppressionStatesSQL = "'not_affected','accepted_risk','false_positive','ignored'"

func supplyChainImpactEffectiveSuppressionStateSQL(
	scopeExpr string,
	stateExpr string,
	expiresAtExpr string,
	readAtExpr string,
) string {
	return `CASE
    WHEN ` + scopeExpr + ` <> '` + supplyChainImpactOperatorSuppressionScopeID + `'
      OR ` + stateExpr + ` NOT IN (` + supplyChainImpactHiddenSuppressionStatesSQL + `)
      OR NULLIF(` + expiresAtExpr + `, '') IS NULL
    THEN ` + stateExpr + `
    WHEN NOT pg_input_is_valid(` + expiresAtExpr + `, 'timestamp with time zone')
    THEN 'expired'
    WHEN (` + expiresAtExpr + `)::timestamptz <= ` + readAtExpr + `
    THEN 'expired'
    ELSE ` + stateExpr + `
  END`
}

func supplyChainImpactPayloadWithEffectiveSuppressionSQL(
	payloadExpr string,
	effectiveStateExpr string,
) string {
	return `(` + payloadExpr + ` || jsonb_build_object(
    'suppression_state', ` + effectiveStateExpr + `,
    'suppression',
      COALESCE(
        CASE
          WHEN jsonb_typeof(` + payloadExpr + `->'suppression') = 'object'
          THEN ` + payloadExpr + `->'suppression'
        END,
        '{}'::jsonb
      ) || jsonb_build_object('state', ` + effectiveStateExpr + `)
  ))`
}

func supplyChainImpactPayloadWithOperatorEffectiveSuppressionSQL(
	scopeExpr string,
	stateExpr string,
	payloadExpr string,
	effectiveStateExpr string,
) string {
	return `CASE
    WHEN ` + scopeExpr + ` = '` + supplyChainImpactOperatorSuppressionScopeID + `'
      AND ` + stateExpr + ` <> 'active'
    THEN ` + supplyChainImpactPayloadWithEffectiveSuppressionSQL(
		payloadExpr,
		effectiveStateExpr,
	) + `
    ELSE ` + payloadExpr + `
  END`
}

var listSupplyChainImpactFindingsQuery = `
WITH ` + supplyChainImpactRuntimeFilterCTE("$9", "$10", "$11", "$22", "$23") + `,
raw_facts AS (
SELECT fact.fact_id,
       fact.scope_id,
       ` + supplyChainImpactPublicFindingIDSQL + ` AS finding_id,
       fact.source_confidence,
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
WHERE fact.fact_kind = $1
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
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
        OR (
              $13 = 'comprehensive'
              AND COALESCE(fact.payload->>'detection_profile', '') = ''
           )
        OR (
              $13 = 'precise'
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
    AND ($14 = '' OR fact.payload->>'priority_bucket' = $14)
    AND ($15 = 0 OR COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) >= $15)
    AND ($16 = '' OR fact.payload->>'image_ref' = $16)
    AND (
          (COALESCE(cardinality($22::text[]), 0) = 0 AND COALESCE(cardinality($23::text[]), 0) = 0)
          OR fact.payload->>'repository_id' = ANY($22::text[])
          OR fact.scope_id = ANY($23::text[])
        )
),
ranked_facts AS (
SELECT *,
       ROW_NUMBER() OVER (
         PARTITION BY canonical_key
         ORDER BY
           CASE
             WHEN scope_id = '` + supplyChainImpactOperatorSuppressionScopeID + `'
               AND suppression_state <> 'active'
             THEN 0
             ELSE 1
           END ASC,
           priority_score DESC, has_payload_finding_id DESC, fact_id ASC
       ) AS canonical_rank
FROM raw_facts
WHERE NOT (
  scope_id = '` + supplyChainImpactOperatorSuppressionScopeID + `'
  AND suppression_state = 'active'
)
),
canonical_winners AS (
SELECT fact_id, scope_id, finding_id, source_confidence, payload,
       suppression_state, priority_score, has_payload_finding_id
FROM ranked_facts
WHERE canonical_rank = 1
),
canonical_facts AS (
SELECT fact_id, scope_id, finding_id, source_confidence, payload,
       suppression_state,
       priority_score, has_payload_finding_id
FROM canonical_winners AS fact
WHERE (
  $20 = ''
  OR ` + supplyChainImpactEffectiveSuppressionStateSQL(
	"fact.scope_id",
	"fact.suppression_state",
	"fact.payload #>> '{suppression,expires_at}'",
	"$24::timestamptz",
) + ` = $20
)
  AND (
    $21::boolean
    OR ` + supplyChainImpactEffectiveSuppressionStateSQL(
	"fact.scope_id",
	"fact.suppression_state",
	"fact.payload #>> '{suppression,expires_at}'",
	"$24::timestamptz",
) + ` NOT IN ('not_affected','accepted_risk','false_positive','ignored')
  )
)
SELECT finding_id,
       source_confidence,
       ` + supplyChainImpactPayloadWithOperatorEffectiveSuppressionSQL(
	"scope_id",
	"suppression_state",
	"payload",
	supplyChainImpactEffectiveSuppressionStateSQL(
		"scope_id",
		"suppression_state",
		"payload #>> '{suppression,expires_at}'",
		"$24::timestamptz",
	),
) + ` AS payload
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
`

// listSupplyChainImpactFindingsFromWinnersQuery is the #3389 Phase 2 read that
// serves the same page from the maintained supply_chain_impact_canonical_winners
// read model instead of deduplicating at read time. The winners table already
// holds one row per canonical_key (the same winner the ROW_NUMBER dedup picks),
// denormalized with every filterable column, so this query runs the filters +
// keyset cursor + LIMIT on the winners table alone (index-served, O(page), no
// window, no sort spill) and joins fact_records by winner_fact_id only for the
// page payloads.
//
// It takes the SAME 24-parameter slice as listSupplyChainImpactFindingsQuery so
// the store can swap queries without rebuilding args; $1 (fact_kind) is not a
// filter here because the winners table is impact-only, but it is referenced in a
// trivially-true guard so every bound parameter is used.
//
// Correctness: winner currency is materialization-enforced (the reducer
// maintainer keeps the table reconciled with the active facts), so this read does
// NOT re-join the active-generation tables — that join defeats O(page) (measured)
// and the maintainer already excludes inactive winners. Output is byte-identical
// to the read-time-dedup query (verified across the filter/sort/cursor matrix).
var listSupplyChainImpactFindingsFromWinnersQuery = `
WITH ` + supplyChainImpactRuntimeFilterCTE("$9", "$10", "$11", "$22", "$23") + `,
filtered AS NOT MATERIALIZED (
    SELECT w.finding_id,
           w.winner_fact_id,
           w.priority_score,
           ` + supplyChainImpactEffectiveSuppressionStateSQL(
	"w.winner_scope_id",
	"w.suppression_state",
	"COALESCE(w.suppression_expires_at::text, '')",
	"$24::timestamptz",
) + ` AS effective_suppression_state
    FROM supply_chain_impact_canonical_winners AS w
    WHERE ($1 = $1)
      AND ($2 = '' OR w.cve_id = $2)
      AND ($3 = '' OR w.package_id = $3)
      AND ($4 = '' OR w.repository_id = $4)
      AND ($5 = '' OR w.subject_digest = $5)
      AND ($6 = '' OR w.impact_status = $6)
      AND ($7 = '' OR w.advisory_id = $7)
      AND ($8 = '' OR LOWER(w.ecosystem) = LOWER($8))
` + supplyChainImpactRuntimeFilterPredicate(
	"w.repository_id",
	"$9",
	"$10",
	"$11",
) + `
      AND ($12 = '' OR w.severity_bucket = $12)
      AND (
            $13 = ''
            OR w.detection_profile = $13
            OR ($13 = 'comprehensive' AND COALESCE(w.detection_profile, '') = '')
            OR (
                  $13 = 'precise'
                  AND COALESCE(w.detection_profile, '') = ''
                  AND w.impact_status IN ('affected_exact', 'not_affected_known_fixed')
                  AND COALESCE(w.observed_version, '') <> ''
                  AND w.match_reason IN (
                        'npm_semver_affected_range', 'npm_semver_known_fixed',
                        'nuget_semver_affected_range', 'nuget_semver_known_fixed',
                        'cargo_semver_affected_range', 'cargo_semver_known_fixed',
                        'hex_semver_affected_range', 'hex_semver_known_fixed',
                        'maven_range_match', 'maven_known_fixed',
                        'swift_semver_affected_range', 'swift_semver_known_fixed'
                      )
               )
          )
      AND ($14 = '' OR w.priority_bucket = $14)
      AND ($15 = 0 OR w.priority_score >= $15)
      AND ($16 = '' OR w.image_ref = $16)
      AND (
            $20 = ''
            OR ` + supplyChainImpactEffectiveSuppressionStateSQL(
	"w.winner_scope_id",
	"w.suppression_state",
	"COALESCE(w.suppression_expires_at::text, '')",
	"$24::timestamptz",
) + ` = $20
          )
      AND (
            $21::boolean
            OR ` + supplyChainImpactEffectiveSuppressionStateSQL(
	"w.winner_scope_id",
	"w.suppression_state",
	"COALESCE(w.suppression_expires_at::text, '')",
	"$24::timestamptz",
) + ` NOT IN (` + supplyChainImpactHiddenSuppressionStatesSQL + `)
          )
      AND (
            (COALESCE(cardinality($22::text[]), 0) = 0 AND COALESCE(cardinality($23::text[]), 0) = 0)
            OR w.repository_id = ANY($22::text[])
            OR w.winner_scope_id = ANY($23::text[])
          )
)
SELECT f.finding_id,
       refetch.source_confidence,
       ` + supplyChainImpactPayloadWithEffectiveSuppressionSQL(
	"refetch.payload",
	"f.effective_suppression_state",
) + ` AS payload
FROM filtered AS f
JOIN fact_records AS refetch
  ON refetch.fact_id = f.winner_fact_id
WHERE (
        $17 = ''
        OR ($18 = 'finding_id' AND f.finding_id > $17)
        OR (
              $18 = 'priority_score_desc'
              AND (
                f.priority_score < COALESCE((SELECT c.priority_score FROM filtered c WHERE c.finding_id = $17), -1)
                OR (
                  f.priority_score = COALESCE((SELECT c.priority_score FROM filtered c WHERE c.finding_id = $17), -1)
                  AND f.finding_id > $17
                )
              )
           )
        OR (
              $18 = 'priority_score_asc'
              AND (
                f.priority_score > COALESCE((SELECT c.priority_score FROM filtered c WHERE c.finding_id = $17), 101)
                OR (
                  f.priority_score = COALESCE((SELECT c.priority_score FROM filtered c WHERE c.finding_id = $17), 101)
                  AND f.finding_id > $17
                )
              )
           )
      )
ORDER BY
  CASE WHEN $18 = 'priority_score_desc' THEN f.priority_score END DESC,
  CASE WHEN $18 = 'priority_score_asc' THEN f.priority_score END ASC,
  f.finding_id ASC
LIMIT $19
`
