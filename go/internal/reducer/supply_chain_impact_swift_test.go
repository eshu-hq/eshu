// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsUsesSwiftSemverRanges(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-swift-crypto", "CVE-2026-71001", 8.8),
		vulnerabilityAffectedPackageRangeFact(
			"affected-swift-crypto",
			"CVE-2026-71001",
			"swift://github.com/apple/swift-crypto",
			"SwiftURL",
			"swift-crypto",
			"4.3.1",
		),
		swiftLockfileConsumptionFact(
			"consume-swift-crypto",
			"swift://github.com/apple/swift-crypto",
			testImpactRepositoryID,
			"4.3.0",
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-71001"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.ObservedVersion != "4.3.0" {
		t.Fatalf("ObservedVersion = %q, want Swift Package.resolved version 4.3.0", got.ObservedVersion)
	}
	if got.MatchReason != "swift_semver_affected_range" {
		t.Fatalf("MatchReason = %q, want swift_semver_affected_range", got.MatchReason)
	}
	if got.DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("DetectionProfile = %q, want precise for exact Swift lockfile match", got.DetectionProfile)
	}
}

func TestBuildSupplyChainImpactFindingsMarksSwiftKnownFixed(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-fixed-swift-crypto", "CVE-2026-71002", 5.4),
		vulnerabilityAffectedPackageRangeFact(
			"affected-fixed-swift-crypto",
			"CVE-2026-71002",
			"swift://github.com/apple/swift-crypto",
			"swift",
			"swift-crypto",
			"4.3.1",
		),
		swiftLockfileConsumptionFact(
			"consume-fixed-swift-crypto",
			"swift://github.com/apple/swift-crypto",
			testImpactRepositoryID,
			"4.3.1",
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-71002"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactNotAffectedKnownFixed)
	if got.MatchReason != "swift_semver_known_fixed" {
		t.Fatalf("MatchReason = %q, want swift_semver_known_fixed", got.MatchReason)
	}
	if got.DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("DetectionProfile = %q, want precise for known-fixed Swift match", got.DetectionProfile)
	}
}

func swiftLockfileConsumptionFact(
	factID string,
	packageID string,
	repositoryID string,
	dependencyRange string,
) facts.Envelope {
	envelope := packageConsumptionFactWithRange(factID, packageID, repositoryID, dependencyRange)
	envelope.Payload["lockfile"] = true
	return envelope
}
