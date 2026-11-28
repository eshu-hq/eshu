// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"slices"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
)

// TestSupplyChainImpactBranchValuesRebindsPackageBoundProbes pins the #11097
// runtime binding: with seed packages or PURLs present,
// affected_package_package_id, affected_package_purl, and foreign_key_guard
// read owned PackageIDs/PURLs (and their lowered twins) only, while consumer,
// SBOM, version, and suppression branches keep the union. When both #11087 and
// #11097 seed lists are present, foreign_key_guard preserves both rebinds
// without clobbering either. Seam-off returns the shared map unchanged.
func TestSupplyChainImpactBranchValuesRebindsPackageBoundProbes(t *testing.T) {
	t.Parallel()

	filter := supplychaincore.SupplyChainImpactFactFilter{
		CVEIDs:              []string{"CVE-2026-0001"},
		PackageIDs:          []string{"npm://registry.npmjs.org/OwnedPkg"},
		SeedPackageIDs:      []string{"npm://registry.npmjs.org/SeedPkg"},
		PURLs:               []string{"pkg:npm/OwnedPkg"},
		SeedPURLs:           []string{"pkg:npm/SeedPkg"},
		ProductCriteria:     []string{"cpe:2.3:a:scope:toaster:2.0:*:*:*:*:*:*:*"},
		SeedProductCriteria: []string{"cpe:2.3:a:seeded:gadget:1.0:*:*:*:*:*:*:*"},
	}
	values := supplyChainImpactParamValues(filter)

	wantPkgUnion := []string{"npm://registry.npmjs.org/OwnedPkg", "npm://registry.npmjs.org/SeedPkg"}
	wantPURLUnion := []string{"pkg:npm/OwnedPkg", "pkg:npm/SeedPkg"}
	wantLowerPkgUnion := []string{"npm://registry.npmjs.org/ownedpkg", "npm://registry.npmjs.org/seedpkg"}
	wantLowerPURLUnion := []string{"pkg:npm/ownedpkg", "pkg:npm/seedpkg"}

	if got := values[factimpact.PackageIDs].([]string); !slices.Equal(got, wantPkgUnion) {
		t.Fatalf("PackageIDs union = %#v, want %#v", got, wantPkgUnion)
	}
	if got := values[factimpact.PURLs].([]string); !slices.Equal(got, wantPURLUnion) {
		t.Fatalf("PURLs union = %#v, want %#v", got, wantPURLUnion)
	}
	if got := values[factimpact.LowerPackageIDs].([]string); !slices.Equal(got, wantLowerPkgUnion) {
		t.Fatalf("LowerPackageIDs union = %#v, want %#v", got, wantLowerPkgUnion)
	}
	if got := values[factimpact.LowerPURLs].([]string); !slices.Equal(got, wantLowerPURLUnion) {
		t.Fatalf("LowerPURLs union = %#v, want %#v", got, wantLowerPURLUnion)
	}

	for _, name := range []string{
		factimpact.BranchAffectedPackagePackageID,
		factimpact.BranchAffectedPackagePURL,
		factimpact.BranchForeignKeyGuard,
	} {
		bound := supplyChainImpactBranchValues(supplyChainImpactBranch(t, name), values, filter)
		if got := bound[factimpact.PackageIDs].([]string); !slices.Equal(got, filter.PackageIDs) {
			t.Fatalf("%s PackageIDs = %#v, want owned only %#v", name, got, filter.PackageIDs)
		}
		if got := bound[factimpact.PURLs].([]string); !slices.Equal(got, filter.PURLs) {
			t.Fatalf("%s PURLs = %#v, want owned only %#v", name, got, filter.PURLs)
		}
		if got := bound[factimpact.LowerPackageIDs].([]string); !slices.Equal(got, []string{"npm://registry.npmjs.org/ownedpkg"}) {
			t.Fatalf("%s LowerPackageIDs = %#v, want owned lower only", name, got)
		}
		if got := bound[factimpact.LowerPURLs].([]string); !slices.Equal(got, []string{"pkg:npm/ownedpkg"}) {
			t.Fatalf("%s LowerPURLs = %#v, want owned lower only", name, got)
		}
		if name == factimpact.BranchForeignKeyGuard {
			if got := bound[factimpact.ProductCriteria].([]string); !slices.Equal(got, filter.ProductCriteria) {
				t.Fatalf("foreign_key_guard must preserve #11087 ProductCriteria rebind alongside #11097, got %#v", got)
			}
		}
	}

	for _, name := range []string{
		"package_consumption_package_id",
		"sbom_component_purl",
		"package_id",
		"package_version_purl",
		"vulnerability_suppression",
	} {
		bound := supplyChainImpactBranchValues(supplyChainImpactBranch(t, name), values, filter)
		if got := bound[factimpact.PackageIDs].([]string); !slices.Equal(got, wantPkgUnion) {
			t.Fatalf("%s must keep PackageIDs union, got %#v", name, got)
		}
		if got := bound[factimpact.PURLs].([]string); !slices.Equal(got, wantPURLUnion) {
			t.Fatalf("%s must keep PURLs union, got %#v", name, got)
		}
		if got := bound[factimpact.LowerPackageIDs].([]string); !slices.Equal(got, wantLowerPkgUnion) {
			t.Fatalf("%s must keep LowerPackageIDs union, got %#v", name, got)
		}
		if got := bound[factimpact.LowerPURLs].([]string); !slices.Equal(got, wantLowerPURLUnion) {
			t.Fatalf("%s must keep LowerPURLs union, got %#v", name, got)
		}
	}

	// Filter with ONLY SeedPackageIDs/SeedPURLs must not be treated as empty
	// and must skip affected_package_* probes (owned lists are empty) while
	// still running consumer probes.
	seedOnly := supplychaincore.SupplyChainImpactFactFilter{
		SeedPackageIDs: []string{"npm://registry.npmjs.org/SeedPkg"},
		SeedPURLs:      []string{"pkg:npm/SeedPkg"},
	}
	normalizeSupplyChainImpactFactFilter(&seedOnly)
	if supplyChainImpactFactFilterEmpty(seedOnly) {
		t.Fatal("filter with SeedPackageIDs/SeedPURLs must not report empty")
	}
	seedOnlyValues := supplyChainImpactParamValues(seedOnly)
	for _, name := range []string{factimpact.BranchAffectedPackagePackageID, factimpact.BranchAffectedPackagePURL} {
		branch := supplyChainImpactBranch(t, name)
		bound := supplyChainImpactBranchValues(branch, seedOnlyValues, seedOnly)
		if supplyChainImpactBranchRuns(branch, bound) {
			t.Fatalf("%s must not run when only seed packages/PURLs are set", name)
		}
	}
	for _, name := range []string{"package_consumption_package_id", "sbom_component_purl", "package_id", "package_version_purl"} {
		branch := supplyChainImpactBranch(t, name)
		bound := supplyChainImpactBranchValues(branch, seedOnlyValues, seedOnly)
		if !supplyChainImpactBranchRuns(branch, bound) {
			t.Fatalf("%s must run when seed packages/PURLs are set", name)
		}
	}

	// Seam-off: no seed lists returns the shared map unchanged for every branch.
	seamOff := supplychaincore.SupplyChainImpactFactFilter{
		PackageIDs: []string{"npm://registry.npmjs.org/OwnedPkg"},
		PURLs:      []string{"pkg:npm/OwnedPkg"},
	}
	seamOffValues := supplyChainImpactParamValues(seamOff)
	for _, branch := range factimpact.Branches() {
		bound := supplyChainImpactBranchValues(branch, seamOffValues, seamOff)
		if len(bound) != len(seamOffValues) {
			t.Fatalf("seam-off branch %s must keep shared map, got %d want %d", branch.Name, len(bound), len(seamOffValues))
		}
	}
}
