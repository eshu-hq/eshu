// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// TestSupplyChainReadAdmissionRoutesExplanationsThroughWinnersReadModel proves
// the #11535 explain miss at the D6 admission seam: the findings port served
// the maintained winners table while the explanation port stayed on the
// legacy fact read, which the C2 write stop left empty, so every digest-keyed
// explain answered not-found. The admission must carry the winners
// read-model flag to the explanation store exactly as it does to the
// findings store.
func TestSupplyChainReadAdmissionRoutesExplanationsThroughWinnersReadModel(t *testing.T) {
	t.Parallel()

	admission := NewSupplyChainReadAdmissionWithReadStore(errSupplyWireShapeStore{err: errSupplyWireShapeProbe}, true, nil)
	if admission == nil {
		t.Fatal("NewSupplyChainReadAdmissionWithReadStore() = nil, want a satisfier bound to the winners read model")
	}
	impl, ok := admission.(*supplyChainReadAdmission)
	if !ok {
		t.Fatalf("admission = %T, want *supplyChainReadAdmission", admission)
	}
	findings, ok := impl.FindingStore.(impact.PostgresFindingStore)
	if !ok {
		t.Fatalf("FindingStore = %T, want impact.PostgresFindingStore", impl.FindingStore)
	}
	if !findings.ReadFromWinners {
		t.Fatal("FindingStore.ReadFromWinners = false, want true for readImpactFromWinners=true")
	}
	explanations, ok := impl.ExplanationStore.(impact.PostgresFindingStore)
	if !ok {
		t.Fatalf("ExplanationStore = %T, want impact.PostgresFindingStore", impl.ExplanationStore)
	}
	if !explanations.ReadFromWinners {
		t.Fatal("ExplanationStore.ReadFromWinners = false, want true for readImpactFromWinners=true: explanations must follow the same read model as findings")
	}
}
