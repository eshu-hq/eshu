// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "strings"

// supplyChainImpactVolatileStripSQL strips from a support payload the keys the
// winners store strips from a winner payload (the 2026-11-21 digest-exclusion
// ruling plus the #11598 EPSS score-date exclusion), so a branch B row reads
// like a branch A row. The date strip is a no-op on real payloads (the date
// lives nested in provenance); it is kept so the strip set matches the
// winners store textually.
func supplyChainImpactVolatileStripSQL(payloadExpr string) string {
	return "((" + payloadExpr + " - 'intent_id' - 'cause' - 'generation_id' - 'selected_epss_score_date') #- '{provenance,content_digest}')::jsonb"
}

// listSupplementBinds is the list read's parameter numbering ($2.. key filters,
// $9-$11 runtime filters, $12 severity, $14 priority bucket, $15 minimum score,
// $20 suppression state, $21 include suppressed, $24 read clock).
var listSupplementBinds = supplementBinds{
	cve: "$2", pkg: "$3", repo: "$4", digest: "$5", status: "$6", advisory: "$7", ecosystem: "$8",
	service: "$9", workload: "$10", environment: "$11",
	severity: "$12", bucket: "$14", minScore: "$15", suppState: "$20", includeSuppressed: "$21", readAt: "$24",
}

// ListFindingsFromWinnersSupplementQuery is the unscoped profile=precise list read
// of route V: the shipped winners list read (branch A, the precise filter on the
// winner row) plus the supplement (branch B, supplement_sql.go) in one statement.
// It takes the same 30 parameters as ListFindingsFromWinnersQuery; the profile
// parameter ($13) is unused because the route fixes the profile to precise, and
// $16 (image_ref) is unused because an image_ref call takes the supports image
// read. It is derived from the shipped text, so the winners read cannot drift
// from it unseen; a marker that no longer matches fails package initialization.
var ListFindingsFromWinnersSupplementQuery = mustDeriveSupplementList(ListFindingsFromWinnersQuery)

func mustDeriveSupplementList(winners string) string {
	query, err := deriveSupplementList(winners)
	if err != nil {
		panic(err)
	}
	return query
}

// supplementProfileBlock cuts the profile predicate between two markers and
// replaces it with the precise predicate on the winner row.
// The profile parameter stays referenced in a trivially true guard, so the statement
// keeps its bound-parameter list and every bound parameter is used.
func supplementProfileBlock(text, startMarker, endMarker, alias, profileParam string) (string, error) {
	start := strings.Index(text, startMarker)
	if start < 0 {
		return "", &derivationError{what: "profile start"}
	}
	end := strings.Index(text[start:], endMarker)
	if end < 0 {
		return "", &derivationError{what: "profile end"}
	}
	return text[:start] + "      AND " + supplyChainImpactPrecisePredicateSQL(alias) + "\n      AND " + profileParam + "::text IS NOT NULL\n" + text[start+end:], nil
}

func deriveSupplementList(winners string) (string, error) {
	query, err := supplementProfileBlock(winners, "      AND (\n            $13 = ''\n", "      AND ($14 = '' OR w.priority_bucket = $14)", "w", "$13")
	if err != nil {
		return "", err
	}
	if query, err = replaceOnce(query, "filtered AS NOT MATERIALIZED (\n    SELECT w.finding_id,", "branch_a AS NOT MATERIALIZED (\n    SELECT w.finding_id,", "filtered CTE"); err != nil {
		return "", err
	}
	if query, err = replaceOnce(query, " AS effective_suppression_state\n    FROM supply_chain_impact_canonical_winners AS w",
		" AS effective_suppression_state,\n           NULL::text AS pick_fact_id,\n           NULL::text AS override_fact_id\n    FROM supply_chain_impact_canonical_winners AS w", "filtered select list"); err != nil {
		return "", err
	}
	union := `)` + supplementCTEs(listSupplementBinds) + `,
filtered AS NOT MATERIALIZED (
  SELECT finding_id, canonical_key, winner_fact_id, priority_score, effective_suppression_state, pick_fact_id, override_fact_id
  FROM branch_a
  UNION ALL
  SELECT finding_id, canonical_key, fact_id, priority_score, effective_suppression_state, fact_id, override_fact_id
  FROM branch_b
),
paged AS MATERIALIZED (`
	if query, err = replaceOnce(query, "),\npaged AS MATERIALIZED (", union, "paged CTE"); err != nil {
		return "", err
	}
	finalOld := "SELECT page.finding_id,\n       win.source_confidence,\n       " + supplyChainImpactPayloadWithEffectiveSuppressionSQL("win.payload", "page.effective_suppression_state") +
		" AS payload\nFROM paged AS page\nJOIN supply_chain_impact_canonical_winners AS win\n  ON win.canonical_key = page.canonical_key\n"
	finalNew := "SELECT page.finding_id,\n       COALESCE(win.source_confidence, pick.source_confidence) AS source_confidence,\n       CASE WHEN page.pick_fact_id IS NULL THEN " +
		supplyChainImpactPayloadWithEffectiveSuppressionSQL("win.payload", "page.effective_suppression_state") + " ELSE " +
		supplyChainImpactPayloadWithSuppressionOverlaySQL(supplyChainImpactVolatileStripSQL("pick.payload"), "override.payload", "page.effective_suppression_state") +
		" END AS payload\nFROM paged AS page\nLEFT JOIN supply_chain_impact_canonical_winners AS win\n  ON page.pick_fact_id IS NULL AND win.canonical_key = page.canonical_key\n" +
		"LEFT JOIN supply_chain_impact_finding_supports AS pick\n  ON pick.fact_id = page.pick_fact_id\nLEFT JOIN supply_chain_impact_finding_supports AS override\n  ON override.fact_id = page.override_fact_id\n"
	return replaceOnce(query, finalOld, finalNew, "payload join")
}
