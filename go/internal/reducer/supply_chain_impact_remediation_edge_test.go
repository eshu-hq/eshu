package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildSupplyChainImpactFindingsRemediationPackageManagerUnsupported
// proves that OS package managers without proven distro version ordering
// report an unknown-confidence "package_manager_unsupported" remediation so
// callers see explicitly that Eshu has not yet computed a safe upgrade path
// for that ecosystem.
func TestBuildSupplyChainImpactFindingsRemediationPackageManagerUnsupported(t *testing.T) {
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

	if remediation.Reason != "package_manager_unsupported" {
		t.Fatalf("Reason = %q, want package_manager_unsupported", remediation.Reason)
	}
	if remediation.Confidence != "unknown" {
		t.Fatalf("Confidence = %q, want unknown", remediation.Confidence)
	}
	if remediation.Ecosystem != "debian" {
		t.Fatalf("Ecosystem = %q, want debian", remediation.Ecosystem)
	}
	if remediation.ManifestAllowsFix != "unknown" {
		t.Fatalf("ManifestAllowsFix = %q, want unknown for unsupported ecosystem", remediation.ManifestAllowsFix)
	}
	if !containsRemediationMissingEvidence(remediation, "ecosystem_remediation_unsupported") {
		t.Fatalf("MissingEvidence = %#v, want ecosystem_remediation_unsupported", remediation.MissingEvidence)
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
