// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
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

// supportsExplainWinnersSource is the ungranted selector lookup without an
// image reference: the supports rows that are the winning pick of the keys whose
// winners row matches the key-determined selectors (cve, advisory, package,
// repository, subject digest, and the runtime filters, which resolve through the
// repository) and the identity lookup. The picks are the whole candidate set of
// their keys, so the legacy pick over every copy lands on the same row. The
// winners table has no selector index; the measured scan over 80,000 rows is
// 10 ms (docs/internal/evidence/10880-pr-b2a/RESULTS.md). The winning
// supports are fetched by primary key inside a MATERIALIZED CTE, fed by an
// ARRAY(subselect) InitPlan: left inline, the planner pulls the fetch up into a
// join and drives from the scope tables (the join-form trap of the list read),
// which the plan-shape guard rejects.
//
// The identity arm mirrors supportsExplainIdentityPredicate over every copy: the
// id may be the key, the winner's finding id or fact id, or the fact id of any
// copy of the key (resolved through the fact id primary key).
var supportsExplainWinnersFence = `winner_picks AS MATERIALIZED (
  SELECT s.*
  FROM supply_chain_impact_finding_supports AS s
  WHERE s.fact_id = ANY(ARRAY(
          SELECT w.winner_fact_id
          FROM supply_chain_impact_canonical_winners AS w
          WHERE ($3 = '' OR w.advisory_id = $3 OR w.cve_id = $3)
            AND ($4 = '' OR w.cve_id = $4)
            AND ($5 = '' OR w.package_id = $5)
            AND ($6 = '' OR w.repository_id = $6)
            AND ($7 = '' OR w.subject_digest = $7)
` + supplyChainImpactRuntimeFilterPredicate("w.repository_id", "$9", "$8", "''") + `
            AND ($2 = ''
                 OR w.winner_fact_id = $2
                 OR w.finding_id = $2
                 OR w.canonical_key = $2
                 OR w.canonical_key IN (
                      SELECT identity.canonical_key
                      FROM supply_chain_impact_finding_supports AS identity
                      WHERE identity.fact_id = $2
` + supportsEligibleCopySQL("identity") + `))
  ))
)`

// supportsExplainImageFence is the ungranted selector lookup with an image
// reference: the candidates come from the image partial index through a
// MATERIALIZED fetch, the same fence the list read pins, so the planner cannot
// flatten it into a scope-driven join.
const supportsExplainImageFence = `image_supports AS MATERIALIZED (
  SELECT * FROM supply_chain_impact_finding_supports
  WHERE image_ref = $10 AND image_ref <> ''
)`

// ExplainFindingFromWinnersQuery is the ungranted selector and identity explain
// without an image reference, picked from the winners table's winning supports.
var ExplainFindingFromWinnersQuery = mustBuildExplainFromSupportsQuery("winner_picks", supportsExplainWinnersFence, "", "")

// ExplainFindingFromImageQuery is the ungranted selector and identity explain
// with an image reference, driven from the image partial index.
var ExplainFindingFromImageQuery = mustBuildExplainFromSupportsQuery(
	"image_supports", supportsExplainImageFence, "", supportsExplainIdentityPredicate)

// ErrExplanationWinnersUnavailable means an ungranted selector explain was
// refused because the winners table, which names the candidate keys, is not
// materialized or is older than the freshness window. Answering from it would
// read a key with supports but no winners row as not found.
var ErrExplanationWinnersUnavailable = errors.New("supply chain impact winners read model is not fresh enough to explain")

// explainRoute names the query each lookup of one explain call runs.
type explainRoute struct {
	// supports reports that the call reads the per-support table and must pass
	// the readiness probe first.
	supports bool
	// winners reports that the selector lookup names its candidate keys from the
	// winners table, so it needs a fresh winners watermark before it reads.
	winners bool
	// publicID is the query for a call that names a finding_id; selector is the
	// query for every other lookup, and the identity fallback of a public-id miss.
	publicID, selector string
}

// explainRoute decides where one explain call reads. With the winners gate off
// every call keeps the legacy fact read. With it on:
//
//   - a granted call reads supports for both lookups: the winners row is one
//     pick across scopes and cannot answer a grant, and a finding id a granted
//     list returned must explain with the same payload;
//   - an ungranted public-id lookup probes supports by finding id, and a miss
//     falls back to the legacy identity read, because supports has no index for
//     an ungranted fact-id or canonical-key lookup;
//   - an ungranted selector lookup keeps the legacy read: the supports table has
//     no selector index, and the canonical (winners) read it is ruled to take
//     needs its own measured shape.
func (s PostgresFindingStore) explainRoute(filter packet.ExplanationFilter) explainRoute {
	legacy := explainRoute{publicID: ExplainFindingByPublicIDQuery, selector: ExplainFindingQuery}
	if !s.ReadFromWinners {
		return legacy
	}
	if len(filter.AllowedRepositoryIDs) > 0 || len(filter.AllowedScopeIDs) > 0 {
		return explainRoute{supports: true, publicID: ExplainFindingByPublicIDFromSupportsQuery, selector: ExplainFindingFromSupportsQuery}
	}
	// An ungranted call reads supports for the public id, the image index for an
	// image selector, and the winners table's picks for every other selector or
	// identity lookup. No gate-on call keeps the legacy fact read.
	route := explainRoute{supports: true, publicID: ExplainFindingByPublicIDFromSupportsQuery}
	if filter.ImageRef != "" {
		route.selector = ExplainFindingFromImageQuery
		return route
	}
	route.winners = true
	route.selector = ExplainFindingFromWinnersQuery
	return route
}

// requireWinnersFresh refuses an explain that names its candidate keys from the
// winners table while that table is absent, stale, or its probe failed.
func (s PostgresFindingStore) requireWinnersFresh(ctx context.Context) error {
	watermark, err := s.SupplyChainImpactWinnersWatermark(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrExplanationWinnersUnavailable, err)
	}
	if !watermark.Present || SuppressionReadAt(s.Now).Sub(watermark.MaterializedAt.UTC()) > WinnersFreshnessWindow {
		return ErrExplanationWinnersUnavailable
	}
	return nil
}
