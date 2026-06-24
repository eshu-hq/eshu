// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsProvesPyPILockfileExactVersions(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-pypi-affected", "CVE-2026-99901", 8.4),
		vulnerabilityAffectedPackageRangeFact(
			"affected-pypi-requests",
			"CVE-2026-99901",
			"pkg:pypi/requests",
			"pypi",
			"requests",
			"2.32.0",
		),
		packageConsumptionFactWithChain(
			"consume-pypi-requests",
			"pkg:pypi/requests",
			testImpactRepositoryID,
			"2.31.0",
			[]string{"requests"},
			1,
			true,
		),

		vulnerabilityCVEFact("cve-pypi-fixed", "CVE-2026-99902", 8.4),
		vulnerabilityAffectedPackageRangeFact(
			"affected-pypi-jinja",
			"CVE-2026-99902",
			"pkg:pypi/jinja2",
			"PyPI",
			"Jinja2",
			"3.1.5",
		),
		packageConsumptionFactWithChain(
			"consume-pypi-jinja",
			"pkg:pypi/jinja2",
			testImpactRepositoryID,
			"3.1.5",
			[]string{"Jinja2"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)
	assertSupplyChainImpactStatus(t, got["CVE-2026-99901"], SupplyChainImpactAffectedExact)
	if got["CVE-2026-99901"].ObservedVersion != "2.31.0" {
		t.Fatalf("affected ObservedVersion = %q, want exact PyPI lockfile version", got["CVE-2026-99901"].ObservedVersion)
	}
	if got["CVE-2026-99901"].MatchReason != "pypi_pep440_affected_range" {
		t.Fatalf("affected MatchReason = %q, want pypi_pep440_affected_range", got["CVE-2026-99901"].MatchReason)
	}
	if got["CVE-2026-99901"].DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("affected DetectionProfile = %q, want precise", got["CVE-2026-99901"].DetectionProfile)
	}

	assertSupplyChainImpactStatus(t, got["CVE-2026-99902"], SupplyChainImpactNotAffectedKnownFixed)
	if got["CVE-2026-99902"].MatchReason != "pypi_pep440_known_fixed" {
		t.Fatalf("fixed MatchReason = %q, want pypi_pep440_known_fixed", got["CVE-2026-99902"].MatchReason)
	}
	if got["CVE-2026-99902"].DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("fixed DetectionProfile = %q, want precise", got["CVE-2026-99902"].DetectionProfile)
	}
}

func TestBuildSupplyChainImpactFindingsKeepsPyPIManifestRangesPossible(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-pypi-range", "CVE-2026-99903", 6.2),
		vulnerabilityAffectedPackageRangeFact(
			"affected-pypi-range",
			"CVE-2026-99903",
			"pkg:pypi/urllib3",
			"pypi",
			"urllib3",
			"2.5.0",
		),
		packageConsumptionFactWithChain(
			"consume-pypi-range",
			"pkg:pypi/urllib3",
			testImpactRepositoryID,
			">=2.0,<3",
			[]string{"urllib3"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-99903"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.ObservedVersion != "" {
		t.Fatalf("ObservedVersion = %q, want blank for manifest-only range", got.ObservedVersion)
	}
	if got.MatchReason != supplyChainVersionReasonRangeOnlyManifest {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonRangeOnlyManifest)
	}
	if got.DetectionProfile != DetectionProfileComprehensive {
		t.Fatalf("DetectionProfile = %q, want comprehensive for range-only manifest", got.DetectionProfile)
	}
	assertContainsString(t, got.MissingEvidence, supplyChainMissingInstalledVersion)
}
