// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildSupplyChainImpactFindingsRemediationDPKGDirectUpgrade proves that
// vendor-proven Debian remediation uses dpkg version ordering and can emit an
// exact direct-upgrade recommendation instead of remaining unsupported.
func TestBuildSupplyChainImpactFindingsRemediationDPKGDirectUpgrade(t *testing.T) {
	t.Parallel()

	remediation := BuildSupplyChainImpactRemediation(SupplyChainImpactFinding{
		Ecosystem:          "debian",
		ObservedVersion:    "1.2.3-1",
		FixedVersion:       "1.2.3-2",
		FixedVersionSource: "debian",
		MatchReason:        supplyChainVersionReasonDPKGExactAffected,
		DirectDependency:   boolPtr(true),
		DependencyPath:     []string{"openssl"},
		FixedVersionBranches: []FixedVersionBranch{
			{Version: "1.2.3-2", Source: "debian"},
		},
	})

	if remediation.Reason != "direct_upgrade_allowed" {
		t.Fatalf("Reason = %q, want direct_upgrade_allowed", remediation.Reason)
	}
	if remediation.Confidence != "exact" {
		t.Fatalf("Confidence = %q, want exact", remediation.Confidence)
	}
	if remediation.Ecosystem != "debian" {
		t.Fatalf("Ecosystem = %q, want debian", remediation.Ecosystem)
	}
	if remediation.ManifestAllowsFix != SupplyChainRemediationManifestUnknown {
		t.Fatalf("ManifestAllowsFix = %q, want unknown for OS package remediation", remediation.ManifestAllowsFix)
	}
	if remediation.FirstPatchedVersion != "1.2.3-2" {
		t.Fatalf("FirstPatchedVersion = %q, want 1.2.3-2", remediation.FirstPatchedVersion)
	}
	if containsRemediationMissingEvidence(remediation, SupplyChainRemediationMissingEcosystemUnsupported) {
		t.Fatalf("MissingEvidence = %#v, did not expect unsupported ecosystem", remediation.MissingEvidence)
	}
}

// TestBuildSupplyChainImpactFindingsRemediationInstalledVersionMissing proves
// that an npm finding whose advisory publishes patches across more than one
// major and whose package consumption evidence carries only a manifest range
// is classified as installed_version_missing instead of silently selecting the
// lowest fix across all majors.
func TestBuildSupplyChainImpactFindingsRemediationInstalledVersionMissing(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-missing-installed-ghsa", "CVE-2026-90008", 7.0),
		multiSourceAffectedPackageFact(
			"affected-missing-installed-ghsa",
			"CVE-2026-90008",
			"pkg:npm/example",
			"npm",
			"example",
			"ghsa",
			"GHSA-missing-1",
			[]string{"1.3.5"},
			"<1.3.5",
		),
		vulnerabilityCVEFact("cve-missing-installed-osv", "CVE-2026-90008", 7.0),
		multiSourceAffectedPackageFact(
			"affected-missing-installed-osv",
			"CVE-2026-90008",
			"pkg:npm/example",
			"npm",
			"example",
			"osv",
			"GHSA-missing-2",
			[]string{"2.0.0"},
			"<2.0.0",
		),
		packageConsumptionFactWithChain(
			"consume-missing-installed",
			"pkg:npm/example",
			testImpactRepositoryID,
			"^1.2.0",
			[]string{"example"},
			1,
			true,
		),
	})

	finding := supplyChainImpactFindingsByCVE(findings)["CVE-2026-90008"]
	if finding.ObservedVersion != "" {
		t.Fatalf("ObservedVersion = %q, want empty for range-only manifest fixture", finding.ObservedVersion)
	}
	remediation := finding.Remediation
	if remediation.Reason != "installed_version_missing" {
		t.Fatalf("Reason = %q, want installed_version_missing", remediation.Reason)
	}
	if remediation.Confidence != "unknown" {
		t.Fatalf("Confidence = %q, want unknown when multiple patched branches exist without an installed version", remediation.Confidence)
	}
	if remediation.FirstPatchedVersion != "" {
		t.Fatalf("FirstPatchedVersion = %q, want empty when Eshu cannot anchor the branch selector to an installed major", remediation.FirstPatchedVersion)
	}
	if remediation.ManifestAllowsFix != "unknown" {
		t.Fatalf("ManifestAllowsFix = %q, want unknown when observed version is missing", remediation.ManifestAllowsFix)
	}
	if !containsRemediationMissingEvidence(remediation, "observed_version_missing") {
		t.Fatalf("MissingEvidence = %#v, want observed_version_missing", remediation.MissingEvidence)
	}
}

// TestBuildSupplyChainImpactFindingsRemediationInstalledVersionMalformed proves
// that an npm finding whose observed version cannot be parsed is classified as
// installed_version_malformed instead of emitting a misleading direct upgrade.
func TestBuildSupplyChainImpactFindingsRemediationInstalledVersionMalformed(t *testing.T) {
	t.Parallel()

	remediation := BuildSupplyChainImpactRemediation(SupplyChainImpactFinding{
		Ecosystem:        "npm",
		ObservedVersion:  "not-a-version",
		RequestedRange:   "^1.2.0",
		FixedVersion:     "1.3.0",
		DependencyPath:   []string{"example"},
		DependencyDepth:  1,
		DirectDependency: boolPtr(true),
		FixedVersionBranches: []FixedVersionBranch{
			{Version: "1.3.0", Source: "ghsa"},
		},
	})

	if remediation.Reason != "installed_version_malformed" {
		t.Fatalf("Reason = %q, want installed_version_malformed", remediation.Reason)
	}
	if remediation.Confidence != "unknown" {
		t.Fatalf("Confidence = %q, want unknown when observed version is malformed", remediation.Confidence)
	}
	if remediation.ManifestAllowsFix != "unknown" {
		t.Fatalf("ManifestAllowsFix = %q, want unknown when observed version is malformed", remediation.ManifestAllowsFix)
	}
	if !containsRemediationMissingEvidence(remediation, "installed_version_malformed") {
		t.Fatalf("MissingEvidence = %#v, want installed_version_malformed", remediation.MissingEvidence)
	}
}
