// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsMatchesNuGetBracketRange(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-nuget-bracket", "CVE-2026-10150", 8.1),
		vulnerabilityAffectedPackageRawRangeFact(
			"affected-nuget-bracket",
			"CVE-2026-10150",
			"pkg:nuget/newtonsoft.json",
			"nuget",
			"Newtonsoft.Json",
			"[13.0.0,13.0.4)",
			"13.0.4",
		),
		nugetLockfileConsumptionFactWithChain(
			"consume-nuget-bracket",
			"pkg:nuget/newtonsoft.json",
			testImpactRepositoryID,
			"13.0.3",
			[]string{"Newtonsoft.Json"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-10150"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.MatchReason != supplyChainVersionReasonNuGetSemverAffectedRange {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonNuGetSemverAffectedRange)
	}
	if got.VulnerableRange != "[13.0.0,13.0.4)" {
		t.Fatalf("VulnerableRange = %q, want bracket range preserved", got.VulnerableRange)
	}
	assertSupplyChainReachability(
		t,
		got,
		SupplyChainReachabilityReachable,
		"nuget",
		"nuget_dependency_path",
	)
}

func TestBuildSupplyChainImpactFindingsMarksNuGetFixedVersionSafe(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-nuget-fixed", "CVE-2026-10151", 8.1),
		vulnerabilityAffectedPackageRawRangeFact(
			"affected-nuget-fixed",
			"CVE-2026-10151",
			"pkg:nuget/newtonsoft.json",
			"nuget",
			"Newtonsoft.Json",
			"[13.0.0,13.0.4)",
			"13.0.4",
		),
		nugetLockfileConsumptionFactWithChain(
			"consume-nuget-fixed",
			"pkg:nuget/newtonsoft.json",
			testImpactRepositoryID,
			"13.0.4",
			[]string{"Newtonsoft.Json"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-10151"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactNotAffectedKnownFixed)
	if got.MatchReason != supplyChainVersionReasonNuGetSemverKnownFixed {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonNuGetSemverKnownFixed)
	}
	if got.ObservedVersion != "13.0.4" || got.FixedVersion != "13.0.4" {
		t.Fatalf("version truth = observed %q fixed %q, want 13.0.4/13.0.4", got.ObservedVersion, got.FixedVersion)
	}
}

func TestBuildSupplyChainImpactFindingsKeepsNuGetLockfileRequestedRangeSeparate(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 31, 12, 10, 0, 0, time.UTC)
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-nuget-requested", "CVE-2026-10152", 8.1),
		vulnerabilityAffectedPackageRawRangeFact(
			"affected-nuget-requested",
			"CVE-2026-10152",
			"pkg:nuget/newtonsoft.json",
			"nuget",
			"Newtonsoft.Json",
			"[13.0.0,13.0.4)",
			"13.0.4",
		),
		packageManifestDependencyFactWithMetadata(
			testImpactRepositoryID,
			"dotnet-worker",
			"packages.lock.json",
			"Newtonsoft.Json",
			"nuget",
			"13.0.3",
			observedAt,
			map[string]any{
				"section":           "packages.lock.json:net8.0",
				"lockfile":          true,
				"requested_range":   "[13.0.0, 14.0.0)",
				"dependency_path":   []any{"Newtonsoft.Json"},
				"dependency_depth":  1,
				"direct_dependency": true,
			},
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-10152"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.ObservedVersion != "13.0.3" {
		t.Fatalf("ObservedVersion = %q, want exact lockfile version", got.ObservedVersion)
	}
	if got.RequestedRange != "[13.0.0, 14.0.0)" {
		t.Fatalf("RequestedRange = %q, want lockfile requested range", got.RequestedRange)
	}
}

