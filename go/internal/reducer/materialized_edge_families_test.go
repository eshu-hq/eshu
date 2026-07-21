// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"testing"
)

// TestMaterializedEdgeFamiliesLocksToAllProjectionDomains is the drift-proof
// lockstep for #5351: MaterializedEdgeFamilies must always equal
// allProjectionDomains (shared_projection.go's own registry of the 12
// reducer-owned shared/edge projection domains), sorted and deduplicated. A
// domain added to or removed from allProjectionDomains must move this result
// in the SAME change, or the Ifá materialized-edge exhaustiveness gate
// (go/internal/ifa's materialized_edges:<domain> surface family) would silently
// stop enumerating (or wrongly keep enumerating) that family.
func TestMaterializedEdgeFamiliesLocksToAllProjectionDomains(t *testing.T) {
	t.Parallel()

	got := MaterializedEdgeFamilies()

	want := make([]string, 0, len(allProjectionDomains))
	for _, d := range allProjectionDomains {
		want = append(want, string(d))
	}
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("MaterializedEdgeFamilies() returned %d families, want %d: got=%v want=%v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("MaterializedEdgeFamilies()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestMaterializedEdgeFamiliesIsSorted proves the result is deterministic
// output, not incidental map/slice ordering: every consumer (the gate's
// EnumerateMaterializedEdgeSurfaces, the coverage report) depends on a stable
// order for byte-stable output.
func TestMaterializedEdgeFamiliesIsSorted(t *testing.T) {
	t.Parallel()

	got := MaterializedEdgeFamilies()
	if !sort.StringsAreSorted(got) {
		t.Fatalf("MaterializedEdgeFamilies() = %v, not sorted", got)
	}
}

// TestMaterializedEdgeFamiliesContainsSQLRelationships proves the one family
// #5351 lands first coverage for is actually enumerated by the drift-proof
// source, not hand-listed in the gate.
func TestMaterializedEdgeFamiliesContainsSQLRelationships(t *testing.T) {
	t.Parallel()

	found := false
	for _, f := range MaterializedEdgeFamilies() {
		if f == DomainSQLRelationships {
			found = true
		}
	}
	if !found {
		t.Fatalf("MaterializedEdgeFamilies() = %v, want it to contain %q", MaterializedEdgeFamilies(), DomainSQLRelationships)
	}
}

// TestMaterializedEdgeFamiliesNoDuplicates guards against a future
// allProjectionDomains edit that accidentally lists a domain twice: a
// duplicate family would double-enumerate one materialized_edges:<domain>
// surface, corrupting the coverage manifest lockstep (two rows collapsing to
// one map key in replaycoverage.Reconcile).
func TestMaterializedEdgeFamiliesNoDuplicates(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)
	for _, f := range MaterializedEdgeFamilies() {
		if seen[f] {
			t.Errorf("MaterializedEdgeFamilies() contains duplicate family %q", f)
		}
		seen[f] = true
	}
}
