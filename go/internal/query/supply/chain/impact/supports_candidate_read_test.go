// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"
)

// The candidate list read of #10880 PR-B1: the legacy ListFindingsQuery with
// its fact_records reads swapped for reads of supply_chain_impact_finding_supports.
// It is test-only. PR-B2 ships the real read; this one proves the supports table
// can answer what the legacy read answers, and pins the plan shape the
// theory proofs measured (docs/internal/design/10880-pr-b1-supports-read-theory-proof.md).
//
// It is derived from the shipped constant, not copied: the text before the
// source candidates, the sources' winner pick, the suppression overlay, the
// paging, and the payload overlay are the legacy text unchanged, so the legacy
// read cannot drift from the candidate unseen. Each replaced block is located by
// an explicit marker, and a marker that no longer matches fails the derivation.

// supportsCandidateSourceCTE replaces the legacy source_candidates and
// operator_candidates CTEs. The grant legs come first, as two index arms of one
// UNION ALL, and the active-generation join is applied to the granted supports
// afterward (raw-first); the second arm excludes the scopes the first already
// covers, so a support matched by both grants appears once. The static-authority
// guard stays: the supports table mirrors every live finding fact, and the
// scanner classification rows decide which are source truth. $22 and $23 are the
// allowed repository and scope ids, as in the legacy bind order.
const supportsCandidateSourceCTE = `
source_candidates AS (
  SELECT fact.fact_id, fact.scope_id, fact.finding_id, fact.source_confidence,
         fact.suppression_state, fact.priority_score,
         fact.has_payload_finding_id, fact.canonical_key
  FROM (SELECT * FROM supply_chain_impact_finding_supports WHERE scope_id = ANY($23::text[])
        UNION ALL
        SELECT * FROM supply_chain_impact_finding_supports WHERE repository_id = ANY($22::text[])
          AND NOT COALESCE(scope_id = ANY($23::text[]), FALSE)) AS fact
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
    AND fact.scope_id <> 'operator:vulnerability_suppressions'
    AND $25::boolean IS NOT NULL AND $26::text IS NOT NULL AND $27::text IS NOT NULL
    AND $28::text IS NOT NULL AND $29::text IS NOT NULL AND $30::text[] IS NOT NULL
    AND finding_association.finding_fact_id IS NULL
    AND fact.scope_id NOT LIKE 'image://derived/%'
    AND (finding_class.finding_fact_id IS NULL
         OR (finding_class.source_class = 'static' AND finding_class.lifecycle = 'active'))
    AND ($2 = '' OR fact.cve_id = $2)
    AND ($3 = '' OR fact.package_id = $3)
    AND ($4 = '' OR fact.repository_id = $4)
    AND ($5 = '' OR fact.subject_digest = $5)
    AND ($6 = '' OR fact.impact_status = $6)
    AND ($7 = '' OR fact.advisory_id = $7)
    AND ($8 = '' OR fact.ecosystem = LOWER(($8) COLLATE "C") COLLATE "default")
    AND ($12 = '' OR fact.severity_bucket = $12)
    AND (
          $13 = ''
          OR fact.detection_profile = $13
          OR ($13 = 'comprehensive' AND fact.detection_profile = '')
          OR (
                $13 = 'precise'
                AND fact.detection_profile = ''
                AND fact.impact_status IN ('affected_exact', 'not_affected_known_fixed')
                AND fact.observed_version <> ''
                AND fact.match_reason IN (
                      'npm_semver_affected_range', 'npm_semver_known_fixed',
                      'nuget_semver_affected_range', 'nuget_semver_known_fixed',
                      'cargo_semver_affected_range', 'cargo_semver_known_fixed',
                      'hex_semver_affected_range', 'hex_semver_known_fixed',
                      'maven_range_match', 'maven_known_fixed',
                      'swift_semver_affected_range', 'swift_semver_known_fixed'
                    )
             )
        )
    AND ($14 = '' OR fact.priority_bucket = $14)
    AND ($15 = 0 OR fact.priority_score >= $15)
    AND ($16 = '' OR fact.image_ref = $16)
)`

// supportsCandidateOverrideLateral replaces the legacy operator_overrides CTE
// with an indexed probe over the partial operator index, one probe per surviving
// source winner (the legacy CTE re-ran its pick per winner).
const supportsCandidateOverrideLateral = `LEFT JOIN LATERAL (
    SELECT fact.fact_id, fact.suppression_state,
           fact.payload #>> '{suppression,expires_at}' AS expires_at
    FROM supply_chain_impact_finding_supports AS fact
    WHERE fact.scope_id = 'operator:vulnerability_suppressions'
      AND fact.canonical_key = source.canonical_key
      AND fact.suppression_state <> 'active'
      AND fact.generation_id = (
        SELECT g.generation_id FROM ingestion_scopes AS s
        JOIN scope_generations AS g ON g.scope_id = s.scope_id AND g.generation_id = s.active_generation_id
        WHERE s.scope_id = 'operator:vulnerability_suppressions' AND g.status = 'active')
    ORDER BY fact.priority_score DESC, fact.has_payload_finding_id DESC, fact.fact_id ASC
    LIMIT 1
  ) AS override ON TRUE`

// supportsCandidateListQuery derives the candidate from the legacy list query.
func supportsCandidateListQuery(t testing.TB) string {
	t.Helper()
	query := ListFindingsQuery
	cut := func(text, from, to string) (before, after string) {
		t.Helper()
		start := strings.Index(text, from)
		if start < 0 {
			t.Fatalf("legacy list query lost the marker %q: update the candidate derivation", from)
		}
		end := strings.Index(text[start:], to)
		if end < 0 {
			t.Fatalf("legacy list query lost the marker %q after %q: update the candidate derivation", to, from)
		}
		return text[:start], text[start+end:]
	}
	// source_candidates and operator_candidates become the supports source CTE.
	head, rest := cut(query, "\nsource_candidates AS (", "\nsource_winners AS (")
	query = head + supportsCandidateSourceCTE + "," + rest
	// The legacy operator_overrides CTE and its join become the lateral probe.
	head, rest = cut(query, "operator_overrides AS (", "joined_winners AS (")
	query = head + rest
	const overrideJoin = "LEFT JOIN operator_overrides AS override\n    ON override.canonical_key = source.canonical_key"
	if !strings.Contains(query, overrideJoin) {
		t.Fatalf("legacy list query lost the operator_overrides join: update the candidate derivation")
	}
	query = strings.Replace(query, overrideJoin, supportsCandidateOverrideLateral, 1)
	// The page and the override payload come from the supports table.
	const sourceJoin = "JOIN fact_records AS source\n  ON source.fact_id = page.fact_id\nLEFT JOIN fact_records AS override\n  ON override.fact_id = page.override_fact_id"
	if !strings.Contains(query, sourceJoin) {
		t.Fatalf("legacy list query lost the payload joins: update the candidate derivation")
	}
	return strings.Replace(query, sourceJoin,
		"JOIN supply_chain_impact_finding_supports AS source\n  ON source.fact_id = page.fact_id\nLEFT JOIN supply_chain_impact_finding_supports AS override\n  ON override.fact_id = page.override_fact_id", 1)
}
