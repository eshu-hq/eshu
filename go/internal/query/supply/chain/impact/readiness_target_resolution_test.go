// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

func TestIntersectReadinessTargetPackagesUnionsAliasesForSurvivingID(t *testing.T) {
	t.Parallel()

	first := map[string]readinessTargetPackage{
		"npm://registry.npmjs.org/lodash": {
			packageID: "npm://registry.npmjs.org/lodash",
			keys: []readinessTargetKey{{
				packageID: "npm://registry.npmjs.org/lodash",
				key:       packageidentity.ConsumptionKey{Ecosystem: packageidentity.EcosystemNPM, PackageName: "lodash"},
			}},
		},
	}
	second := map[string]readinessTargetPackage{
		"npm://registry.npmjs.org/lodash": {
			packageID: "npm://registry.npmjs.org/lodash",
			keys: []readinessTargetKey{{
				packageID: "npm://registry.npmjs.org/lodash",
				key:       packageidentity.ConsumptionKey{Ecosystem: packageidentity.EcosystemNPM, PackageName: "lodash.js"},
			}},
		},
	}

	got := intersectReadinessTargetPackages([]map[string]readinessTargetPackage{first, second})
	want := readinessTarget{
		PackageIDs: []string{"npm://registry.npmjs.org/lodash"},
		Keys: []readinessTargetKey{
			{packageID: "npm://registry.npmjs.org/lodash", key: packageidentity.ConsumptionKey{Ecosystem: packageidentity.EcosystemNPM, PackageName: "lodash"}},
			{packageID: "npm://registry.npmjs.org/lodash", key: packageidentity.ConsumptionKey{Ecosystem: packageidentity.EcosystemNPM, PackageName: "lodash.js"}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("intersectReadinessTargetPackages() = %#v, want %#v", got, want)
	}
}

func TestReadinessTargetOwnerArgumentsPreservesPackageKeyAssociation(t *testing.T) {
	t.Parallel()

	target := readinessTarget{
		Keys: []readinessTargetKey{
			{
				packageID:     "npm://registry.npmjs.org/lodash",
				key:           packageidentity.ConsumptionKey{Ecosystem: packageidentity.EcosystemNPM, PackageName: "lodash"},
				parserTrusted: true,
			},
			{
				packageID: "npm://registry.npmjs.org/left-pad",
				key:       packageidentity.ConsumptionKey{Ecosystem: packageidentity.EcosystemNPM, PackageName: "left-pad"},
			},
		},
	}

	packageIDs, ecosystems, packageNames, trustedPackageIDs, trustedEcosystems, trustedPackageNames := readinessTargetOwnerArguments(target)
	if got, want := packageIDs, []string{"npm://registry.npmjs.org/lodash", "npm://registry.npmjs.org/left-pad"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("owner package IDs = %#v, want %#v", got, want)
	}
	if got, want := ecosystems, []string{"npm", "npm"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("owner ecosystems = %#v, want %#v", got, want)
	}
	if got, want := packageNames, []string{"lodash", "left-pad"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("owner package names = %#v, want %#v", got, want)
	}
	if got, want := trustedPackageIDs, []string{"npm://registry.npmjs.org/lodash"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trusted package IDs = %#v, want %#v", got, want)
	}
	if got, want := trustedEcosystems, []string{"npm"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trusted ecosystems = %#v, want %#v", got, want)
	}
	if got, want := trustedPackageNames, []string{"lodash"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trusted package names = %#v, want %#v", got, want)
	}
}

func TestIntersectReadinessTargetPackagesDropsNonIntersectingID(t *testing.T) {
	t.Parallel()

	got := intersectReadinessTargetPackages([]map[string]readinessTargetPackage{
		{
			"npm://registry.npmjs.org/lodash": {packageID: "npm://registry.npmjs.org/lodash"},
		},
		{
			"npm://registry.npmjs.org/left-pad": {packageID: "npm://registry.npmjs.org/left-pad"},
		},
	})
	if len(got.PackageIDs) != 0 || len(got.Keys) != 0 {
		t.Fatalf("intersectReadinessTargetPackages() = %#v, want empty intersection", got)
	}
}

func TestResolveReadinessTargetPackageUsesPURLCanonicalIdentity(t *testing.T) {
	t.Parallel()

	got, err := resolveReadinessTargetPackage(
		"",
		"pkg:npm/lodash@4.17.21",
		"npm",
		"lodash",
		nil,
	)
	if err != nil {
		t.Fatalf("resolveReadinessTargetPackage() error = %v", err)
	}
	if got.packageID != "npm://registry.npmjs.org/lodash" {
		t.Fatalf("package ID = %q, want canonical npm package ID", got.packageID)
	}
	if !reflect.DeepEqual(got.keys, []readinessTargetKey{{
		packageID:     "npm://registry.npmjs.org/lodash",
		key:           packageidentity.ConsumptionKey{Ecosystem: packageidentity.EcosystemNPM, PackageName: "lodash"},
		parserTrusted: true,
	}}) {
		t.Fatalf("keys = %#v, want lodash consumption key", got.keys)
	}
}
