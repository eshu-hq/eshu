// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/semantic"
)

// TestSemanticEvidenceTruthBasis pins the truth_basis derivation behind the
// shared SemanticEvidenceRow schema: both known fact kinds map to their
// documented enum values, and an unknown kind falls through to the
// semantic_evidence default arm (unreachable on current filters, but part of
// the owner's contract and the documented enum).
func TestSemanticEvidenceTruthBasis(t *testing.T) {
	t.Parallel()

	if got := semanticEvidenceTruthBasis(semantic.SemanticDocumentationObservationFactKind); got != "semantic_observation" {
		t.Errorf("documentation observation basis = %q, want %q", got, "semantic_observation")
	}
	if got := semanticEvidenceTruthBasis(semantic.SemanticCodeHintFactKind); got != "code_hint" {
		t.Errorf("code hint basis = %q, want %q", got, "code_hint")
	}
	if got := semanticEvidenceTruthBasis("bogus.kind"); got != "semantic_evidence" {
		t.Errorf("unknown kind basis = %q, want %q (default arm)", got, "semantic_evidence")
	}
}
