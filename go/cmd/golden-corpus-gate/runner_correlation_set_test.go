// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestResolveBlockingCorrelationsAllDerivesFromSnapshot is the keystone
// assertion for #4596: the "all" sentinel must derive the blocking set from
// the snapshot's own required_correlations ids, so promoting an rc-N to
// blocking is a single-file edit (the snapshot) instead of the historical
// two-file hand-edit (snapshot plus the -required-correlations flag's
// comma-separated mirror in scripts/verify-golden-corpus-gate.sh).
func TestResolveBlockingCorrelationsAllDerivesFromSnapshot(t *testing.T) {
	rcs := []RequiredCorrelation{{ID: "rc-1"}, {ID: "rc-2"}, {ID: "rc-3"}}
	got := resolveBlockingCorrelations("all", rcs)
	want := map[string]bool{"rc-1": true, "rc-2": true, "rc-3": true}
	if len(got) != len(want) {
		t.Fatalf("resolveBlockingCorrelations(%q) = %v, want %v", "all", got, want)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("resolveBlockingCorrelations(%q) missing %q", "all", id)
		}
	}
}

// TestResolveBlockingCorrelationsExplicitListStillWorks preserves the
// pre-#4596 escape hatch: an explicit comma-separated id list still names
// exactly its own ids as blocking, independent of what the snapshot carries.
// This is how a newly-added rc can stay advisory (not yet promoted) while
// "all" is not in effect, or how a caller can block a strict subset.
func TestResolveBlockingCorrelationsExplicitListStillWorks(t *testing.T) {
	rcs := []RequiredCorrelation{{ID: "rc-1"}, {ID: "rc-2"}, {ID: "rc-3"}}
	got := resolveBlockingCorrelations("rc-1,rc-3", rcs)
	if len(got) != 2 || !got["rc-1"] || !got["rc-3"] || got["rc-2"] {
		t.Fatalf("resolveBlockingCorrelations(%q) = %v, want exactly rc-1 and rc-3 blocking", "rc-1,rc-3", got)
	}
}

// TestResolveBlockingCorrelationsEmptyMeansAllAdvisory preserves the historical
// default: an empty flag value blocks nothing, regardless of snapshot content.
func TestResolveBlockingCorrelationsEmptyMeansAllAdvisory(t *testing.T) {
	rcs := []RequiredCorrelation{{ID: "rc-1"}, {ID: "rc-2"}}
	got := resolveBlockingCorrelations("", rcs)
	if len(got) != 0 {
		t.Fatalf("resolveBlockingCorrelations(%q) = %v, want empty (all advisory)", "", got)
	}
}

// TestResolveBlockingCorrelationsAllOnEmptySnapshotIsEmpty guards the edge
// case of "all" against a snapshot that carries no required correlations: the
// result must be an empty set, not a nil-map panic on lookup, and must not
// fabricate any blocking id.
func TestResolveBlockingCorrelationsAllOnEmptySnapshotIsEmpty(t *testing.T) {
	got := resolveBlockingCorrelations("all", nil)
	if len(got) != 0 {
		t.Fatalf("resolveBlockingCorrelations(%q, nil) = %v, want empty", "all", got)
	}
}