func TestBuildSupplyChainImpactFindingsKeepsNuGetManifestOnlyPartial(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 31, 12, 12, 0, 0, time.UTC)
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-nuget-manifest-only", "CVE-2026-10155", 8.1),
		vulnerabilityAffectedPackageRawRangeFact(
			"affected-nuget-manifest-only",
			"CVE-2026-10155",
			"pkg:nuget/newtonsoft.json",
			"nuget",
			"Newtonsoft.Json",
			"[13.0.0,13.0.4)",
			"13.0.4",
		),
		packageManifestDependencyFactWithMetadata(
			testImpactRepositoryID,
			"dotnet-worker",
			"Worker.csproj",
			"Newtonsoft.Json",
			"nuget",
			"13.0.3",
			observedAt,
			map[string]any{
				"section":           "PackageReference",
				"requested_version": "13.0.3",
			},
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-10155"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.ObservedVersion != "" {
		t.Fatalf("ObservedVersion = %q, want empty without packages.lock.json evidence", got.ObservedVersion)
	}
	if got.RequestedRange != "13.0.3" {
		t.Fatalf("RequestedRange = %q, want manifest requested version", got.RequestedRange)
	}
	if len(got.DependencyPath) != 0 || got.DependencyDepth != 0 || got.DirectDependency != nil {
		t.Fatalf(
			"dependency chain = path %#v depth %d direct %#v, want empty until packages.lock.json proves it",
			got.DependencyPath,
			got.DependencyDepth,
			got.DirectDependency,
		)
	}
	if got.MatchReason != supplyChainVersionReasonRangeOnlyManifest {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonRangeOnlyManifest)
	}
	assertContainsString(t, got.MissingEvidence, supplyChainMissingInstalledVersion)
}

func TestBuildSupplyChainImpactFindingsKeepsNuGetUnresolvedPropertyPartial(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 31, 12, 15, 0, 0, time.UTC)
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-nuget-unresolved", "CVE-2026-10153", 8.1),
		vulnerabilityAffectedPackageRawRangeFact(
			"affected-nuget-unresolved",
			"CVE-2026-10153",
			"pkg:nuget/newtonsoft.json",
			"nuget",
			"Newtonsoft.Json",
			"[13.0.0,13.0.4)",
			"13.0.4",
		),
		packageManifestDependencyFactWithMetadata(
			testImpactRepositoryID,
			"dotnet-worker",
			"Worker.csproj",
			"Newtonsoft.Json",
			"nuget",
			"$(NewtonsoftJsonVersion)",
			observedAt,
			map[string]any{
				"section":                     "PackageReference",
				"requested_version":           "$(NewtonsoftJsonVersion)",
				"version_evidence":            "unresolved_msbuild_property",
				"unresolved_msbuild_property": "NewtonsoftJsonVersion",
				"partial_evidence":            true,
			},
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-10153"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.ObservedVersion != "" {
		t.Fatalf("ObservedVersion = %q, want empty for unresolved MSBuild property", got.ObservedVersion)
	}
	if got.MatchReason != supplyChainVersionReasonRangeOnlyManifest {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonRangeOnlyManifest)
	}
	assertContainsString(t, got.MissingEvidence, "msbuild property unresolved: NewtonsoftJsonVersion")
	assertContainsString(t, got.MissingEvidence, supplyChainMissingInstalledVersion)
}

func TestBuildSupplyChainImpactFindingsFailsClosedForMalformedNuGetRange(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-nuget-malformed", "CVE-2026-10154", 8.1),
		vulnerabilityAffectedPackageRawRangeFact(
			"affected-nuget-malformed",
			"CVE-2026-10154",
			"pkg:nuget/newtonsoft.json",
			"nuget",
			"Newtonsoft.Json",
			"[13.0.0,13.0.4",
			"13.0.4",
		),
		nugetLockfileConsumptionFactWithChain(
			"consume-nuget-malformed",
			"pkg:nuget/newtonsoft.json",
			testImpactRepositoryID,
			"13.0.3",
			[]string{"Newtonsoft.Json"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-10154"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.MatchReason != supplyChainVersionReasonMalformedRange {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonMalformedRange)
	}
	assertContainsString(t, got.MissingEvidence, supplyChainMissingMalformedRange)
}

func nugetLockfileConsumptionFactWithChain(
	factID string,
	packageID string,
	repositoryID string,
	dependencyRange string,
	dependencyPath []string,
	dependencyDepth int,
	directDependency bool,
) facts.Envelope {
	envelope := packageConsumptionFactWithChain(
		factID,
		packageID,
		repositoryID,
		dependencyRange,
		dependencyPath,
		dependencyDepth,
		directDependency,
	)
	envelope.Payload["lockfile"] = true
	return envelope
}
