// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsExplainsNPMSemverRangeMatch(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-npm", "CVE-2026-59001", 8.8),
		vulnerabilityAffectedPackageRangeFact(
			"affected-npm",
			"CVE-2026-59001",
			"pkg:npm/example",
			"npm",
			"example",
			"2.0.0",
		),
		packageConsumptionFactWithRange("consume-npm", "pkg:npm/example", testImpactRepositoryID, "1.2.3+build.7"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-59001"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.ObservedVersion != "1.2.3+build.7" {
		t.Fatalf("ObservedVersion = %q, want exact installed npm version", got.ObservedVersion)
	}
	if got.RequestedRange != "1.2.3+build.7" {
		t.Fatalf("RequestedRange = %q, want original manifest range", got.RequestedRange)
	}
	if got.FixedVersion != "2.0.0" {
		t.Fatalf("FixedVersion = %q, want advisory fixed version", got.FixedVersion)
	}
	if got.MatchReason != "npm_semver_affected_range" {
		t.Fatalf("MatchReason = %q, want npm_semver_affected_range", got.MatchReason)
	}
}

func TestBuildSupplyChainImpactFindingsExplainsMavenRangeAndFixedVersion(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-maven-affected", "CVE-2026-59002", 7.7),
		vulnerabilityAffectedPackageMavenRangeFact(
			"affected-maven",
			"CVE-2026-59002",
			"pkg:maven/org.apache.maven/maven-core",
			"[3.8.0,3.9.9)",
			"3.9.9",
		),
		packageConsumptionFactWithRange(
			"consume-maven",
			"pkg:maven/org.apache.maven/maven-core",
			testImpactRepositoryID,
			"3.9.8",
		),

		vulnerabilityCVEFact("cve-maven-fixed", "CVE-2026-59003", 7.7),
		vulnerabilityAffectedPackageMavenRangeFact(
			"affected-maven-fixed",
			"CVE-2026-59003",
			"pkg:maven/org.apache.maven/maven-resolver",
			"[3.8.0,3.9.9)",
			"3.9.9",
		),
		packageConsumptionFactWithRange(
			"consume-maven-fixed",
			"pkg:maven/org.apache.maven/maven-resolver",
			testImpactRepositoryID,
			"3.9.9",
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)
	assertSupplyChainImpactStatus(t, got["CVE-2026-59002"], SupplyChainImpactAffectedExact)
	if got["CVE-2026-59002"].MatchReason != "maven_range_match" {
		t.Fatalf("affected MatchReason = %q, want maven_range_match", got["CVE-2026-59002"].MatchReason)
	}
	if got["CVE-2026-59002"].RequestedRange != "3.9.8" {
		t.Fatalf("affected RequestedRange = %q, want 3.9.8", got["CVE-2026-59002"].RequestedRange)
	}

	assertSupplyChainImpactStatus(t, got["CVE-2026-59003"], SupplyChainImpactNotAffectedKnownFixed)
	if got["CVE-2026-59003"].MatchReason != "maven_known_fixed" {
		t.Fatalf("fixed MatchReason = %q, want maven_known_fixed", got["CVE-2026-59003"].MatchReason)
	}
	if got["CVE-2026-59003"].FixedVersion != "3.9.9" {
		t.Fatalf("fixed FixedVersion = %q, want 3.9.9", got["CVE-2026-59003"].FixedVersion)
	}
}

func TestBuildSupplyChainImpactFindingsExplainsNuGetLockfileExactVersion(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-nuget", "CVE-2026-59010", 8.1),
		vulnerabilityAffectedPackageRangeFact(
			"affected-nuget",
			"CVE-2026-59010",
			"pkg:nuget/newtonsoft.json",
			"nuget",
			"Newtonsoft.Json",
			"13.0.4",
		),
		nugetLockfileConsumptionFactWithChain(
			"consume-nuget",
			"pkg:nuget/newtonsoft.json",
			testImpactRepositoryID,
			"13.0.3",
			[]string{"Newtonsoft.Json"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-59010"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.ObservedVersion != "13.0.3" {
		t.Fatalf("ObservedVersion = %q, want exact NuGet lockfile version", got.ObservedVersion)
	}
	if got.MatchReason != "nuget_semver_affected_range" {
		t.Fatalf("MatchReason = %q, want nuget_semver_affected_range", got.MatchReason)
	}
	if got.DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("DetectionProfile = %q, want precise for exact NuGet lockfile match", got.DetectionProfile)
	}
}

