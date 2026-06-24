// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import "testing"

func TestReadinessLaneValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		lane ReadinessLane
		want bool
	}{
		{ReadinessImplemented, true},
		{ReadinessPartial, true},
		{ReadinessGated, true},
		{ReadinessFoundationOnly, true},
		{ReadinessFixtureOnly, true},
		{ReadinessResearchOnly, true},
		{ReadinessNotImplemented, true},
		{ReadinessUnsupported, true},
		{"", false},
		{"production_ready", false},
		{"ga", false},
	}
	for _, tc := range cases {
		if got := tc.lane.Valid(); got != tc.want {
			t.Errorf("ReadinessLane(%q).Valid() = %v, want %v", tc.lane, got, tc.want)
		}
	}
}

// TestReadinessRequiresPromotionProof locks the contract the collector
// promotion-proof gate (#3146) depends on: only an "implemented" claim asserts
// production readiness and therefore must carry promotion proof. Every gated or
// pre-promotion lane is honest about being not-yet-live and needs no proof.
func TestReadinessRequiresPromotionProof(t *testing.T) {
	t.Parallel()
	requires := map[ReadinessLane]bool{
		ReadinessImplemented:    true,
		ReadinessPartial:        false,
		ReadinessGated:          false,
		ReadinessFoundationOnly: false,
		ReadinessFixtureOnly:    false,
		ReadinessResearchOnly:   false,
		ReadinessNotImplemented: false,
		ReadinessUnsupported:    false,
	}
	for lane, want := range requires {
		if got := lane.RequiresPromotionProof(); got != want {
			t.Errorf("ReadinessLane(%q).RequiresPromotionProof() = %v, want %v", lane, got, want)
		}
	}
}

func TestAllReadinessLanesAreValid(t *testing.T) {
	t.Parallel()
	lanes := AllReadinessLanes()
	if len(lanes) == 0 {
		t.Fatal("AllReadinessLanes() returned no lanes")
	}
	seen := map[ReadinessLane]bool{}
	for _, lane := range lanes {
		if !lane.Valid() {
			t.Errorf("AllReadinessLanes() includes invalid lane %q", lane)
		}
		if seen[lane] {
			t.Errorf("AllReadinessLanes() includes duplicate lane %q", lane)
		}
		seen[lane] = true
	}
}
