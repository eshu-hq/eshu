// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsMatchesPyPISpecifierSets(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-pypi", "CVE-2026-59011", 8.1),
		vulnerabilityAffectedPackageRawRangeFact(
			"affected-pypi",
			"CVE-2026-59011",
			"pkg:pypi/django",
			"pypi",
			"django",
			">=1!4.2, <1!4.3, !=1!4.2.5",
			"1!4.3",
		),
		packageConsumptionFactWithChain(
			"consume-pypi",
			"pkg:pypi/django",
			testImpactRepositoryID,
			"1!4.2.6+local.1",
			[]string{"django"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-59011"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.MatchReason != "pypi_pep440_affected_range" {
		t.Fatalf("MatchReason = %q, want pypi_pep440_affected_range", got.MatchReason)
	}
	if got.ObservedVersion != "1!4.2.6+local.1" {
		t.Fatalf("ObservedVersion = %q, want PEP 440 epoch/local version", got.ObservedVersion)
	}
	if got.FixedVersion != "1!4.3" {
		t.Fatalf("FixedVersion = %q, want advisory fixed version", got.FixedVersion)
	}
}

func TestEvaluatePyPIMatchHandlesCompatibleReleaseAndPrerelease(t *testing.T) {
	t.Parallel()

	decision := evaluateSupplyChainVersionMatch(
		"pypi",
		"2.2.0rc1",
		"",
		"",
		[]supplyChainAffectedPackage{{affectedRangeRaw: "~=2.2.0rc1"}},
	)

	if decision.Status != SupplyChainImpactAffectedExact {
		t.Fatalf("Status = %q, want affected exact for compatible prerelease", decision.Status)
	}
	if decision.Reason != "pypi_pep440_affected_range" {
		t.Fatalf("Reason = %q, want pypi_pep440_affected_range", decision.Reason)
	}
}

func TestEvaluatePyPIMatchHandlesEpochCompatibleReleaseWithWhitespace(t *testing.T) {
	t.Parallel()

	decision := evaluateSupplyChainVersionMatch(
		"pypi",
		"1!4.2.1",
		"",
		"",
		[]supplyChainAffectedPackage{{affectedRangeRaw: "~= 1!4.2, != 1!4.2.5"}},
	)

	if decision.Status != SupplyChainImpactAffectedExact {
		t.Fatalf("Status = %q, want affected exact for epoch compatible release", decision.Status)
	}
	if decision.Reason != supplyChainVersionReasonPyPIPep440AffectedRange {
		t.Fatalf("Reason = %q, want %q", decision.Reason, supplyChainVersionReasonPyPIPep440AffectedRange)
	}
}

func TestEvaluatePyPIMatchFailsClosedForSingleSegmentCompatibleRelease(t *testing.T) {
	t.Parallel()

	decision := evaluateSupplyChainVersionMatch(
		"pypi",
		"1.2.0",
		"",
		"",
		[]supplyChainAffectedPackage{{affectedRangeRaw: "~=1"}},
	)

	if decision.Status != SupplyChainImpactPossiblyAffected {
		t.Fatalf("Status = %q, want possibly affected for malformed compatible release", decision.Status)
	}
	if decision.Reason != supplyChainVersionReasonMalformedRange {
		t.Fatalf("Reason = %q, want %q", decision.Reason, supplyChainVersionReasonMalformedRange)
	}
	assertContainsString(t, decision.MissingEvidence, supplyChainMissingMalformedRange)
}

func TestEvaluatePyPIMatchFailsClosedForLocalVersionSpecifier(t *testing.T) {
	t.Parallel()

	decision := evaluateSupplyChainVersionMatch(
		"pypi",
		"1.2.1+consumer.1",
		"",
		"",
		[]supplyChainAffectedPackage{{affectedRangeRaw: ">=1.2+advisory.1"}},
	)

	if decision.Status != SupplyChainImpactPossiblyAffected {
		t.Fatalf("Status = %q, want possibly affected for malformed local-version specifier", decision.Status)
	}
	if decision.Reason != supplyChainVersionReasonMalformedRange {
		t.Fatalf("Reason = %q, want %q", decision.Reason, supplyChainVersionReasonMalformedRange)
	}
	assertContainsString(t, decision.MissingEvidence, supplyChainMissingMalformedRange)
}

