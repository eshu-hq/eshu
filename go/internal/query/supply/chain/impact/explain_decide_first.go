// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"strings"
)

// The ungranted selector explain decides "none, one, or ambiguous" before it computes any
// winner (#10880 PR-C1 f, arbiter ruling item-f-time 2026-11-24). The route answers at most one
// finding: a selector that matches two canonical findings is a refusal that carries only the
// count (ErrExplanationAmbiguous, impact/explain_postgres.go), so computing every winner of a
// broad selector to return that refusal is the waste: the first key-first statement spent about
// 70 microseconds per candidate (scope, generation and classification probes, then a sort of every
// candidate with its payload) on a CVE with 140,292 candidates and 679 canonical findings.
//
// The statement streams the support rows of the route's own key and stops as early as the answer
// allows. Every stage reads lazily (a MATERIALIZED CTE fills its tuplestore on demand, so a LIMIT
// above it stops the scan underneath it):
//
//  1. key_supports: the narrow tuples (no payload) of the supports that match the route's key,
//     read through that key's partial btree, with every selector, grant, runtime-filter and
//     column-only authority predicate. Nothing blocks inside it: no sort, no aggregate.
//  2. first_hit: the first row, in index order, whose identity predicate holds and whose authority
//     probe (active scope and generation, no scanner association, static classification) passes.
//  3. second_hit: the first such row whose canonical key DIFFERS from first_hit's. The cheap column
//     compare runs on the scan node, before the authority probe, so the probe runs only on
//     candidates of a canonical key not yet found. Two canonical keys decide "ambiguous" and the
//     scan stops there.
//  4. Ambiguous: the two hit rows are returned with their payload fetched by primary key; the store
//     counts the rows and answers the refusal, which carries only that count. No winner is computed.
//  5. Exactly one or none: when no second key exists the scan has run to its end. The one key's
//     copies are ranked as the winner CTEs rank them (priority score, payload finding id, fact id) and
//     probed in that order until the first authorized copy, which is the winner's source row: the
//     canonical winner CTEs of every other explain read keep only the best-ranked authorized copy of a
//     key, so the lower-ranked copies cannot change the answer and are never probed. The winner
//     CTEs then run over that one row, its payload fetched by primary key.
//
// The authority probe is a LATERAL subquery with a LIMIT, which the planner cannot pull up into a
// join it orders from a row estimate: it runs once per candidate row that reaches it, as a nested
// loop, whatever the estimates say. A key whose copies all fail the authority probe contributes
// nothing to the decision, and a key whose best-ranked copy fails it is answered by its next
// authorized copy, exactly as the legacy statement answers them.

// explainDecideFirstNarrowColumns are the columns the key fence carries; the payload,
// the confidence and the suppression state are fetched by primary key, for the rows that need them.
const explainDecideFirstNarrowColumns = `fact.fact_id, fact.canonical_key, fact.scope_id, fact.generation_id, fact.finding_id,
         fact.priority_score, fact.has_payload_finding_id`

// explainAuthorityLateral is the legacy statement's per-candidate authority, probed one candidate at
// a time: the active scope and generation, no scanner association, and a static, active scanner
// classification when there is one. The candidate alias is "fact". The LIMIT keeps the planner from
// turning the probe into a join it orders from a row estimate.
const explainAuthorityLateral = `  CROSS JOIN LATERAL (
    SELECT 1 AS authorized
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS generation
      ON generation.scope_id = scope.scope_id
     AND generation.generation_id = scope.active_generation_id
    LEFT JOIN scanner_image_result_fact_classifications AS finding_class
      ON finding_class.finding_fact_id = fact.fact_id
    LEFT JOIN scanner_image_result_fact_associations AS finding_association
      ON finding_association.finding_fact_id = fact.fact_id
    WHERE scope.scope_id = fact.scope_id
      AND scope.active_generation_id = fact.generation_id
      AND generation.status = 'active'
      AND finding_association.finding_fact_id IS NULL
      AND (finding_class.finding_fact_id IS NULL
           OR (finding_class.source_class = 'static' AND finding_class.lifecycle = 'active'))
    LIMIT 1
  ) AS authority`

// explainIdentityPredicate is the identity lookup of the other explain reads (a fact id, a finding id,
// a canonical key, or the canonical key of the fact the identity names), on a row of alias fact.
const explainIdentityPredicate = `($2 = ''
        OR fact.fact_id = $2
        OR fact.finding_id = $2
        OR fact.canonical_key = $2
        OR fact.canonical_key IN (SELECT identity_key.canonical_key FROM identity_keys AS identity_key))`

// explainCandidateColumnPredicates are the selector, grant, runtime-filter, image, operator-scope and
// derived-scope predicates that need only the support's own columns (alias fact), exactly as the
// other explain reads apply them to their candidates. They narrow the key fence so no later stage sees
// a row the legacy statement would have dropped before its pick.
func explainCandidateColumnPredicates() string {
	return `    AND fact.scope_id <> '` + supplyChainImpactOperatorSuppressionScopeID + `'
    AND fact.scope_id NOT LIKE 'image://derived/%'
    AND $14::boolean IS NOT NULL AND $15::text IS NOT NULL AND $16::text IS NOT NULL
    AND $17::text IS NOT NULL AND $18::text IS NOT NULL AND $19::text[] IS NOT NULL
    AND ($3 = '' OR fact.advisory_id = $3 OR fact.cve_id = $3)
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
    )`
}

