// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"strings"
)

// supportsListSourceCTE replaces the legacy source_candidates and
// operator_candidates CTEs. The grant legs come first, as two index arms of one
// UNION ALL, and the active-generation join is applied to the granted supports
// afterward (raw-first); the second arm excludes the scopes the first already
// covers, so a support matched by both grants appears once. The static-authority
// guard stays: the supports table mirrors every live finding fact, and the
// scanner classification rows decide which are source truth. $22 and $23 are the
// allowed repository and scope ids, as in the legacy bind order.
const supportsListSourceCTE = `
source_candidates AS (
  SELECT fact.fact_id, fact.scope_id, fact.finding_id, fact.source_confidence,
         fact.suppression_state, fact.priority_score,
         fact.has_payload_finding_id, fact.canonical_key,
         fact.severity_bucket, fact.priority_bucket
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
    AND ($16 = '' OR fact.image_ref = $16)
)`

// supportsListOverrideLateral replaces the legacy operator_overrides CTE
// with an indexed probe over the partial operator index, one probe per surviving
// source winner (the legacy CTE re-ran its pick per winner).
const supportsListOverrideLateral = `LEFT JOIN LATERAL (
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

// supportsListRankFilter is the rank predicate the read applies to the
// picked row, ahead of the suppression filters. $12, $14 and $15 keep the legacy
// bind order.
const supportsListRankFilter = `  WHERE ($12 = '' OR severity_bucket = $12)
    AND ($14 = '' OR priority_bucket = $14)
    AND ($15 = 0 OR priority_score >= $15)
    AND ($20 = '' OR effective_suppression_state = $20)`

// ListFindingsFromSupportsQuery is the granted list read of #10880 PR-B2a: the
// legacy ListFindingsQuery with its fact_records reads swapped for reads of
// supply_chain_impact_finding_supports. PR-B1 built it test-only and proved it
// against the legacy read and the theory proof's plan shape
// (docs/internal/design/10880-pr-b1-supports-read-theory-proof.md); PR-B2a
// serves it. It takes the SAME 30-parameter slice as ListFindingsQuery, so the
// store swaps queries without rebuilding args.
//
// It implements the ruled rank-filter order (arbiter ruling for #10880 PR-B1,
// Q2): the pick runs over the granted supports that pass the key and existence
// filters, and the rank filters (severity, priority bucket, minimum priority
// score) then apply to the picked row, before the suppression filters. The
// legacy read filters first and picks after, so rank cases differ from it on
// purpose and are compared with an independent oracle instead.
//
// It is derived from the shipped constant, not copied: the text before the
// source candidates, the sources' winner pick, the suppression overlay, the
// paging, and the payload overlay are the legacy text unchanged, so the legacy
// read cannot drift from the candidate unseen. Each replaced block is located by
// an explicit marker, and a marker that no longer matches fails the derivation.
// A marker that no longer matches the legacy text is a build-time defect: package
// initialization panics, and TestSupportsListQueryDerivationFailsOnALostMarker
// pins each marker.
var ListFindingsFromSupportsQuery = mustDeriveSupportsListQuery(ListFindingsQuery)

func mustDeriveSupportsListQuery(legacy string) string {
	query, err := deriveSupportsListQuery(legacy)
	if err != nil {
		panic(err)
	}
	return query
}

// deriveSupportsListQuery swaps each fact_records block of the legacy list query
// for its supports equivalent. Each replaced block is located by an explicit
// marker; a marker that no longer matches returns an error naming it.
func deriveSupportsListQuery(legacy string) (string, error) {
	cut := func(text, from, to string) (before, after string, err error) {
		start := strings.Index(text, from)
		if start < 0 {
			return "", "", fmt.Errorf("legacy list query lost the marker %q: update the supports derivation", from)
		}
		end := strings.Index(text[start:], to)
		if end < 0 {
			return "", "", fmt.Errorf("legacy list query lost the marker %q after %q: update the supports derivation", to, from)
		}
		return text[:start], text[start+end:], nil
	}
	// source_candidates and operator_candidates become the supports source CTE.
	head, rest, err := cut(legacy, "\nsource_candidates AS (", "\nsource_winners AS (")
	if err != nil {
		return "", err
	}
	query := head + supportsListSourceCTE + "," + rest
	// The legacy operator_overrides CTE and its join become the lateral probe.
	head, rest, err = cut(query, "operator_overrides AS (", "joined_winners AS (")
	if err != nil {
		return "", err
	}
	query = head + rest
	const overrideJoin = "LEFT JOIN operator_overrides AS override\n    ON override.canonical_key = source.canonical_key"
	if !strings.Contains(query, overrideJoin) {
		return "", fmt.Errorf("legacy list query lost the operator_overrides join: update the supports derivation")
	}
	query = strings.Replace(query, overrideJoin, supportsListOverrideLateral, 1)
	// The rank filters apply to the picked row, ahead of the suppression filters.
	const suppressionFilter = "  WHERE ($20 = '' OR effective_suppression_state = $20)"
	if !strings.Contains(query, suppressionFilter) {
		return "", fmt.Errorf("legacy list query lost the canonical_facts filter: update the supports derivation")
	}
	query = strings.Replace(query, suppressionFilter, supportsListRankFilter, 1)
	// The page and the override payload come from the supports table.
	const sourceJoin = "JOIN fact_records AS source\n  ON source.fact_id = page.fact_id\nLEFT JOIN fact_records AS override\n  ON override.fact_id = page.override_fact_id"
	if !strings.Contains(query, sourceJoin) {
		return "", fmt.Errorf("legacy list query lost the payload joins: update the supports derivation")
	}
	return strings.Replace(query, sourceJoin,
		"JOIN supply_chain_impact_finding_supports AS source\n  ON source.fact_id = page.fact_id\nLEFT JOIN supply_chain_impact_finding_supports AS override\n  ON override.fact_id = page.override_fact_id", 1), nil
}
