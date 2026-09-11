// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/freshness"
)

// TestFreshnessCapabilityLockstep pins root's three literal
// contract_changed_since.go/contract_freshness.go/contract_service_changed_since.go
// `init()` registrations to package freshness's own Support constructors,
// field by field, so the two copies of the same contract can never drift
// apart silently.
//
// Root's contract_* files are owned by a later #6642 lane (Part C) and keep a
// literal copy of these four fields today -- freshness/capabilities.go's
// doc comment names the follow-up that should point root's rows at these
// constructors directly instead of a copy. Until that follow-up lands, this
// test is what catches a one-sided edit: flip one ceiling on either side and
// this test fails (sensitivity proven once by hand during development: a
// deliberate edit to LocalAuthoritativeMax on one side, reverted after
// observing the failure).
func TestFreshnessCapabilityLockstep(t *testing.T) {
	for _, tc := range []struct {
		name        string
		rootConst   string
		leafConst   string
		rootSupport capabilitySupport
		leafSupport capabilitySupport
	}{
		{
			name:        "changed_since",
			rootConst:   freshnessChangedSinceCapability,
			leafConst:   freshness.ChangedSinceCapability,
			rootSupport: capabilityMatrix[freshnessChangedSinceCapability],
			leafSupport: freshness.ChangedSinceSupport(),
		},
		{
			name:        "generation_lifecycle",
			rootConst:   freshnessGenerationLifecycleCapability,
			leafConst:   freshness.GenerationLifecycleCapability,
			rootSupport: capabilityMatrix[freshnessGenerationLifecycleCapability],
			leafSupport: freshness.GenerationLifecycleSupport(),
		},
		{
			name:        "service_changed_since",
			rootConst:   freshnessServiceChangedSinceCapability,
			leafConst:   freshness.ServiceChangedSinceCapability,
			rootSupport: capabilityMatrix[freshnessServiceChangedSinceCapability],
			leafSupport: freshness.ServiceChangedSinceSupport(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.rootConst != tc.leafConst {
				t.Fatalf("capability string = %q (root) vs %q (freshness); the two copies of the same contract must spell the identical capability id", tc.rootConst, tc.leafConst)
			}
			if !truthLevelPtrEqual(tc.rootSupport.LocalLightweightMax, tc.leafSupport.LocalLightweightMax) {
				t.Fatalf("%s: LocalLightweightMax = %v (root) vs %v (freshness)", tc.name, tc.rootSupport.LocalLightweightMax, tc.leafSupport.LocalLightweightMax)
			}
			if !truthLevelPtrEqual(tc.rootSupport.LocalAuthoritativeMax, tc.leafSupport.LocalAuthoritativeMax) {
				t.Fatalf("%s: LocalAuthoritativeMax = %v (root) vs %v (freshness)", tc.name, tc.rootSupport.LocalAuthoritativeMax, tc.leafSupport.LocalAuthoritativeMax)
			}
			if !truthLevelPtrEqual(tc.rootSupport.LocalFullStackMax, tc.leafSupport.LocalFullStackMax) {
				t.Fatalf("%s: LocalFullStackMax = %v (root) vs %v (freshness)", tc.name, tc.rootSupport.LocalFullStackMax, tc.leafSupport.LocalFullStackMax)
			}
			if !truthLevelPtrEqual(tc.rootSupport.ProductionMax, tc.leafSupport.ProductionMax) {
				t.Fatalf("%s: ProductionMax = %v (root) vs %v (freshness)", tc.name, tc.rootSupport.ProductionMax, tc.leafSupport.ProductionMax)
			}
			if tc.rootSupport.RequiredProfile != tc.leafSupport.RequiredProfile {
				t.Fatalf("%s: RequiredProfile = %q (root) vs %q (freshness)", tc.name, tc.rootSupport.RequiredProfile, tc.leafSupport.RequiredProfile)
			}
		})
	}
}

// truthLevelPtrEqual compares two *TruthLevel by value: CapabilitySupport's
// ceilings are pointers (so a shared var cannot leave callers aliased, see
// querycontract.HardcodedSecretSupport), so a plain != would compare
// addresses instead of the truth levels they hold.
func truthLevelPtrEqual(a, b *TruthLevel) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
