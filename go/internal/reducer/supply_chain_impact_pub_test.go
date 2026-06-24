// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsMatchesPubLockfileSemverAndKeepsRangeOnlyPartial(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-pub-http", "CVE-2026-1092", 8.2),
		vulnerabilityAffectedPackageRangeFact(
			"affected-pub-http",
			"CVE-2026-1092",
			"pub://pub.dev/http",
			"Pub",
			"http",
			"1.3.0",
		),
		pubLockfileConsumptionFact(
			"consume-pub-http",
			"pub://pub.dev/http",
			testImpactRepositoryID,
			"1.2.2",
		),
		vulnerabilityCVEFact("cve-pub-collection", "CVE-2026-1093", 6.9),
		vulnerabilityAffectedPackageRangeFact(
			"affected-pub-collection",
			"CVE-2026-1093",
			"pub://pub.dev/collection",
			"pub",
			"collection",
			"1.19.0",
		),
		packageConsumptionFactWithRange(
			"consume-pub-collection",
			"pub://pub.dev/collection",
			testImpactRepositoryID,
			"^1.18.0",
		),
	})

	byCVE := supplyChainImpactFindingsByCVE(findings)
	exact := byCVE["CVE-2026-1092"]
	assertSupplyChainImpactStatus(t, exact, SupplyChainImpactAffectedExact)
	if exact.ObservedVersion != "1.2.2" {
		t.Fatalf("ObservedVersion = %q, want Pub lockfile version 1.2.2", exact.ObservedVersion)
	}
	if exact.RequestedRange != "1.2.2" {
		t.Fatalf("RequestedRange = %q, want preserved Pub lockfile request 1.2.2", exact.RequestedRange)
	}
	if exact.VulnerableRange == "" {
		t.Fatalf("VulnerableRange = blank, want advisory SEMVER range preserved")
	}
	if exact.FixedVersion != "1.3.0" {
		t.Fatalf("FixedVersion = %q, want 1.3.0", exact.FixedVersion)
	}
	if exact.MatchReason != "pub_semver_affected_range" {
		t.Fatalf("MatchReason = %q, want pub_semver_affected_range", exact.MatchReason)
	}
	if exact.DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("DetectionProfile = %q, want precise for exact Pub lockfile match", exact.DetectionProfile)
	}

	rangeOnly := byCVE["CVE-2026-1093"]
	assertSupplyChainImpactStatus(t, rangeOnly, SupplyChainImpactPossiblyAffected)
	if rangeOnly.ObservedVersion != "" {
		t.Fatalf("ObservedVersion = %q, want blank for Pub manifest range-only evidence", rangeOnly.ObservedVersion)
	}
	if rangeOnly.RequestedRange != "^1.18.0" {
		t.Fatalf("RequestedRange = %q, want Pub manifest range ^1.18.0", rangeOnly.RequestedRange)
	}
	if rangeOnly.MatchReason != "range_only_manifest" {
		t.Fatalf("MatchReason = %q, want range_only_manifest", rangeOnly.MatchReason)
	}
	assertContainsString(t, rangeOnly.MissingEvidence, "installed package version missing")
}

func pubLockfileConsumptionFact(
	factID string,
	packageID string,
	repositoryID string,
	dependencyRange string,
) facts.Envelope {
	envelope := packageConsumptionFactWithRange(factID, packageID, repositoryID, dependencyRange)
	envelope.Payload["lockfile"] = true
	return envelope
}
