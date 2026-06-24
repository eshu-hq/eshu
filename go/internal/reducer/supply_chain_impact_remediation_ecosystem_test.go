// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestBuildSupplyChainImpactRemediationSupportsLanguageEcosystems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		finding           SupplyChainImpactFinding
		wantReason        string
		wantConfidence    string
		wantAllowsFix     string
		wantFirstPatched  string
		wantParentPackage string
		wantMissing       string
	}{
		{
			name: "go module direct require must be bumped",
			finding: remediationFinding("gomod", "v1.2.3", "v1.2.0", "<v1.2.4", "v1.2.4",
				supplyChainVersionReasonGoSemverAffectedRange, true, []string{"example.com/mod"}),
			wantReason:       SupplyChainRemediationReasonDirectRangeBlocked,
			wantConfidence:   SupplyChainRemediationConfidenceExact,
			wantAllowsFix:    SupplyChainRemediationManifestBlocked,
			wantFirstPatched: "v1.2.4",
		},
		{
			name: "pypi specifier range admits fix",
			finding: remediationFinding("pypi", "2.31.0", ">=2.31,<3", "<2.31.1", "2.31.1",
				supplyChainVersionReasonPyPIPep440AffectedRange, true, []string{"requests"}),
			wantReason:       SupplyChainRemediationReasonDirectUpgradeAllowed,
			wantConfidence:   SupplyChainRemediationConfidenceExact,
			wantAllowsFix:    SupplyChainRemediationManifestAllowed,
			wantFirstPatched: "2.31.1",
		},
		{
			name: "pypi manifest-only range preserves missing installed version",
			finding: remediationFinding("pypi", "", ">=2.31,<3", "<2.31.1", "2.31.1",
				supplyChainVersionReasonRangeOnlyManifest, true, []string{"requests"}),
			wantReason:       SupplyChainRemediationReasonDirectUpgradeAllowed,
			wantConfidence:   SupplyChainRemediationConfidenceExact,
			wantAllowsFix:    SupplyChainRemediationManifestAllowed,
			wantFirstPatched: "2.31.1",
			wantMissing:      SupplyChainRemediationMissingObservedVersion,
		},
		{
			name: "maven bracket range admits fix",
			finding: remediationFinding("maven", "3.9.8", "[3.8.0,4.0.0)", "[3.8.0,3.9.9)", "3.9.9",
				supplyChainVersionReasonMavenRangeMatch, true, []string{"org.apache.maven:maven-core"}),
			wantReason:       SupplyChainRemediationReasonDirectUpgradeAllowed,
			wantConfidence:   SupplyChainRemediationConfidenceExact,
			wantAllowsFix:    SupplyChainRemediationManifestAllowed,
			wantFirstPatched: "3.9.9",
		},
		{
			name: "gradle alias uses maven bracket semantics",
			finding: remediationFinding("gradle", "1.7.20", "[1.7.0,1.8.0)", "[1.7.0,1.7.21)", "1.7.21",
				supplyChainVersionReasonMavenRangeMatch, true, []string{"org.jetbrains.kotlin:kotlin-gradle-plugin"}),
			wantReason:       SupplyChainRemediationReasonDirectUpgradeAllowed,
			wantConfidence:   SupplyChainRemediationConfidenceExact,
			wantAllowsFix:    SupplyChainRemediationManifestAllowed,
			wantFirstPatched: "1.7.21",
		},
		{
			name: "nuget prerelease keeps semver ordering",
			finding: remediationFinding("nuget", "13.0.4-beta.1", "[13.0.0,14.0.0)", "[13.0.0,13.0.4)", "13.0.4",
				supplyChainVersionReasonNuGetSemverAffectedRange, true, []string{"Newtonsoft.Json"}),
			wantReason:       SupplyChainRemediationReasonDirectUpgradeAllowed,
			wantConfidence:   SupplyChainRemediationConfidenceExact,
			wantAllowsFix:    SupplyChainRemediationManifestAllowed,
			wantFirstPatched: "13.0.4",
		},
		{
			name: "cargo bare manifest requirement is caret-compatible",
			finding: remediationFinding("cargo", "1.0.116", "1.0.116", "<1.0.117", "1.0.117",
				supplyChainVersionReasonCargoSemverAffectedRange, true, []string{"serde"}),
			wantReason:       SupplyChainRemediationReasonDirectUpgradeAllowed,
			wantConfidence:   SupplyChainRemediationConfidenceExact,
			wantAllowsFix:    SupplyChainRemediationManifestAllowed,
			wantFirstPatched: "1.0.117",
		},
		{
			name: "composer constraint admits fix",
			finding: remediationFinding("composer", "2.4.1", "^2.4", "<2.4.3", "2.4.3",
				supplyChainVersionReasonComposerSemverAffectedRange, true, []string{"symfony/http-foundation"}),
			wantReason:       SupplyChainRemediationReasonDirectUpgradeAllowed,
			wantConfidence:   SupplyChainRemediationConfidenceExact,
			wantAllowsFix:    SupplyChainRemediationManifestAllowed,
			wantFirstPatched: "2.4.3",
		},
		{
			name: "rubygems transitive dependency names parent package",
			finding: remediationFinding("rubygems", "3.2.1", "~> 3.2.0", "<3.2.4", "3.2.4",
				supplyChainVersionReasonRubyGemsAffectedRange, false, []string{"rails", "rack"}),
			wantReason:        SupplyChainRemediationReasonTransitiveParentUpgrade,
			wantConfidence:    SupplyChainRemediationConfidencePartial,
			wantAllowsFix:     SupplyChainRemediationManifestUnknown,
			wantFirstPatched:  "3.2.4",
			wantParentPackage: "rails",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			remediation := BuildSupplyChainImpactRemediation(tc.finding)
			if remediation.Reason != tc.wantReason {
				t.Fatalf("Reason = %q, want %q; remediation=%#v", remediation.Reason, tc.wantReason, remediation)
			}
			if remediation.Confidence != tc.wantConfidence {
				t.Fatalf("Confidence = %q, want %q", remediation.Confidence, tc.wantConfidence)
			}
			if remediation.ManifestAllowsFix != tc.wantAllowsFix {
				t.Fatalf("ManifestAllowsFix = %q, want %q", remediation.ManifestAllowsFix, tc.wantAllowsFix)
			}
			if remediation.FirstPatchedVersion != tc.wantFirstPatched {
				t.Fatalf("FirstPatchedVersion = %q, want %q", remediation.FirstPatchedVersion, tc.wantFirstPatched)
			}
			if remediation.MatchReason != tc.finding.MatchReason {
				t.Fatalf("MatchReason = %q, want %q", remediation.MatchReason, tc.finding.MatchReason)
			}
			if remediation.FixedVersionSource != "ghsa" {
				t.Fatalf("FixedVersionSource = %q, want ghsa", remediation.FixedVersionSource)
			}
			if remediation.ParentPackage != tc.wantParentPackage {
				t.Fatalf("ParentPackage = %q, want %q", remediation.ParentPackage, tc.wantParentPackage)
			}
			if tc.wantMissing != "" && !containsRemediationMissingEvidence(remediation, tc.wantMissing) {
				t.Fatalf("MissingEvidence = %#v, want %q", remediation.MissingEvidence, tc.wantMissing)
			}
		})
	}
}

