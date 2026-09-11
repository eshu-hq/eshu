// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// supplyChainImpactOperatorCandidatesCTE returns the authoritative operator
// suppression stream. Caller filters and grants deliberately do not apply here:
// authorization is established by joining an override onto an already-authorized
// active source finding with the same canonical key.
func supplyChainImpactOperatorCandidatesCTE(factKindExpr string) string {
	return supplyChainImpactOperatorCandidatesWithPredicateCTE(
		factKindExpr,
		"",
		false,
	)
}

func supplyChainImpactBoundedOperatorCandidatesCTE(factKindExpr string) string {
	return supplyChainImpactOperatorCandidatesWithPredicateCTE(
		factKindExpr,
		`
    AND `+SupplyChainImpactCanonicalFindingKeySQL+` IN (
      SELECT canonical_key FROM source_candidates
    )`,
		true,
	)
}

func supplyChainImpactOperatorCandidatesWithPredicateCTE(
	factKindExpr string,
	extraPredicate string,
	materialized bool,
) string {
	materialization := ""
	if materialized {
		materialization = " MATERIALIZED"
	}
	return `
operator_candidates AS` + materialization + ` (
  SELECT fact.fact_id,
         fact.scope_id,
         ` + supplyChainImpactPublicFindingIDSQL + ` AS finding_id,
         fact.source_confidence,
         fact.payload,
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
  WHERE fact.fact_kind = ` + factKindExpr + `
    AND fact.is_tombstone = FALSE
    AND generation.status = 'active'
    AND fact.scope_id = '` + supplyChainImpactOperatorSuppressionScopeID + `'
    AND COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') <> 'active'` + extraPredicate + `
)`
}

// supplyChainImpactSplitCanonicalWinnerCTEs ranks source and operator streams
// independently, then overlays only the suppression decision onto source truth.
// An operator-only canonical key cannot appear because source_winners owns the
// left side of the join.
func supplyChainImpactSplitCanonicalWinnerCTEs(readAtExpr string) string {
	operatorEffectiveState := supplyChainImpactEffectiveSuppressionStateSQL(
		"override.scope_id",
		"override.suppression_state",
		"override.payload #>> '{suppression,expires_at}'",
		readAtExpr,
	)
	return `
source_winners AS (
  SELECT DISTINCT ON (canonical_key)
         fact_id, scope_id, finding_id, source_confidence, payload,
         suppression_state, priority_score, has_payload_finding_id,
         canonical_key
  FROM source_candidates
  ORDER BY canonical_key,
           priority_score DESC,
           has_payload_finding_id DESC,
           fact_id ASC
),
operator_overrides AS (
  SELECT DISTINCT ON (canonical_key)
         fact_id, scope_id, payload, suppression_state, canonical_key
  FROM operator_candidates
  ORDER BY canonical_key,
           priority_score DESC,
           has_payload_finding_id DESC,
           fact_id ASC
),
joined_winners AS (
  SELECT source.fact_id,
         source.scope_id,
         source.finding_id,
         source.source_confidence,
         source.payload AS source_payload,
         override.payload AS override_payload,
         CASE
           WHEN override.fact_id IS NULL THEN source.suppression_state
           ELSE ` + operatorEffectiveState + `
         END AS effective_suppression_state,
         source.priority_score,
         source.has_payload_finding_id,
         source.canonical_key
  FROM source_winners AS source
  LEFT JOIN operator_overrides AS override
    ON override.canonical_key = source.canonical_key
),
canonical_winners AS (
  SELECT fact_id,
         scope_id,
         finding_id,
         source_confidence,
         ` + supplyChainImpactPayloadWithSuppressionOverlaySQL(
		"source_payload",
		"override_payload",
		"effective_suppression_state",
	) + ` AS payload,
         effective_suppression_state AS suppression_state,
         priority_score,
         has_payload_finding_id,
         canonical_key
  FROM joined_winners
)`
}
