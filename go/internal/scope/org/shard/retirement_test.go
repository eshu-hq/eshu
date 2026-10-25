// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shard

import "testing"

func TestRetirementAcceptanceUnitIDRoundTrips(t *testing.T) {
	t.Parallel()

	id := ScopeID(testBase, "01")
	if !IsRetirement(id, RetirementAcceptanceUnitID(id)) {
		t.Fatalf("IsRetirement(%q, %q) = false", id, RetirementAcceptanceUnitID(id))
	}
	for _, au := range []string{id, "", testBase + RetirementMarker, id + RetirementMarker + "x"} {
		if IsRetirement(id, au) {
			t.Fatalf("IsRetirement(%q, %q) = true, want false", id, au)
		}
	}
	if IsRetirement("", RetirementMarker) {
		t.Fatal("blank scope id read as a retirement")
	}
}
