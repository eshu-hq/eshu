// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

const (
	relationshipConfidenceBasisAssertionOverride = "assertion_override"
	relationshipConfidenceBasisEvidenceAggregate = "evidence_aggregate"
	relationshipConfidenceBasisEvidenceConstant  = "evidence_constant"
)

// addRelationshipConfidenceBasis adds a comparable correlation confidence
// basis without changing the stored confidence or graph/query truth source.
func addRelationshipConfidenceBasis(row map[string]any) {
	if len(row) == 0 || strings.TrimSpace(StringVal(row, "confidence_basis")) != "" {
		return
	}
	if basis := relationshipConfidenceBasis(row); basis != "" {
		row["confidence_basis"] = basis
	}
}

func relationshipConfidenceBasis(row map[string]any) string {
	if relationshipFloatVal(row, "confidence") <= 0 {
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
