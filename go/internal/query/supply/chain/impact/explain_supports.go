// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

// The explain reads of #10880 PR-B2a answer from the per-support table. They
// keep the legacy explain's shape: the same 19 bind arguments, the same selector
// and grant predicates over the support row's own columns, the same
// source_candidates identity predicate, the same canonical winner pick and the
// same suppression overlay. Only the fact_records reads change: candidates come
// from supply_chain_impact_finding_supports, and the operator override is the
// indexed lateral probe the list read uses.
//
// Two source shapes serve two lookups:
//
//   - A public finding id probes (finding_id) directly: the exact id names at
//     most a few supports, so the grant applies as a filter over those rows.
//   - Any other granted lookup drives from the grant, as two index arms, so a
//     selector over a large grant never scans the table.
//
// An ungranted selector or identity lookup has no supports read yet (the table
// has no selector index) and stays on the legacy read.

// supportsExplainOverrideLateral is the operator override probe of the explain
// reads. Unlike the list's, it returns the override payload, which the explain
// overlays onto the picked source payload.
const supportsExplainOverrideLateral = `LEFT JOIN LATERAL (
    SELECT operator.fact_id, operator.scope_id, operator.payload, operator.suppression_state
    FROM supply_chain_impact_finding_supports AS operator
    WHERE operator.scope_id = 'operator:vulnerability_suppressions'
      AND operator.canonical_key = source.canonical_key
      AND operator.suppression_state <> 'active'
      AND operator.generation_id = (
        SELECT g.generation_id FROM ingestion_scopes AS s
        JOIN scope_generations AS g ON g.scope_id = s.scope_id AND g.generation_id = s.active_generation_id
        WHERE s.scope_id = 'operator:vulnerability_suppressions' AND g.status = 'active')
    ORDER BY operator.priority_score DESC, operator.has_payload_finding_id DESC, operator.fact_id ASC
    LIMIT 1
  ) AS override ON TRUE`

// supportsExplainGrantArms is the grant-driven source of a granted explain
// lookup: the scope arm, then the repository arm excluding the scopes the first
// covers. $11 and $12 are the allowed repository and scope ids.
const supportsExplainGrantArms = `(SELECT * FROM supply_chain_impact_finding_supports WHERE scope_id = ANY($12::text[])
        UNION ALL
        SELECT * FROM supply_chain_impact_finding_supports WHERE repository_id = ANY($11::text[])
          AND repository_id <> ''
          AND NOT COALESCE(scope_id = ANY($12::text[]), FALSE))`

// supportsExplainPublicIDPredicate pins a lookup to the exact public finding id
// a finding carries in its payload, as the legacy public-id read does: a finding
// whose id is only its canonical key is not matched by id.
const supportsExplainPublicIDPredicate = `
    AND fact.has_payload_finding_id = 1
    AND fact.finding_id = $2`