func TestBuildSupplyChainImpactFindingsExplainsHexLockfileExactVersion(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-hex", "CVE-2026-1093", 8.4),
		vulnerabilityAffectedPackageRangeFact(
			"affected-hex",
			"CVE-2026-1093",
			"pkg:hex/phoenix_html",
			"hex",
			"phoenix_html",
			"4.2.2",
		),
		packageConsumptionFactWithChain(
			"consume-hex",
			"pkg:hex/phoenix_html",
			testImpactRepositoryID,
			"4.2.1",
			[]string{"phoenix_html"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-1093"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.ObservedVersion != "4.2.1" {
		t.Fatalf("ObservedVersion = %q, want exact Hex lockfile version", got.ObservedVersion)
	}
	if got.MatchReason != "hex_semver_affected_range" {
		t.Fatalf("MatchReason = %q, want hex_semver_affected_range", got.MatchReason)
	}
	if got.DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("DetectionProfile = %q, want precise for exact Hex lockfile match", got.DetectionProfile)
	}
}

func TestBuildSupplyChainImpactFindingsMarksHexLockfileKnownFixed(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-hex-fixed", "CVE-2026-1094", 8.4),
		vulnerabilityAffectedPackageRangeFact(
			"affected-hex-fixed",
			"CVE-2026-1094",
			"pkg:hex/jason",
			"hex",
			"jason",
			"1.4.3",
		),
		packageConsumptionFactWithChain(
			"consume-hex-fixed",
			"pkg:hex/jason",
			testImpactRepositoryID,
			"1.4.3",
			[]string{"jason"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-1094"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactNotAffectedKnownFixed)
	if got.MatchReason != "hex_semver_known_fixed" {
		t.Fatalf("MatchReason = %q, want hex_semver_known_fixed", got.MatchReason)
	}
	if got.FixedVersion != "1.4.3" {
		t.Fatalf("FixedVersion = %q, want first fixed Hex version", got.FixedVersion)
	}
	if got.DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("DetectionProfile = %q, want precise for known-fixed Hex lockfile match", got.DetectionProfile)
	}
}

func TestEvaluateNuGetSemverMatchAcceptsShortLockfileVersion(t *testing.T) {
	t.Parallel()

	decision := evaluateSupplyChainVersionMatch(
		"nuget",
		"13.0",
		"",
		"",
		[]supplyChainAffectedPackage{
			{
				affectedRanges: []supplyChainAffectedRange{
					{
						kind: "SEMVER",
						events: []supplyChainAffectedRangeEvent{
							{introduced: "0"},
							{fixed: "13.0.1"},
						},
					},
				},
			},
		},
	)

	if decision.Status != SupplyChainImpactAffectedExact {
		t.Fatalf("Status = %q, want affected exact for normalized short NuGet version", decision.Status)
	}
	if decision.Reason != supplyChainVersionReasonNuGetSemverAffectedRange {
		t.Fatalf("Reason = %q, want %q", decision.Reason, supplyChainVersionReasonNuGetSemverAffectedRange)
	}
}

func TestEvaluateNuGetSemverMatchAcceptsFourSegmentRevision(t *testing.T) {
	t.Parallel()

	decision := evaluateSupplyChainVersionMatch(
		"nuget",
		"1.2.3.4",
		"",
		"",
		[]supplyChainAffectedPackage{
			{
				affectedRanges: []supplyChainAffectedRange{
					{
						kind: "SEMVER",
						events: []supplyChainAffectedRangeEvent{
							{introduced: "1.2.3.3"},
							{fixed: "1.2.3.5"},
						},
					},
				},
			},
		},
	)

	if decision.Status != SupplyChainImpactAffectedExact {
		t.Fatalf("Status = %q, want affected exact for NuGet revision version", decision.Status)
	}
	if decision.Reason != supplyChainVersionReasonNuGetSemverAffectedRange {
		t.Fatalf("Reason = %q, want %q", decision.Reason, supplyChainVersionReasonNuGetSemverAffectedRange)
	}
}

func TestBuildSupplyChainImpactFindingsFailsClosedForUnsupportedAndMalformedRanges(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-unsupported", "CVE-2026-59004", 6.1),
		vulnerabilityAffectedPackageFact(
			"affected-unsupported",
			"CVE-2026-59004",
			"pkg:unsupported/example",
			"unsupported",
			"example",
			"1.0.0",
			"1.1.0",
		),
		packageConsumptionFactWithRange("consume-unsupported", "pkg:unsupported/example", testImpactRepositoryID, "2.0.0"),

		vulnerabilityCVEFact("cve-malformed", "CVE-2026-59005", 6.1),
		vulnerabilityAffectedPackageMalformedRangeFact(
			"affected-malformed",
			"CVE-2026-59005",
			"pkg:npm/malformed",
			"npm",
			"malformed",
			"not-a-version",
			"2.0.0",
		),
		packageConsumptionFactWithRange("consume-malformed", "pkg:npm/malformed", testImpactRepositoryID, "1.0.0"),
	})

	got := supplyChainImpactFindingsByCVE(findings)
	if _, ok := got["CVE-2026-59004"]; ok {
		t.Fatalf("unsupported ecosystem emitted finding %#v, want no user-facing impact finding", got["CVE-2026-59004"])
	}

	assertSupplyChainImpactStatus(t, got["CVE-2026-59005"], SupplyChainImpactPossiblyAffected)
	if got["CVE-2026-59005"].MatchReason != "malformed_advisory_range" {
		t.Fatalf("malformed MatchReason = %q, want malformed_advisory_range", got["CVE-2026-59005"].MatchReason)
	}
	assertContainsString(t, got["CVE-2026-59005"].MissingEvidence, "advisory version range malformed")
}

