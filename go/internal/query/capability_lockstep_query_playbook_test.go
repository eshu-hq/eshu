// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/playbook"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestQueryPlaybookCapabilityLockstep guards the two declarations of the
// query-playbook capability contract against silent drift.
//
// contract/capability_matrix.go (the Part C leaf, off-limits to this move)
// still carries its own capabilitySupport literal for
// CapabilityQueryPlaybooks rather than calling playbook.Support() the way
// its repository.ContextOverviewCapability entry calls
// repository.ContextOverviewSupport(). Until a follow-up lane adopts
// playbook.Support() directly, nothing else stops the two declarations from
// drifting apart, so this test asserts they agree field by field: mutate any
// ceiling in either declaration and this test fails.
func TestQueryPlaybookCapabilityLockstep(t *testing.T) {
	root, ok := capabilityMatrix[CapabilityQueryPlaybooks]
	if !ok {
		t.Fatalf("capabilityMatrix has no entry for CapabilityQueryPlaybooks %q", CapabilityQueryPlaybooks)
	}
	leaf := playbook.Support()

	if playbook.Capability != CapabilityQueryPlaybooks {
		t.Errorf("Capability: root=%q leaf=%q", CapabilityQueryPlaybooks, playbook.Capability)
	}

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