// buildExplainFromSupportsQuery composes one supports explain read. from is the
// source of the authorized candidates; authorizedPredicate and
// sourceCandidatePredicate are the legacy builder's extension points.
func buildExplainFromSupportsQuery(from, fence, authorizedPredicate, sourceCandidatePredicate string) (string, error) {
	winners, err := deriveSupportsExplainWinners(supplyChainImpactSplitCanonicalWinnerCTEs("$13::timestamptz"))
	if err != nil {
		return "", err
	}
	if fence != "" {
		fence = ",\n" + fence
	}
	return `
WITH ` + supplyChainImpactRuntimeFilterCTE("$9", "$8", "''", "$11", "$12") + fence + `,
authorized_source_candidates AS NOT MATERIALIZED (
  SELECT fact.fact_id, fact.scope_id, fact.finding_id, fact.source_confidence, fact.payload,
         fact.suppression_state, fact.priority_score, fact.has_payload_finding_id, fact.canonical_key
  FROM ` + from + ` AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
  LEFT JOIN scanner_image_result_fact_classifications AS finding_class
    ON finding_class.finding_fact_id = fact.fact_id
  LEFT JOIN scanner_image_result_fact_associations AS finding_association
    ON finding_association.finding_fact_id = fact.fact_id
  WHERE $1::text IS NOT NULL
    AND generation.status = 'active'
    AND fact.scope_id <> '` + supplyChainImpactOperatorSuppressionScopeID + `'
    AND $14::boolean IS NOT NULL AND $15::text IS NOT NULL AND $16::text IS NOT NULL
    AND $17::text IS NOT NULL AND $18::text IS NOT NULL AND $19::text[] IS NOT NULL
` + supportsStaticAuthoritySQL + `    AND ($3 = '' OR fact.advisory_id = $3 OR fact.cve_id = $3)
    AND ($4 = '' OR fact.cve_id = $4)
    AND ($5 = '' OR fact.package_id = $5)
    AND ($6 = '' OR fact.repository_id = $6)
    AND ($7 = '' OR fact.subject_digest = $7)
` + supplyChainImpactRuntimeFilterPredicate("fact.repository_id", "$9", "$8", "''") + `
    AND ($10 = '' OR fact.image_ref = $10)
    AND (
      (COALESCE(cardinality($11::text[]), 0) = 0 AND COALESCE(cardinality($12::text[]), 0) = 0)
      OR fact.repository_id = ANY($11::text[])
      OR fact.scope_id = ANY($12::text[])
    )` + authorizedPredicate + `
),
source_candidates AS MATERIALIZED (
  SELECT *
  FROM authorized_source_candidates
` + sourceCandidatePredicate + `
),` + winners + `,
canonical_facts AS (
  SELECT finding_id, source_confidence, payload
  FROM canonical_winners
)
SELECT finding_id, source_confidence, payload
FROM canonical_facts
ORDER BY finding_id ASC
LIMIT 2
`, nil
}

// deriveSupportsExplainWinners swaps the legacy split canonical winner CTEs'
// operator stream for the lateral probe. A marker the legacy text lost returns
// an error naming it.
func deriveSupportsExplainWinners(legacy string) (string, error) {
	start := strings.Index(legacy, "operator_overrides AS (")
	end := strings.Index(legacy, "joined_winners AS (")
	if start < 0 || end < start {
		return "", fmt.Errorf("legacy explain winner CTEs lost the operator_overrides block: update the supports derivation")
	}
	query := legacy[:start] + legacy[end:]
	const overrideJoin = "LEFT JOIN operator_overrides AS override\n    ON override.canonical_key = source.canonical_key"
	if !strings.Contains(query, overrideJoin) {
		return "", fmt.Errorf("legacy explain winner CTEs lost the operator_overrides join: update the supports derivation")
	}
	return strings.Replace(query, overrideJoin, supportsExplainOverrideLateral, 1), nil
}

func mustBuildExplainFromSupportsQuery(from, fence, authorizedPredicate, sourceCandidatePredicate string) string {
	query, err := buildExplainFromSupportsQuery(from, fence, authorizedPredicate, sourceCandidatePredicate)
	if err != nil {
		panic(err)
	}
	return supplyChainImpactReadModeSQL(query, false)
}

// supportsExplainIdentityPredicate is the legacy ExplainFindingQuery identity
// predicate: an empty $2 applies no identifier filter, and a non-empty $2
// matches a fact id, finding id, or canonical key, or the canonical key of the
// candidate whose fact id is $2.
const supportsExplainIdentityPredicate = `
  WHERE $2 = ''
     OR fact_id = $2
     OR finding_id = $2
     OR canonical_key = $2
     OR canonical_key IN (
          SELECT canonical_key
          FROM authorized_source_candidates AS identity_candidate
          WHERE identity_candidate.fact_id = $2
        )`

// ExplainFindingByPublicIDFromSupportsQuery explains one finding anchored on its
// exact public finding id: the id probes the finding index, and the grant
// applies as a filter over the few supports that carry it.
var ExplainFindingByPublicIDFromSupportsQuery = mustBuildExplainFromSupportsQuery(
	"supply_chain_impact_finding_supports", "", supportsExplainPublicIDPredicate, "")

// ExplainFindingFromSupportsQuery is the granted ExplainFindingQuery: a
// selector or identity lookup driven from the grant arms.
var ExplainFindingFromSupportsQuery = mustBuildExplainFromSupportsQuery(
	supportsExplainGrantArms, "", "", supportsExplainIdentityPredicate)