func TestBuildSupplyChainImpactFindingsReportsMalformedInstalledVersion(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-bad-installed", "CVE-2026-59008", 6.1),
		vulnerabilityAffectedPackageRangeFact(
			"affected-bad-installed",
			"CVE-2026-59008",
			"pkg:npm/bad-installed",
			"npm",
			"bad-installed",
			"2.0.0",
		),
		packageConsumptionFactWithRange(
			"consume-bad-installed",
			"pkg:npm/bad-installed",
			testImpactRepositoryID,
			"not-a-version",
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-59008"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.MatchReason != "installed_version_malformed" {
		t.Fatalf("MatchReason = %q, want installed_version_malformed", got.MatchReason)
	}
	assertContainsString(t, got.MissingEvidence, "installed package version malformed")
}

func TestBuildSupplyChainImpactFindingsKeepsRangeOnlyManifestSeparate(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-range-only", "CVE-2026-59006", 5.3),
		vulnerabilityAffectedPackageRangeFact(
			"affected-range-only",
			"CVE-2026-59006",
			"pkg:npm/vite",
			"npm",
			"vite",
			"6.4.2",
		),
		packageConsumptionFactWithRange("consume-range-only", "pkg:npm/vite", testImpactRepositoryID, "^5.4.11"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-59006"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.ObservedVersion != "" {
		t.Fatalf("ObservedVersion = %q, want blank for range-only manifest", got.ObservedVersion)
	}
	if got.RequestedRange != "^5.4.11" {
		t.Fatalf("RequestedRange = %q, want ^5.4.11", got.RequestedRange)
	}
	if got.MatchReason != "range_only_manifest" {
		t.Fatalf("MatchReason = %q, want range_only_manifest", got.MatchReason)
	}
}

func TestBuildSupplyChainImpactFindingsSupportsGitLabNotEqualRange(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-not-equal", "CVE-2026-59009", 7.2),
		vulnerabilityAffectedPackageRawRangeFact(
			"affected-not-equal",
			"CVE-2026-59009",
			"pkg:npm/not-equal",
			"npm",
			"not-equal",
			"!=1.2.3 <2.0.0",
			"2.0.0",
		),
		packageConsumptionFactWithRange("consume-not-equal", "pkg:npm/not-equal", testImpactRepositoryID, "1.2.4"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-59009"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.MatchReason != "npm_semver_affected_range" {
		t.Fatalf("MatchReason = %q, want npm_semver_affected_range", got.MatchReason)
	}
}

