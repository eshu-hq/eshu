// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsRemediationSupportsVendorDPKGBranch(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFactWithProvenance(
			"debian-remediation-cve",
			"CVE-2026-119301",
			"debian",
			"DSA-2026-119301",
			7.5,
			"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
			"HIGH",
			"2026-06-01T12:00:00Z",
		),
		vulnerabilityAffectedPackageFactWithSource(
			"debian-remediation-affected",
			"CVE-2026-119301",
			"debian",
			"DSA-2026-119301",
			"pkg:deb/debian/openssl",
			"deb",
			"openssl",
			"3.0.11-1~deb12u2",
			"3.0.11-1~deb12u3",
		),
		osPackageFact("dpkg-remediation-openssl", "image://registry.example/debian-remediation@sha256:119301", map[string]any{
			"distro":                 "debian",
			"distro_version":         "12",
			"package_manager":        "dpkg",
			"name":                   "openssl",
			"arch":                   "amd64",
			"repository_class":       "vendor",
			"vendor_advisory_source": "debian",
			"installed_version_raw":  "3.0.11-1~deb12u2",
			"purl":                   "pkg:deb/debian/openssl@3.0.11-1~deb12u2?arch=amd64&distro=debian-12",
		}),
	})

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want one Debian OS package finding: %#v", len(findings), findings)
	}
	remediation := findings[0].Remediation
	if remediation.Reason != SupplyChainRemediationReasonDirectUpgradeAllowed {
		t.Fatalf("Reason = %q, want direct_upgrade_allowed", remediation.Reason)
	}
	if remediation.Confidence != SupplyChainRemediationConfidenceExact {
		t.Fatalf("Confidence = %q, want exact", remediation.Confidence)
	}
	if remediation.FirstPatchedVersion != "3.0.11-1~deb12u3" {
		t.Fatalf("FirstPatchedVersion = %q, want Debian fixed backport", remediation.FirstPatchedVersion)
	}
	if remediation.FixedVersionSource != "debian" {
		t.Fatalf("FixedVersionSource = %q, want debian", remediation.FixedVersionSource)
	}
	if remediation.ManifestAllowsFix != SupplyChainRemediationManifestUnknown {
		t.Fatalf("ManifestAllowsFix = %q, want unknown for OS package remediation", remediation.ManifestAllowsFix)
	}
	if containsRemediationMissingEvidence(remediation, SupplyChainRemediationMissingEcosystemUnsupported) {
		t.Fatalf("MissingEvidence = %#v, did not expect unsupported ecosystem", remediation.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactFindingsRemediationSupportsVendorAPKBranch(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFactWithProvenance(
			"alpine-remediation-cve",
			"CVE-2026-119302",
			"alpine",
			"ALPINE-2026-119302",
			8.1,
			"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:N",
			"HIGH",
			"2026-06-01T12:00:00Z",
		),
		vulnerabilityAffectedPackageFactWithSource(
			"alpine-remediation-affected",
			"CVE-2026-119302",
			"alpine",
			"ALPINE-2026-119302",
			"pkg:apk/alpine/openssl",
			"apk",
			"openssl",
			"3.1.4-r5",
			"3.1.4-r6",
		),
		osPackageFact("apk-remediation-openssl", "image://registry.example/alpine-remediation@sha256:119302", map[string]any{
			"distro":                 "alpine",
			"distro_version":         "3.19.1",
			"package_manager":        "apk",
			"name":                   "openssl",
			"arch":                   "x86_64",
			"repository_class":       "vendor",
			"vendor_advisory_source": "alpine",
			"installed_version_raw":  "3.1.4-r5",
			"purl":                   "pkg:apk/alpine/openssl@3.1.4-r5?arch=x86_64&distro=alpine-3.19.1",
		}),
	})

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want one Alpine OS package finding: %#v", len(findings), findings)
	}
	remediation := findings[0].Remediation
	if remediation.Reason != SupplyChainRemediationReasonDirectUpgradeAllowed {
		t.Fatalf("Reason = %q, want direct_upgrade_allowed", remediation.Reason)
	}
	if remediation.Confidence != SupplyChainRemediationConfidenceExact {
		t.Fatalf("Confidence = %q, want exact", remediation.Confidence)
	}
	if remediation.FirstPatchedVersion != "3.1.4-r6" {
		t.Fatalf("FirstPatchedVersion = %q, want Alpine fixed revision", remediation.FirstPatchedVersion)
	}
	if remediation.FixedVersionSource != "alpine" {
		t.Fatalf("FixedVersionSource = %q, want alpine", remediation.FixedVersionSource)
	}
}

func TestBuildSupplyChainImpactRemediationRequiresOSAdvisorySourceProvenance(t *testing.T) {
	t.Parallel()

	finding := remediationFinding("debian", "1.2.3-1", "", "1.2.3-1", "1.2.3-2",
		supplyChainVersionReasonDPKGExactAffected, true, []string{"openssl"})
	finding.FixedVersionSource = "ghsa"
	finding.FixedVersionBranches = []FixedVersionBranch{{Version: "1.2.3-2", Source: "ghsa"}}

	remediation := BuildSupplyChainImpactRemediation(finding)
	if remediation.Reason != SupplyChainRemediationReasonPackageManagerUnsupported {
		t.Fatalf("Reason = %q, want package_manager_unsupported", remediation.Reason)
	}
	if remediation.FirstPatchedVersion != "" {
		t.Fatalf("FirstPatchedVersion = %q, want blank without vendor advisory provenance", remediation.FirstPatchedVersion)
	}
	if !containsRemediationMissingEvidence(remediation, SupplyChainRemediationMissingAdvisoryProvenance) {
		t.Fatalf("MissingEvidence = %#v, want advisory provenance missing", remediation.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactRemediationRejectsAmbiguousOSFixedBranches(t *testing.T) {
	t.Parallel()

	finding := remediationFinding("alpine", "3.1.4-r5", "", "3.1.4-r5", "3.1.4-r6",
		supplyChainVersionReasonAPKExactAffected, true, []string{"openssl"})
	finding.FixedVersionSource = "alpine"
	finding.FixedVersionBranches = []FixedVersionBranch{
		{Version: "3.1.4-r6", Source: "alpine"},
		{Version: "3.2.0-r0", Source: "alpine"},
	}

	remediation := BuildSupplyChainImpactRemediation(finding)
	if remediation.Reason != SupplyChainRemediationReasonPackageManagerUnsupported {
		t.Fatalf("Reason = %q, want package_manager_unsupported", remediation.Reason)
	}
	if remediation.FirstPatchedVersion != "" {
		t.Fatalf("FirstPatchedVersion = %q, want blank for ambiguous OS fixed branches", remediation.FirstPatchedVersion)
	}
	if !containsRemediationMissingEvidence(remediation, SupplyChainRemediationMissingFixedBranchAmbiguous) {
		t.Fatalf("MissingEvidence = %#v, want fixed branch ambiguity evidence", remediation.MissingEvidence)
	}
}