func TestBuildSupplyChainImpactFindingsMatchesGoModulePseudoVersions(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-gomod", "CVE-2026-59012", 7.9),
		vulnerabilityAffectedPackageRawRangeFact(
			"affected-gomod",
			"CVE-2026-59012",
			"pkg:golang/golang.org/x/crypto",
			"gomod",
			"golang.org/x/crypto",
			">=v0.0.0 <v0.0.0-20240203123456-bbbbbbbbbbbb",
			"v0.0.0-20240203123456-bbbbbbbbbbbb",
		),
		packageConsumptionFactWithChain(
			"consume-gomod",
			"pkg:golang/golang.org/x/crypto",
			testImpactRepositoryID,
			"v0.0.0-20240202123456-abcdef123456",
			[]string{"golang.org/x/crypto"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-59012"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.MatchReason != "go_semver_affected_range" {
		t.Fatalf("MatchReason = %q, want go_semver_affected_range", got.MatchReason)
	}
}

func TestBuildSupplyChainImpactFindingsMatchesComposerConstraints(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-composer", "CVE-2026-59013", 8.4),
		vulnerabilityAffectedPackageRawRangeFact(
			"affected-composer",
			"CVE-2026-59013",
			"pkg:composer/symfony/http-foundation",
			"composer",
			"symfony/http-foundation",
			"^6.0 || ~5.4.0",
			"6.4.12",
		),
		packageConsumptionFactWithChain(
			"consume-composer",
			"pkg:composer/symfony/http-foundation",
			testImpactRepositoryID,
			"5.4.7",
			[]string{"symfony/http-foundation"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-59013"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.MatchReason != supplyChainVersionReasonComposerSemverAffectedRange {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonComposerSemverAffectedRange)
	}
}

func TestEvaluateComposerMatchFailsClosedForBranchAlias(t *testing.T) {
	t.Parallel()

	decision := evaluateSupplyChainVersionMatch(
		"composer",
		"dev-main",
		"",
		"",
		[]supplyChainAffectedPackage{{affectedRangeRaw: "dev-main as 1.0.x-dev"}},
	)

	if decision.Status != SupplyChainImpactPossiblyAffected {
		t.Fatalf("Status = %q, want possibly affected for branch alias", decision.Status)
	}
	if decision.Reason != supplyChainVersionReasonMalformedInstalled {
		t.Fatalf("Reason = %q, want %q", decision.Reason, supplyChainVersionReasonMalformedInstalled)
	}
	assertContainsString(t, decision.MissingEvidence, supplyChainMissingMalformedInstalled)
}

func TestBuildSupplyChainImpactFindingsMatchesRubyGemsPessimisticConstraint(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-ruby", "CVE-2026-59014", 7.1),
		vulnerabilityAffectedPackageRawRangeFact(
			"affected-ruby",
			"CVE-2026-59014",
			"pkg:gem/rails",
			"rubygems",
			"rails",
			"~> 7.1.0",
			"7.1.4",
		),
		packageConsumptionFactWithChain(
			"consume-ruby",
			"pkg:gem/rails",
			testImpactRepositoryID,
			"7.1.3",
			[]string{"rails"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-59014"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.MatchReason != supplyChainVersionReasonRubyGemsAffectedRange {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonRubyGemsAffectedRange)
	}
}

func TestEvaluateRubyGemsMatchHandlesPrereleaseRequirement(t *testing.T) {
	t.Parallel()

	decision := evaluateSupplyChainVersionMatch(
		"rubygems",
		"2.2.beta.12",
		"",
		"",
		[]supplyChainAffectedPackage{{affectedRangeRaw: "~> 2.2.beta"}},
	)

	if decision.Status != SupplyChainImpactAffectedExact {
		t.Fatalf("Status = %q, want affected exact for prerelease requirement", decision.Status)
	}
	if decision.Reason != supplyChainVersionReasonRubyGemsAffectedRange {
		t.Fatalf("Reason = %q, want %q", decision.Reason, supplyChainVersionReasonRubyGemsAffectedRange)
	}
}
