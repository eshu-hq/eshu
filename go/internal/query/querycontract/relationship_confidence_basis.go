// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "strings"

const (
	relationshipConfidenceBasisAssertionOverride = "assertion_override"
	relationshipConfidenceBasisEvidenceAggregate = "evidence_aggregate"
	relationshipConfidenceBasisEvidenceConstant  = "evidence_constant"
)

// AddRelationshipConfidenceBasis adds a comparable correlation confidence
// basis without changing the stored confidence or graph/query truth source.
func AddRelationshipConfidenceBasis(row map[string]any) {
	if len(row) == 0 || strings.TrimSpace(StringVal(row, "confidence_basis")) != "" {
		return
	}
	if basis := RelationshipConfidenceBasis(row); basis != "" {
		row["confidence_basis"] = basis
	}
}

// RelationshipConfidenceBasis names what a correlation row's confidence rests
// on, so two rows with the same score can still be told apart. It reports "" for
// a row with no positive confidence, because a basis for a score that does not
// exist would read as evidence the caller does not have.
//
// Precedence is deliberate. An explicit assertion outranks accumulated
// evidence: a human or policy that asserted the relationship is a stronger claim
// than any number of inferred observations, so a row whose resolution_source is
// "assertion" reports assertion_override even when evidence is also present.
// Below that, more than one observation is an aggregate; a single observation,
// or an evidence type or kind list with no count, is a constant.
func RelationshipConfidenceBasis(row map[string]any) string {
	if FloatVal(row, "confidence") <= 0 {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(StringVal(row, "resolution_source")), "assertion") {
		return relationshipConfidenceBasisAssertionOverride
	}
	evidenceCount := IntVal(row, "evidence_count")
	if evidenceCount > 1 {
		return relationshipConfidenceBasisEvidenceAggregate
	}
	if evidenceCount == 1 ||
		strings.TrimSpace(StringVal(row, "evidence_type")) != "" ||
		len(StringSliceVal(row, "evidence_kinds")) > 0 {
		return relationshipConfidenceBasisEvidenceConstant
	}
	return ""
}