func TestBuildSupplyChainImpactRemediationSupportsMultipleFixedBranchesPerEcosystem(t *testing.T) {
	t.Parallel()

	finding := remediationFinding("maven", "1.2.0", "[1.0.0,3.0.0)", "[1.0.0,1.2.9)", "2.0.0",
		supplyChainVersionReasonMavenRangeMatch, true, []string{"org.example:lib"})
	finding.FixedVersionSource = "osv"
	finding.FixedVersionBranches = []FixedVersionBranch{
		{Version: "2.0.0", Source: "osv"},
		{Version: "1.2.9", Source: "ghsa"},
	}

	remediation := BuildSupplyChainImpactRemediation(finding)
	if remediation.Reason != SupplyChainRemediationReasonMultiplePatchedBranches {
		t.Fatalf("Reason = %q, want %q", remediation.Reason, SupplyChainRemediationReasonMultiplePatchedBranches)
	}
	if remediation.FirstPatchedVersion != "1.2.9" {
		t.Fatalf("FirstPatchedVersion = %q, want same-major branch 1.2.9", remediation.FirstPatchedVersion)
	}
	if remediation.FixedVersionSource != "ghsa" {
		t.Fatalf("FixedVersionSource = %q, want selected branch source ghsa", remediation.FixedVersionSource)
	}
	if remediation.Confidence != SupplyChainRemediationConfidencePartial {
		t.Fatalf("Confidence = %q, want partial", remediation.Confidence)
	}
	if got := remediationBranchVersions(remediation.PatchedVersionBranches); !got["1.2.9"] || !got["2.0.0"] {
		t.Fatalf("PatchedVersionBranches = %#v, want both 1.2.9 and 2.0.0", got)
	}
}