// supportsExplainImageFence is the ungranted selector lookup with an image
// reference: the candidates come from the image partial index through a
// MATERIALIZED fetch, the same fence the list read pins, so the planner cannot
// flatten it into a scope-driven join.
const supportsExplainImageFence = `image_supports AS MATERIALIZED (
  SELECT * FROM supply_chain_impact_finding_supports
  WHERE image_ref = $10 AND image_ref <> ''
)`

// ExplainFindingFromImageQuery is the ungranted selector explain with an image
// reference: filter, then pick, from the image partial index. The identity
// predicate applies to the image candidates exactly as the legacy read applies it
// to its authorized candidates, so an identity lookup that names an image keeps its
// legacy answer.
var ExplainFindingFromImageQuery = mustBuildExplainFromSupportsQuery(
	"image_supports", supportsExplainImageFence, "", supportsExplainIdentityPredicate)

// explainRoute names the query each lookup of one explain call runs.
type explainRoute struct {
	// supports reports that the call reads the per-support table and must pass
	// the readiness probe first.
	supports bool
	// publicID is the query for a call that names a finding_id; selector is the
	// query for every other lookup, and the identity fallback of a public-id miss.
	publicID, selector string
	// publicIDRoute and selectorRoute label the statements for the route counter
	// and the read-duration histogram: legacy for the reads that stay on the fact
	// table, so the metric shows how many explain statements still take them.
	publicIDRoute, selectorRoute ReadRoute
}

// explainRoute decides where one explain call reads. With the winners gate off
// every call keeps the legacy fact read. With it on:
//
//   - a granted call reads supports for both lookups: the winners row is one
//     pick across scopes and cannot answer a grant, and a finding id a granted
//     list returned must explain with the same payload;
//   - an ungranted public-id lookup probes supports by finding id, and a miss
//     falls back to the legacy identity read, because supports has no index for
//     an ungranted fact-id or canonical-key lookup (the identity read moves to
//     supports in PR-C on its own measured index);
//   - an ungranted selector lookup with an image reference reads supports through
//     the image partial index, filter then pick;
//   - an ungranted selector lookup without one keeps the legacy read. The
//     arbiter's contract S reads it from supports through the cve and advisory
//     indexes of PR-B2b, so it lands after those migrations; the winners table
//     cannot name its candidates (a mixed cve_id key, a second key that matches
//     only on a lower copy, and a replaced winning support each give a wrong
//     answer).
func (s PostgresFindingStore) explainRoute(filter packet.ExplanationFilter) explainRoute {
	legacy := explainRoute{
		publicID: ExplainFindingByPublicIDQuery, selector: ExplainFindingQuery,
		publicIDRoute: RouteLegacy, selectorRoute: RouteLegacy,
	}
	if !s.ReadFromWinners {
		return legacy
	}
	if len(filter.AllowedRepositoryIDs) > 0 || len(filter.AllowedScopeIDs) > 0 {
		return explainRoute{
			supports: true, publicID: ExplainFindingByPublicIDFromSupportsQuery, selector: ExplainFindingFromSupportsQuery,
			publicIDRoute: RouteSupportsGrant, selectorRoute: RouteSupportsGrant,
		}
	}
	route := explainRoute{
		supports: filter.FindingID != "" || filter.ImageRef != "",
		publicID: ExplainFindingByPublicIDFromSupportsQuery, selector: ExplainFindingQuery,
		publicIDRoute: RouteSupportsFindingID, selectorRoute: RouteLegacy,
	}
	// One exception to "the identity lookup stays legacy": a finding id that misses the
	// public-id probe AND names an image reference falls back to the image read
	// (filter, then identity, on the image candidates), which is the same answer the
	// legacy identity read gives for that filter set; it counts as supports_image.
	if filter.ImageRef != "" {
		route.selector, route.selectorRoute = ExplainFindingFromImageQuery, RouteSupportsImage
	}
	if filter.FindingID == "" && filter.ImageRef == "" {
		return legacy
	}
	return route
}
