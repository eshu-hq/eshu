// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsUsesRubyGemsLockfileVersion(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-rails", "CVE-2026-6480", 7.5),
		vulnerabilityAffectedPackageRangeFact(
			"affected-rails",
			"CVE-2026-6480",
			"pkg:gem/rails",
			"rubygems",
			"rails",
			"7.1.4",
		),
		rubyGemsPackageConsumptionFactWithInstalledVersion(
			"consume-rails",
			"pkg:gem/rails",
			testImpactRepositoryID,
			"~> 7.1",
			"7.1.3",
			[]string{"rails"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-6480"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.RepositoryID != testImpactRepositoryID {
		t.Fatalf("RepositoryID = %q, want %q", got.RepositoryID, testImpactRepositoryID)
	}
	if got.ObservedVersion != "7.1.3" {
		t.Fatalf("ObservedVersion = %q, want Bundler lockfile version 7.1.3", got.ObservedVersion)
	}
	if got.RequestedRange != "~> 7.1" {
		t.Fatalf("RequestedRange = %q, want Gemfile requested range ~> 7.1", got.RequestedRange)
	}
	if !reflect.DeepEqual(got.DependencyPath, []string{"rails"}) {
		t.Fatalf("DependencyPath = %#v, want rails", got.DependencyPath)
	}
	if got.DependencyDepth != 1 {
		t.Fatalf("DependencyDepth = %d, want 1", got.DependencyDepth)
	}
	if got.DirectDependency == nil || !*got.DirectDependency {
		t.Fatalf("DirectDependency = %#v, want true", got.DirectDependency)
	}
	if got.MatchReason != supplyChainVersionReasonRubyGemsAffectedRange {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonRubyGemsAffectedRange)
	}
	if got.DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("DetectionProfile = %q, want precise for exact RubyGems match", got.DetectionProfile)
	}
	assertSupplyChainReachability(
		t,
		got,
		SupplyChainReachabilityReachable,
		"bundler",
		"bundler_dependency_path",
	)
}