func TestBuildSupplyChainImpactRemediationSupportsVendorOSManagers(t *testing.T) {
	t.Parallel()

	rpm := remediationFinding("redhat", "1:3.0.7-18.el9_2", "", "1:3.0.7-18.el9_2", "1:3.0.7-19.el9_2",
		supplyChainVersionReasonRPMExactAffected, true, []string{"openssl"})
	rpm.FixedVersionSource = "redhat"
	rpm.FixedVersionBranches = []FixedVersionBranch{{Version: "1:3.0.7-19.el9_2", Source: "redhat"}}

	remediation := BuildSupplyChainImpactRemediation(rpm)
	if remediation.Reason != SupplyChainRemediationReasonDirectUpgradeAllowed {
		t.Fatalf("rpm Reason = %q, want direct_upgrade_allowed", remediation.Reason)
	}
	if remediation.Confidence != SupplyChainRemediationConfidenceExact {
		t.Fatalf("rpm Confidence = %q, want exact", remediation.Confidence)
	}
	if remediation.FirstPatchedVersion != "1:3.0.7-19.el9_2" {
		t.Fatalf("rpm FirstPatchedVersion = %q, want vendor EVR fix", remediation.FirstPatchedVersion)
	}
	if remediation.FixedVersionSource != "redhat" {
		t.Fatalf("rpm FixedVersionSource = %q, want redhat", remediation.FixedVersionSource)
	}

	dpkg := remediationFinding("debian", "1.2.3-1", "", "1.2.3-1", "1.2.3-2",
		supplyChainVersionReasonDPKGExactAffected, true, []string{"openssl"})
	dpkg.FixedVersionSource = "debian"
	dpkg.FixedVersionBranches = []FixedVersionBranch{{Version: "1.2.3-2", Source: "debian"}}

	dpkgRemediation := BuildSupplyChainImpactRemediation(dpkg)
	if dpkgRemediation.Reason != SupplyChainRemediationReasonDirectUpgradeAllowed {
		t.Fatalf("dpkg Reason = %q, want direct_upgrade_allowed", dpkgRemediation.Reason)
	}
	if dpkgRemediation.Confidence != SupplyChainRemediationConfidenceExact {
		t.Fatalf("dpkg Confidence = %q, want exact", dpkgRemediation.Confidence)
	}
	if dpkgRemediation.FirstPatchedVersion != "1.2.3-2" {
		t.Fatalf("dpkg FirstPatchedVersion = %q, want dpkg fixed version", dpkgRemediation.FirstPatchedVersion)
	}
	if containsRemediationMissingEvidence(dpkgRemediation, SupplyChainRemediationMissingEcosystemUnsupported) {
		t.Fatalf("dpkg MissingEvidence = %#v, did not expect unsupported ecosystem evidence", dpkgRemediation.MissingEvidence)
	}
}

func remediationFinding(
	ecosystem string,
	observed string,
	requested string,
	vulnerableRange string,
	fixed string,
	matchReason string,
	direct bool,
	path []string,
) SupplyChainImpactFinding {
	return SupplyChainImpactFinding{
		Ecosystem:          ecosystem,
		ObservedVersion:    observed,
		RequestedRange:     requested,
		VulnerableRange:    vulnerableRange,
		FixedVersion:       fixed,
		FixedVersionSource: "ghsa",
		MatchReason:        matchReason,
		DependencyPath:     path,
		DependencyDepth:    len(path),
		DirectDependency:   boolPtr(direct),
		FixedVersionBranches: []FixedVersionBranch{
			{Version: fixed, Source: "ghsa"},
		},
	}
}
