// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"slices"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
)

// TestSupplyChainImpactBranchValuesRebindsBoundProbes pins the #11087 runtime
// binding: with seed-product criteria present, the affected_product_criteria
// probe and the foreign_key_guard read the component-origin criteria only, so
// a transitive row's criteria can neither pull another CVE's product rows nor
// re-enter through the guard's criteria arm. Every other branch keeps the
// union, and seam-off returns the shared map unchanged.
func TestSupplyChainImpactBranchValuesRebindsBoundProbes(t *testing.T) {
	t.Parallel()

	filter := supplychaincore.SupplyChainImpactFactFilter{
		CVEIDs:              []string{"CVE-2026-0001"},
		ProductCriteria:     []string{"cpe:2.3:a:scope:toaster:2.0:*:*:*:*:*:*:*"},
		SeedProductCriteria: []string{"cpe:2.3:a:seeded:gadget:1.0:*:*:*:*:*:*:*"},
	}
	values := supplyChainImpactParamValues(filter)
	union, ok := values[factimpact.ProductCriteria].([]string)
	if !ok || !slices.Equal(union, []string{
		"cpe:2.3:a:scope:toaster:2.0:*:*:*:*:*:*:*",
		"cpe:2.3:a:seeded:gadget:1.0:*:*:*:*:*:*:*",
	}) {
		t.Fatalf("criteria entry must bind the union, got %#v", values[factimpact.ProductCriteria])
	}

	for _, name := range []string{factimpact.BranchAffectedProductCriteria, factimpact.BranchForeignKeyGuard} {
		bound := supplyChainImpactBranchValues(supplyChainImpactBranch(t, name), values, filter)
		if got, ok := bound[factimpact.ProductCriteria].([]string); !ok || !slices.Equal(got, filter.ProductCriteria) {
			t.Fatalf("%s must rebind criteria to the component list, got %#v", name, bound[factimpact.ProductCriteria])
		}
		if !slices.Equal(bound[factimpact.CVEIDs].([]string), values[factimpact.CVEIDs].([]string)) {
			t.Fatalf("%s must share the other bindings, CVEIDs moved", name)
		}
	}
	for _, name := range []string{"sbom_component_cpe", "vulnerability_cve_id"} {
		bound := supplyChainImpactBranchValues(supplyChainImpactBranch(t, name), values, filter)
		if got := bound[factimpact.ProductCriteria]; !slices.Equal(got.([]string), union) {
			t.Fatalf("%s must keep the union, got %#v", name, got)
		}
	}
	if _, ok := values[factimpact.ProductCriteria].([]string); !ok {
		t.Fatalf("shared map must stay intact, got %#v", values[factimpact.ProductCriteria])
	}

	seamOff := supplychaincore.SupplyChainImpactFactFilter{
		CVEIDs:          []string{"CVE-2026-0001"},
		ProductCriteria: []string{"cpe:2.3:a:scope:toaster:2.0:*:*:*:*:*:*:*"},
	}
	seamOffValues := supplyChainImpactParamValues(seamOff)
	// Seam off the union degenerates to exactly ProductCriteria: every probe
	// binds today's values, byte-identical to before the seam.
	if got, ok := seamOffValues[factimpact.ProductCriteria].([]string); !ok || !slices.Equal(got, seamOff.ProductCriteria) {
		t.Fatalf("seam-off union = %#v, want exactly ProductCriteria", seamOffValues[factimpact.ProductCriteria])
	}
	for _, branch := range factimpact.Branches() {
		bound := supplyChainImpactBranchValues(branch, seamOffValues, seamOff)
		if len(bound) != len(seamOffValues) {
			t.Fatalf("seam-off branch %s must keep the shared map, got %d entries want %d", branch.Name, len(bound), len(seamOffValues))
		}
	}
}