func TestBuildSupplyChainImpactFindingsMarksRubyGemsFixedVersionKnownFixed(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-fixed-rails", "CVE-2026-6481", 7.5),
		vulnerabilityAffectedPackageRangeFact(
			"affected-fixed-rails",
			"CVE-2026-6481",
			"pkg:gem/rails",
			"rubygems",
			"rails",
			"7.1.4",
		),
		packageConsumptionFactWithChain(
			"consume-fixed-rails",
			"pkg:gem/rails",
			testImpactRepositoryID,
			"7.1.4",
			[]string{"rails"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-6481"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactNotAffectedKnownFixed)
	if got.ObservedVersion != "7.1.4" {
		t.Fatalf("ObservedVersion = %q, want Bundler lockfile version 7.1.4", got.ObservedVersion)
	}
	if got.MatchReason != "rubygems_known_fixed" {
		t.Fatalf("MatchReason = %q, want rubygems_known_fixed", got.MatchReason)
	}
	if got.RuntimeReachability != "known_fixed" {
		t.Fatalf("RuntimeReachability = %q, want known_fixed", got.RuntimeReachability)
	}
}

func TestBuildSupplyChainImpactFindingsPrefersRubyGemsLockfileOverManifestRange(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-prefer-lockfile-rails", "CVE-2026-6483", 7.5),
		vulnerabilityAffectedPackageRangeFact(
			"affected-prefer-lockfile-rails",
			"CVE-2026-6483",
			"pkg:gem/rails",
			"rubygems",
			"rails",
			"7.1.4",
		),
		packageConsumptionFactWithRange(
			"consume-manifest-rails",
			"pkg:gem/rails",
			testImpactRepositoryID,
			"~> 7.1",
		),
		rubyGemsPackageConsumptionFactWithInstalledVersion(
			"consume-lockfile-rails",
			"pkg:gem/rails",
			testImpactRepositoryID,
			"~> 7.1",
			"7.1.3",
			[]string{"rails"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-6483"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.ObservedVersion != "7.1.3" {
		t.Fatalf("ObservedVersion = %q, want Bundler lockfile installed version 7.1.3", got.ObservedVersion)
	}
	if got.RequestedRange != "~> 7.1" {
		t.Fatalf("RequestedRange = %q, want Gemfile requested range ~> 7.1", got.RequestedRange)
	}
	if got.MatchReason != "rubygems_affected_range" {
		t.Fatalf("MatchReason = %q, want rubygems_affected_range", got.MatchReason)
	}
}

func TestBuildSupplyChainImpactFindingsMatchesRubyGemsFourSegmentVersions(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-actionpack", "CVE-2026-6482", 7.5),
		vulnerabilityAffectedPackageRangeFact(
			"affected-actionpack",
			"CVE-2026-6482",
			"pkg:gem/actionpack",
			"rubygems",
			"actionpack",
			"6.1.7.7",
		),
		packageConsumptionFactWithChain(
			"consume-actionpack",
			"pkg:gem/actionpack",
			testImpactRepositoryID,
			"6.1.7.6",
			[]string{"rails", "actionpack"},
			2,
			false,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-6482"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.MatchReason != "rubygems_affected_range" {
		t.Fatalf("MatchReason = %q, want rubygems_affected_range", got.MatchReason)
	}
	if !reflect.DeepEqual(got.DependencyPath, []string{"rails", "actionpack"}) {
		t.Fatalf("DependencyPath = %#v, want rails -> actionpack", got.DependencyPath)
	}
	if got.DirectDependency == nil || *got.DirectDependency {
		t.Fatalf("DirectDependency = %#v, want false for transitive Bundler chain", got.DirectDependency)
	}
}

func TestBuildSupplyChainImpactFindingsFailsClosedForMalformedRubyGemsInstalledVersion(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-malformed-rubygems-installed", "CVE-2026-6484", 7.5),
		vulnerabilityAffectedPackageRangeFact(
			"affected-malformed-rubygems-installed",
			"CVE-2026-6484",
			"pkg:gem/rails",
			"rubygems",
			"rails",
			"7.1.4",
		),
		rubyGemsPackageConsumptionFactWithInstalledVersion(
			"consume-malformed-rubygems-installed",
			"pkg:gem/rails",
			testImpactRepositoryID,
			"~> 7.1",
			"1a.0",
			[]string{"rails"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-6484"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.MatchReason != supplyChainVersionReasonMalformedInstalled {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonMalformedInstalled)
	}
	assertContainsString(t, got.MissingEvidence, supplyChainMissingMalformedInstalled)
}

func TestBuildSupplyChainImpactFindingsFailsClosedForMalformedRubyGemsAdvisoryRange(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-malformed-rubygems-range", "CVE-2026-6485", 7.5),
		vulnerabilityAffectedPackageMalformedRangeFact(
			"affected-malformed-rubygems-range",
			"CVE-2026-6485",
			"pkg:gem/rails",
			"rubygems",
			"rails",
			"not-a-version",
			"7.1.4",
		),
		rubyGemsPackageConsumptionFactWithInstalledVersion(
			"consume-malformed-rubygems-range",
			"pkg:gem/rails",
			testImpactRepositoryID,
			"~> 7.1",
			"7.1.3",
			[]string{"rails"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-6485"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.MatchReason != supplyChainVersionReasonMalformedRange {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonMalformedRange)
	}
	assertContainsString(t, got.MissingEvidence, supplyChainMissingMalformedRange)
}

func TestCompareRubyGemsVersionAcceptsHyphenPrerelease(t *testing.T) {
	t.Parallel()

	cmp, ok := compareRubyGemsVersion("1.0-rc1", "1.0")
	if !ok {
		t.Fatal("compareRubyGemsVersion valid = false, want true for RubyGems hyphen prerelease")
	}
	if cmp >= 0 {
		t.Fatalf("compareRubyGemsVersion(1.0-rc1, 1.0) = %d, want prerelease below release", cmp)
	}
}

func TestCompareRubyGemsVersionCanonicalizesPrereleaseZeroSegments(t *testing.T) {
	t.Parallel()

	cmp, ok := compareRubyGemsVersion("1.0.a", "1.a")
	if !ok {
		t.Fatal("compareRubyGemsVersion valid = false, want true for RubyGems prerelease")
	}
	if cmp != 0 {
		t.Fatalf("compareRubyGemsVersion(1.0.a, 1.a) = %d, want canonical equivalence", cmp)
	}
}

func TestCompareRubyGemsVersionRejectsNonGemVersionSyntax(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"v1.0", "a1.0", "1a.0"} {
		if _, ok := compareRubyGemsVersion(version, "1.0"); ok {
			t.Fatalf("compareRubyGemsVersion(%q, 1.0) valid = true, want false", version)
		}
	}
}

func rubyGemsPackageConsumptionFactWithInstalledVersion(
	factID string,
	packageID string,
	repositoryID string,
	dependencyRange string,
	installedVersion string,
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
	envelope.Payload["installed_version"] = installedVersion
	envelope.Payload["lockfile"] = true
	return envelope
}