// explainKeyLegs are the index arms of each driving key: each arm is a predicate on alias fact whose
// `<> ”` literal repeats the partial index predicate so a generic plan can prove the index applies.
// The advisory-or-CVE argument ($3) is two arms, the second excluding the rows the first covers.
var explainKeyLegs = map[explainDrivingKey][]string{
	explainKeyCVE:      {"fact.cve_id = $4 AND fact.cve_id <> ''"},
	explainKeyAdvisory: {"fact.advisory_id = $3 AND fact.advisory_id <> ''", "fact.cve_id = $3 AND fact.cve_id <> '' AND fact.advisory_id <> $3"},
}

// best_copy takes the first authorized row of ranked_copies and carries no ORDER BY of its
// own: it relies on the lateral nested loop reading its outer rows in the order the
// MATERIALIZED ranked_copies CTE stored them (a LATERAL subquery with a LIMIT cannot be
// reordered, and a CTE scan returns its tuplestore in order). The live differential pins the
// dependency: the "ranked copies in reverse" and tie-break seeded violations change the answer.
// buildExplainDecideFirstQuery composes the decide-first ungranted explain for one driving key.
func buildExplainDecideFirstQuery(key explainDrivingKey) (string, error) {
	arms, ok := explainKeyLegs[key]
	if !ok {
		return "", fmt.Errorf("no index legs for the driving key %q", key)
	}
	winners, err := deriveSupportsExplainWinners(supplyChainImpactSplitCanonicalWinnerCTEs("$13::timestamptz"))
	if err != nil {
		return "", err
	}
	var fence []string
	for _, arm := range arms {
		fence = append(fence, `  SELECT `+explainDecideFirstNarrowColumns+`
  FROM supply_chain_impact_finding_supports AS fact
  WHERE $1::text IS NOT NULL
    AND `+arm+`
`+explainCandidateColumnPredicates())
	}
	return `
WITH ` + supplyChainImpactRuntimeFilterCTE("$9", "$8", "''", "$11", "$12") + `,
key_supports AS MATERIALIZED (
` + strings.Join(fence, "  UNION ALL\n") + `
),
identity_keys AS (
  SELECT fact.canonical_key
  FROM supply_chain_impact_finding_supports AS fact
` + explainAuthorityLateral + `
  WHERE $2 <> ''
    AND fact.fact_id = $2
` + explainCandidateColumnPredicates() + `
),
first_hit AS MATERIALIZED (
  SELECT fact.fact_id, fact.canonical_key
  FROM key_supports AS fact
` + explainAuthorityLateral + `
  WHERE ` + explainIdentityPredicate + `
  LIMIT 1
),
second_hit AS MATERIALIZED (
  SELECT fact.fact_id, fact.canonical_key
  FROM key_supports AS fact
` + explainAuthorityLateral + `
  WHERE fact.canonical_key <> (SELECT first_key.canonical_key FROM first_hit AS first_key)
    AND ` + explainIdentityPredicate + `
  LIMIT 1
),
ambiguous_rows AS (
  SELECT support.finding_id, support.source_confidence, support.payload
  FROM (
    SELECT hit.fact_id FROM first_hit AS hit WHERE EXISTS (SELECT 1 FROM second_hit)
    UNION ALL
    SELECT hit.fact_id FROM second_hit AS hit
  ) AS decided
  JOIN supply_chain_impact_finding_supports AS support ON support.fact_id = decided.fact_id
),
ranked_copies AS MATERIALIZED (
  SELECT fact.fact_id, fact.scope_id, fact.generation_id
  FROM key_supports AS fact
  WHERE NOT EXISTS (SELECT 1 FROM second_hit)
    AND fact.canonical_key = (SELECT first_key.canonical_key FROM first_hit AS first_key)
    AND ` + explainIdentityPredicate + `
  ORDER BY fact.priority_score DESC, fact.has_payload_finding_id DESC, fact.fact_id ASC
),
best_copy AS MATERIALIZED (
  SELECT fact.fact_id
  FROM ranked_copies AS fact
` + explainAuthorityLateral + `
  LIMIT 1
),
source_candidates AS MATERIALIZED (
  SELECT support.fact_id, support.scope_id, support.finding_id, support.source_confidence, support.payload,
         support.suppression_state, support.priority_score, support.has_payload_finding_id, support.canonical_key
  FROM best_copy
  JOIN supply_chain_impact_finding_supports AS support ON support.fact_id = best_copy.fact_id
),` + winners + `,
canonical_facts AS (
  SELECT finding_id, source_confidence, payload
  FROM canonical_winners
  UNION ALL
  SELECT finding_id, source_confidence, payload
  FROM ambiguous_rows
)
SELECT finding_id, source_confidence, payload
FROM canonical_facts
ORDER BY finding_id ASC
LIMIT 2
`, nil
}

func mustBuildExplainDecideFirstQuery(key explainDrivingKey) string {
	query, err := buildExplainDecideFirstQuery(key)
	if err != nil {
		panic(err)
	}
	return supplyChainImpactReadModeSQL(query, false)
}
