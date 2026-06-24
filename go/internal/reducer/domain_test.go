// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"testing"
)

// TestAllDomainsUnionsKnownAndProjectionDomains locks AllDomains to the union of
// the claim/materialization registry (knownDomains) and the shared/edge
// projection registry (allProjectionDomains), deduplicated. The surface
// inventory drift gate (#3145) enumerates reducer domains through AllDomains, so
// a reducer-owned domain in either registry must appear; a projection domain
// (code_calls, handles_route, runs_in, ...) missing here would silently leave
// the inventory and bypass the gate.
func TestAllDomainsUnionsKnownAndProjectionDomains(t *testing.T) {
	t.Parallel()
	got := AllDomains()

	want := map[Domain]struct{}{}
	for d := range knownDomains {
		want[d] = struct{}{}
	}
	for _, d := range allProjectionDomains {
		want[d] = struct{}{}
	}
	if len(got) != len(want) {
		t.Fatalf("AllDomains() returned %d domains, want %d (knownDomains ∪ allProjectionDomains)", len(got), len(want))
	}
	seen := map[Domain]bool{}
	for _, d := range got {
		if _, ok := want[d]; !ok {
			t.Errorf("AllDomains() returned %q which is in neither registry", d)
		}
		if seen[d] {
			t.Errorf("AllDomains() returned duplicate domain %q", d)
		}
		seen[d] = true
	}
}

// TestAllDomainsIncludesSharedProjectionDomains is the regression for the Codex
// review on #3145: shared-projection domains drained outside knownDomains must
// still be enumerated so the surface inventory tracks them.
func TestAllDomainsIncludesSharedProjectionDomains(t *testing.T) {
	t.Parallel()
	got := map[Domain]bool{}
	for _, d := range AllDomains() {
		got[d] = true
	}
	for _, d := range []Domain{DomainCodeCalls, DomainHandlesRoute, DomainRunsIn, DomainInvokesCloudAction, DomainRepoDependency} {
		if !got[d] {
			t.Errorf("AllDomains() is missing shared-projection domain %q", d)
		}
	}
}

// TestAllDomainsIsSorted guarantees deterministic order for generated output.
func TestAllDomainsIsSorted(t *testing.T) {
	t.Parallel()
	got := AllDomains()
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i] < got[j] }) {
		t.Fatalf("AllDomains() is not sorted: %v", got)
	}
}
