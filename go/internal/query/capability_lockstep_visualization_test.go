// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/visualization"
)

// TestVisualizationPacketDerivationCapabilityLockstep guards the two
// declarations of the packet-derivation capability contract against silent
// drift.
//
// contract_capability_matrix.go (root, #6642 Part C, off-limits to this
// move) carries its own literal row for visualization.packet_derivation
// rather than calling visualization.PacketDerivationSupport(), which the
// leaf's TestMain registers for the leaf's own handler tests. Until Part C
// adopts the constructor, nothing else stops the two declarations from
// drifting apart, so this test asserts they agree field by field: mutate any
// ceiling in either declaration and this test fails.
func TestVisualizationPacketDerivationCapabilityLockstep(t *testing.T) {
	root, ok := capabilityMatrix[visualization.PacketDerivationCapability]
	if !ok {
		t.Fatalf("capabilityMatrix has no entry for %q", visualization.PacketDerivationCapability)
	}
	leaf := visualization.PacketDerivationSupport()

	assertTruthLevel := func(name string, root, leaf *querycontract.TruthLevel) {
		t.Helper()
		switch {
		case root == nil && leaf == nil:
			return
		case root == nil || leaf == nil:
			t.Errorf("%s: root=%v leaf=%v (one is nil)", name, root, leaf)
		case *root != *leaf:
			t.Errorf("%s: root=%q leaf=%q", name, *root, *leaf)
		}
	}

	assertTruthLevel("LocalLightweightMax", root.LocalLightweightMax, leaf.LocalLightweightMax)
	assertTruthLevel("LocalAuthoritativeMax", root.LocalAuthoritativeMax, leaf.LocalAuthoritativeMax)
	assertTruthLevel("LocalFullStackMax", root.LocalFullStackMax, leaf.LocalFullStackMax)
	assertTruthLevel("ProductionMax", root.ProductionMax, leaf.ProductionMax)

	if root.RequiredProfile != leaf.RequiredProfile {
		t.Errorf("RequiredProfile: root=%q leaf=%q", root.RequiredProfile, leaf.RequiredProfile)
	}
}
