// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsProvesNPMFamilyLockfileExactVersions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		factID           string
		packageID        string
		cveID            string
		observedVersion  string
		fixedVersion     string
		dependencyPath   []string
		dependencyDepth  int
		directDependency bool
	}{
		{
			name:             "package lock direct",
			factID:           "consume-package-lock-express",
			packageID:        "pkg:npm/express",
			cveID:            "CVE-2026-99701",
			observedVersion:  "4.18.2",
			fixedVersion:     "4.19.0",
			dependencyPath:   []string{"express"},
			dependencyDepth:  1,
			directDependency: true,
		},
		{
			name:             "yarn classic transitive",
			factID:           "consume-yarn-rollup",
			packageID:        "pkg:npm/rollup",
			cveID:            "CVE-2026-99702",
			observedVersion:  "4.0.0",
			fixedVersion:     "4.8.0",
			dependencyPath:   []string{"vite", "rollup"},
			dependencyDepth:  2,
			directDependency: false,
		},
		{
			name:             "yarn berry direct",
			factID:           "consume-yarn-berry-lodash",
			packageID:        "pkg:npm/lodash",
			cveID:            "CVE-2026-99703",
			observedVersion:  "4.17.20",
			fixedVersion:     "4.17.21",
			dependencyPath:   []string{"lodash"},
			dependencyDepth:  1,
			directDependency: true,
		},
		{
			name:             "pnpm transitive",
			factID:           "consume-pnpm-fsevents",
			packageID:        "pkg:npm/fsevents",
			cveID:            "CVE-2026-99704",
			observedVersion:  "2.3.2",
			fixedVersion:     "2.3.3",
			dependencyPath:   []string{"vite", "rollup", "fsevents"},
			dependencyDepth:  3,
			directDependency: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			findings := BuildSupplyChainImpactFindings([]facts.Envelope{
				vulnerabilityCVEFact("cve-"+tc.cveID, tc.cveID, 7.1),
				vulnerabilityAffectedPackageRangeFact(
					"affected-"+tc.cveID,
					tc.cveID,
					tc.packageID,
					"npm",
					npmPackageNameFromID(tc.packageID),
					tc.fixedVersion,
				),
				packageConsumptionFactWithChain(
					tc.factID,
					tc.packageID,
					testImpactRepositoryID,
					tc.observedVersion,
					tc.dependencyPath,
					tc.dependencyDepth,
					tc.directDependency,
				),
			})

			got := supplyChainImpactFindingsByCVE(findings)[tc.cveID]
			assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
			if got.ObservedVersion != tc.observedVersion {
				t.Fatalf("ObservedVersion = %q, want %q", got.ObservedVersion, tc.observedVersion)
			}
			if got.MatchReason != supplyChainVersionReasonNPMSemverAffectedRange {
				t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonNPMSemverAffectedRange)
			}
			if got.DetectionProfile != DetectionProfilePrecise {
				t.Fatalf("DetectionProfile = %q, want precise", got.DetectionProfile)
			}
			if !reflect.DeepEqual(got.DependencyPath, tc.dependencyPath) {
				t.Fatalf("DependencyPath = %#v, want %#v", got.DependencyPath, tc.dependencyPath)
			}
			if got.DirectDependency == nil || *got.DirectDependency != tc.directDependency {
				t.Fatalf("DirectDependency = %#v, want %v", got.DirectDependency, tc.directDependency)
			}
		})
	}
}

func TestBuildSupplyChainImpactFindingsKeepsNPMDevScopeVisible(t *testing.T) {
	t.Parallel()

	consumption := packageConsumptionFactWithChain(
		"consume-dev-vitest",
		"pkg:npm/vitest",
		testImpactRepositoryID,
		"2.0.0",
		[]string{"vitest"},
		1,
		true,
	)
	consumption.Payload["dependency_scope"] = "dev"

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-vitest", "CVE-2026-99705", 4.3),
		vulnerabilityAffectedPackageRangeFact(
			"affected-vitest",
			"CVE-2026-99705",
			"pkg:npm/vitest",
			"npm",
			"vitest",
			"2.1.0",
		),
		consumption,
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-99705"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.DependencyScope != "dev" {
		t.Fatalf("DependencyScope = %q, want dev", got.DependencyScope)
	}
	if got.DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("DetectionProfile = %q, want precise for exact dev lockfile evidence", got.DetectionProfile)
	}
}

func npmPackageNameFromID(packageID string) string {
	switch packageID {
	case "pkg:npm/express":
		return "express"
	case "pkg:npm/rollup":
		return "rollup"
	case "pkg:npm/lodash":
		return "lodash"
	case "pkg:npm/fsevents":
		return "fsevents"
	default:
		return packageID
	}
}
