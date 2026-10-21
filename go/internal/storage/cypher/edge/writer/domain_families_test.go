// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"testing"
)

// TestDomainWritersLiveInEdgeWriterLeaf pins the #6694 home of the five
// root domain edge/evidence writers: reducer constructs them from this leaf
// (see go/cmd/reducer/canonical_graph_writers.go), so every constructor must
// resolve here and every relationship vocabulary must stay non-empty.
func TestDomainFamiliesLiveInEdgeWriterLeaf(t *testing.T) {
	t.Parallel()

	if got := NewCodeInterprocEvidenceWriter(nil, 0); got == nil {
		t.Error("NewCodeInterprocEvidenceWriter returned nil")
	}
	if got := NewCodeTaintEvidenceWriter(nil, 0); got == nil {
		t.Error("NewCodeTaintEvidenceWriter returned nil")
	}
	if got := NewIncidentRoutingEvidenceWriter(nil, 0); got == nil {
		t.Error("NewIncidentRoutingEvidenceWriter returned nil")
	}
	if got := NewKubernetesCorrelationEdgeWriter(nil, 0); got == nil {
		t.Error("NewKubernetesCorrelationEdgeWriter returned nil")
	}
	if got := NewObservabilityCoverageEdgeWriter(nil, 0); got == nil {
		t.Error("NewObservabilityCoverageEdgeWriter returned nil")
	}
	if got := IncidentRoutingEvidenceRelationshipTypes(); len(got) == 0 {
		t.Error("IncidentRoutingEvidenceRelationshipTypes is empty")
	}
	if got := IncidentRoutingEvidenceSlotVocabulary(); len(got) == 0 {
		t.Error("IncidentRoutingEvidenceSlotVocabulary is empty")
	}
	if got := ObservabilityCoverageRelationshipTypes(); len(got) == 0 {
		t.Error("ObservabilityCoverageRelationshipTypes is empty")
	}
}