func TestBuildSupplyChainImpactFindingsMatchesSecondVulnerableRange(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-multi-range", "CVE-2026-59007", 7.2),
		vulnerabilityAffectedPackageRangeFact(
			"affected-old-range",
			"CVE-2026-59007",
			"pkg:npm/multi-range",
			"npm",
			"multi-range",
			"1.3.0",
		),
		vulnerabilityAffectedPackageRangeFact(
			"affected-current-range",
			"CVE-2026-59007",
			"pkg:npm/multi-range",
			"npm",
			"multi-range",
			"2.0.0",
		),
		packageConsumptionFactWithRange("consume-multi-range", "pkg:npm/multi-range", testImpactRepositoryID, "1.4.0"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-59007"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.MatchReason != "npm_semver_affected_range" {
		t.Fatalf("MatchReason = %q, want npm_semver_affected_range", got.MatchReason)
	}
}

func TestComparatorRangeContainsExcludesNotEqualVersion(t *testing.T) {
	t.Parallel()

	got, valid := comparatorRangeContains("!=1.2.3 <2.0.0", "1.2.3", compareOSVSemver)
	if !valid {
		t.Fatal("comparatorRangeContains valid = false, want true for supported != operator")
	}
	if got {
		t.Fatal("comparatorRangeContains matched excluded version 1.2.3")
	}
}

func TestMavenVersionCompareFollowsQualifierOrdering(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{name: "alpha release is before final release", left: "1.0.0-alpha", right: "1.0.0", want: -1},
		{name: "case insensitive qualifier", left: "1.0-ALPHA1", right: "1.0-alpha1", want: 0},
		{name: "numeric qualifier ordering", left: "1.0-foo2", right: "1.0-foo10", want: -1},
		{name: "service pack after release", left: "1-sp", right: "1", want: 1},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, valid := compareMavenVersion(tc.left, tc.right)
			if !valid {
				t.Fatalf("compareMavenVersion(%q, %q) valid = false", tc.left, tc.right)
			}
			if compareSign(got) != tc.want {
				t.Fatalf("compareMavenVersion(%q, %q) = %d, want sign %d", tc.left, tc.right, got, tc.want)
			}
		})
	}
}

func vulnerabilityAffectedPackageMavenRangeFact(
	factID string,
	cveID string,
	packageID string,
	affectedRange string,
	fixedVersion string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":         cveID,
			"package_id":     packageID,
			"ecosystem":      "maven",
			"package_name":   "org.apache.maven:maven-core",
			"affected_range": affectedRange,
			"fixed_versions": []any{fixedVersion},
		},
	}
}

func vulnerabilityAffectedPackageMalformedRangeFact(
	factID string,
	cveID string,
	packageID string,
	ecosystem string,
	name string,
	introduced string,
	fixedVersion string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":       cveID,
			"package_id":   packageID,
			"ecosystem":    ecosystem,
			"package_name": name,
			"fixed_versions": []any{
				fixedVersion,
			},
			"affected_ranges": []any{
				map[string]any{
					"type": "SEMVER",
					"events": []any{
						map[string]any{"introduced": introduced},
						map[string]any{"fixed": fixedVersion},
					},
				},
			},
		},
	}
}

func vulnerabilityAffectedPackageRawRangeFact(
	factID string,
	cveID string,
	packageID string,
	ecosystem string,
	name string,
	affectedRange string,
	fixedVersion string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":         cveID,
			"package_id":     packageID,
			"ecosystem":      ecosystem,
			"package_name":   name,
			"affected_range": affectedRange,
			"fixed_versions": []any{fixedVersion},
		},
	}
}

func assertContainsString(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return
		}
	}
	t.Fatalf("%#v does not contain %q", values, want)
}

func compareSign(value int) int {
	switch {
	case value < 0:
		return -1
	case value > 0:
		return 1
	default:
		return 0
	}
}
