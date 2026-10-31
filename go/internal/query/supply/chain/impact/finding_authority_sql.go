// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "strings"

const supplyChainImpactProtectedModeMarker = "__PROTECTED_READS__"

func supplyChainImpactReadModeSQL(query string, protected bool) string {
	mode := "FALSE"
	if protected {
		mode = "TRUE"
	}
	return strings.ReplaceAll(query, supplyChainImpactProtectedModeMarker, mode)
}

// Protected classifications survive result retraction. A missing association
// never turns a known derived finding into a direct static finding.
const supplyChainImpactProtectedSourceJoinsSQL = `
  LEFT JOIN scanner_image_result_fact_classifications AS finding_class
    ON finding_class.finding_fact_id = fact.fact_id
  LEFT JOIN scanner_image_result_fact_associations AS finding_association
    ON finding_association.finding_fact_id = fact.fact_id`

// The derived branch remains closed until advisory dependencies, fenced
// reuse eligibility and caller visibility join the current source vector in
// this same statement. The protected association is identity, not authority.
func supplyChainImpactProtectedSourcePredicateSQL(
	scoped, tenant, workspace, subjectClass, policy, allowedScopes, filterScopes, readAt string,
) string {
	return `
    AND (
      (((NOT __PROTECTED_READS__)
        OR (finding_class.source_class = 'static' AND finding_class.lifecycle = 'active'
            AND ` + supplyChainImpactCurrentStaticAuthoritySQL(readAt) + `))
       AND finding_association.finding_fact_id IS NULL
       AND fact.scope_id NOT LIKE 'image://derived/%'
       AND (finding_class.finding_fact_id IS NULL
            OR (finding_class.source_class = 'static' AND finding_class.lifecycle = 'active')))
      OR (finding_class.source_class = 'derived'
          AND finding_class.lifecycle = 'active'
          AND finding_association.finding_fact_id IS NOT NULL
          AND ` + supplyChainImpactCurrentSourceVectorForCallerSQL(
		scoped, tenant, workspace, subjectClass, policy, allowedScopes, filterScopes, readAt,
	) + `
          AND FALSE)
    )`
}

// The protected target partitions otherwise identical derived findings before
// winner selection. Direct static findings keep their existing canonical key.
const supplyChainImpactProtectedSourceCanonicalKeySQL = `CASE
    WHEN finding_class.source_class = 'derived'
    THEN ` + CanonicalFindingKeySQL + ` || '|' || finding_association.scan_scope_id
    ELSE ` + CanonicalFindingKeySQL + `
  END`

// Materialized winners are reserved for static findings. The maintainer must
// apply this classification before ranking so a retained derived winner cannot
// hide a valid static runner-up.
const supplyChainImpactStaticWinnerPredicateSQL = `
    AND NOT EXISTS (
      SELECT 1 FROM scanner_image_result_fact_associations AS finding_association
      WHERE finding_association.finding_fact_id = w.winner_fact_id
    )
    AND (NOT __PROTECTED_READS__ OR EXISTS (
      SELECT 1 FROM scanner_image_result_fact_classifications AS finding_class
      WHERE finding_class.finding_fact_id = w.winner_fact_id
        AND finding_class.source_class = 'static' AND finding_class.lifecycle = 'active'
    ))
    AND w.winner_scope_id NOT LIKE 'image://derived/%'`
