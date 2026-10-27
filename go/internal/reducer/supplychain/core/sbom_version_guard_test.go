// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSBOMPathUsesPackageVersionDecision proves attachment evidence cannot
// promote an unproved installed package version to an affected finding.
func TestSBOMPathUsesPackageVersionDecision(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, fieldVersion, purl, status, reason, observed string
	}{
		{"outside range", "4.17.5", "pkg:npm/lodash@4.17.5", "possibly_affected", "version_not_in_advisory_range", "4.17.5"},
		{"inside range", "4.17.12", "pkg:npm/lodash@4.17.12", "affected_exact", "npm_semver_affected_range", "4.17.12"},
		{"exactly fixed", "4.17.21", "pkg:npm/lodash@4.17.21", "not_affected_known_fixed", "npm_semver_known_fixed", "4.17.21"},
		{"missing version", "", "pkg:npm/lodash", "possibly_affected", "installed_version_missing", ""},
		{"purl fallback", "", "pkg:npm/lodash@4.17.12", "affected_exact", "npm_semver_affected_range", "4.17.12"},
		{"malformed version", "bogus", "pkg:npm/lodash@bogus", "possibly_affected", "installed_version_malformed", "bogus"},
		{"equivalent spellings", "v4.17.12", "pkg:npm/lodash@4.17.12", "affected_exact", "npm_semver_affected_range", "v4.17.12"},
		{"contradictory versions", "4.17.5", "pkg:npm/lodash@4.17.12", "possibly_affected", "sbom_component_version_conflict", "4.17.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := sbomVersionGuardFinding(tc.fieldVersion, tc.purl)
			assertSupplyChainImpactStatus(t, got, SupplyChainImpactStatus(tc.status))
			if got.MatchReason != tc.reason || got.ObservedVersion != tc.observed {
				t.Fatalf("MatchReason=%q ObservedVersion=%q, want %q %q", got.MatchReason, got.ObservedVersion, tc.reason, tc.observed)
			}
			wantReachability := "image_sbom"
			if tc.status == "not_affected_known_fixed" {
				wantReachability = "known_fixed"
			}
			if got.RuntimeReachability != wantReachability || got.SubjectDigest != testImpactSubjectDigest {
				t.Fatalf("image provenance lost: reachability=%q digest=%q", got.RuntimeReachability, got.SubjectDigest)
			}
			if !slices.Contains(got.EvidenceFactIDs, "component-version") || !slices.Contains(got.EvidenceFactIDs, "attachment-version") || !slices.Contains(got.EvidenceFactIDs, "image-version") {
				t.Fatalf("SBOM evidence lost: %#v", got.EvidenceFactIDs)
			}
			wantProfile := DetectionProfileComprehensive
			if tc.status == "affected_exact" || tc.status == "not_affected_known_fixed" {
				wantProfile = DetectionProfilePrecise
			}
			if got.DetectionProfile != wantProfile {
				t.Fatalf("DetectionProfile=%q, want %q", got.DetectionProfile, wantProfile)
			}
			if tc.reason == supplyChainVersionReasonSBOMVersionConflict &&
				!slices.Contains(got.MissingEvidence, supplyChainMissingSBOMVersionConflict) {
				t.Fatalf("conflict missing from evidence: %#v", got.MissingEvidence)
			}
		})
	}
}

func sbomVersionGuardFinding(fieldVersion, purl string) SupplyChainImpactFinding {
	findings := BuildSupplyChainImpactFindings(sbomVersionGuardEnvelopes(fieldVersion, purl))
	return supplyChainImpactFindingsByCVE(findings)["CVE-2026-91913"]
}

func sbomVersionGuardEnvelopes(fieldVersion, purl string) []facts.Envelope {
	affected := vulnerabilityAffectedPackageMalformedRangeFact(
		"affected-version", "CVE-2026-91913", crossScopeAffectedPackageID,
		"npm", "lodash", "4.17.11", "4.17.21",
	)
	affected.Payload["purl"] = crossScopeAffectedPURL
	component := sbomComponentImpactFactWithPackageID("component-version", "doc-version", purl, crossScopeAffectedPackageID)
	component.Payload["version"] = fieldVersion
	return []facts.Envelope{
		vulnerabilityCVEFact("cve-version", "CVE-2026-91913", 8.0),
		affected,
		component,
		sbomAttachmentImpactFact("attachment-version", "doc-version", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-version", testImpactSubjectDigest, testImpactRepositoryID),
	}
}

// TestSBOMPathConsumptionConflictKeepsPartialTruth preserves both observations
// without attributing an unassociated package manifest to an image.
func TestSBOMPathConsumptionConflictKeepsPartialTruth(t *testing.T) {
	t.Parallel()
	base := sbomVersionGuardEnvelopes("4.17.5", "pkg:npm/lodash@4.17.5")
	consumption := packageConsumptionFactWithRange("consume-version", crossScopeAffectedPackageID, testImpactRepositoryID, "4.17.12")
	for _, reverse := range []bool{false, true} {
		input := append(append([]facts.Envelope(nil), base...), consumption)
		if reverse {
			slices.Reverse(input)
		}
		findings := BuildSupplyChainImpactFindings(input)
		if len(findings) != 2 {
			t.Fatalf("reverse=%t findings=%#v", reverse, findings)
		}
		image := supplyChainImpactFindingForDigest(t, findings, testImpactSubjectDigest)
		if image.Status != SupplyChainImpactPossiblyAffected || image.MatchReason != "version_not_in_advisory_range" || slices.Contains(image.EvidenceFactIDs, consumption.FactID) {
			t.Fatalf("reverse=%t image borrowed manifest: %#v", reverse, image)
		}
		manifest := supplyChainImpactFindingForDigest(t, findings, "")
		if manifest.Status != SupplyChainImpactAffectedExact || !slices.Contains(manifest.EvidenceFactIDs, consumption.FactID) {
			t.Fatalf("reverse=%t manifest lost: %#v", reverse, manifest)
		}
	}
}

func TestScopedPURLVersionSplit(t *testing.T) {
	t.Parallel()
	if got := versionFromPURL("pkg:npm/@scope/name"); got != "" {
		t.Fatalf("versionless scoped purl version=%q", got)
	}
	if got := packageIDFromPURL("pkg:npm/@scope/name@1.2.3"); got != "pkg:npm/@scope/name" {
		t.Fatalf("scoped purl package ID=%q", got)
	}
	if got := versionFromPURL("pkg:npm/@scope/name@1.2.3?arch=x#sub"); got != "1.2.3" {
		t.Fatalf("scoped purl version=%q", got)
	}
}

// TestSBOMExactVersionDoesNotBorrowRangeOnlyConsumptionProof keeps the image
// source when a manifest names only a range rather than an installed version.
func TestSBOMExactVersionDoesNotBorrowRangeOnlyConsumptionProof(t *testing.T) {
	t.Parallel()
	envelopes := sbomVersionGuardEnvelopes("4.17.12", "pkg:npm/lodash@4.17.12")
	consumption := packageConsumptionFactWithRange(
		"consume-range", crossScopeAffectedPackageID, testImpactRepositoryID, "^4.17.0",
	)
	envelopes = append(envelopes, consumption)
	got := supplyChainImpactFindingForDigest(t, BuildSupplyChainImpactFindings(envelopes), testImpactSubjectDigest)
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.RuntimeReachability != "image_sbom" || got.RepositoryID != testImpactRepositoryID {
		t.Fatalf("range-only consumption borrowed precision: reachability=%q repository=%q", got.RuntimeReachability, got.RepositoryID)
	}
}

// TestSBOMPathRetainsVendorOSVersionAuthority keeps vendor-backed scan proof
// authoritative when the same package also appears in an attached SBOM.
func TestSBOMPathRetainsVendorOSVersionAuthority(t *testing.T) {
	t.Parallel()
	const packageID = "pkg:rpm/redhat/openssl"
	facts := []facts.Envelope{
		vulnerabilityCVEFactWithProvenance(
			"cve-rpm-sbom", "CVE-2026-91914", "redhat", "RHSA-2026:91914", 7.8,
			"CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:H/A:H", "HIGH", "2026-05-31T12:00:00Z",
		),
		vulnerabilityAffectedPackageFactWithSource(
			"affected-rpm-sbom", "CVE-2026-91914", "redhat", "RHSA-2026:91914",
			packageID, "redhat", "openssl", "1:3.0.7-18.el9_2", "1:3.0.7-20.el9_2",
		),
		rpmOSPackageFact("os-rpm-sbom", map[string]any{
			"distro": "redhat", "distro_version": "9.2", "package_manager": "rpm",
			"name": "openssl", "epoch": "1", "upstream_version": "3.0.7",
			"distro_release": "18.el9_2", "arch": "x86_64", "source_package": "openssl",
			"repository_class": "vendor", "vendor_advisory_source": "redhat",
			"installed_version_raw": "1:3.0.7-18.el9_2",
			"purl":                  "pkg:rpm/redhat/openssl@1:3.0.7-18.el9_2?arch=x86_64&distro=redhat-9.2",
		}),
		sbomComponentImpactFactWithPackageIDVersion(
			"component-rpm-sbom", "doc-rpm-sbom", "pkg:rpm/redhat/openssl@1:3.0.7-18.el9_2",
			packageID, "1:3.0.7-18.el9_2",
		),
		sbomAttachmentImpactFact("attachment-rpm-sbom", "doc-rpm-sbom", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-rpm-sbom", testImpactSubjectDigest, testImpactRepositoryID),
	}
	findings := BuildSupplyChainImpactFindings(facts)
	if len(findings) != 2 {
		t.Fatalf("findings=%#v, want separate SBOM and OS observations", findings)
	}
	got := supplyChainImpactFindingForDigest(t, findings, "")
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.RuntimeReachability != "image_os_package" || got.ObservedVersion != "1:3.0.7-18.el9_2" {
		t.Fatalf("vendor OS version proof lost: reachability=%q version=%q", got.RuntimeReachability, got.ObservedVersion)
	}
	if slices.Contains(got.EvidenceFactIDs, "component-rpm-sbom") || !slices.Contains(got.EvidenceFactIDs, "os-rpm-sbom") {
		t.Fatalf("coexisting evidence lost: %#v", got.EvidenceFactIDs)
	}
}

// TestSBOMPathMalformedAdvisoryRangeFailsClosed retains the matcher's
// malformed-range verdict even when a verified image attachment exists.
func TestSBOMPathMalformedAdvisoryRangeFailsClosed(t *testing.T) {
	t.Parallel()
	envelopes := sbomVersionGuardEnvelopes("4.17.12", "pkg:npm/lodash@4.17.12")
	bad := vulnerabilityAffectedPackageMalformedRangeFact(
		"affected-version", "CVE-2026-91913", crossScopeAffectedPackageID,
		"npm", "lodash", "bogus", "4.17.21",
	)
	bad.Payload["purl"] = crossScopeAffectedPURL
	envelopes[1] = bad
	got := supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(envelopes))["CVE-2026-91913"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.MatchReason != supplyChainVersionReasonMalformedRange || got.RuntimeReachability != "image_sbom" {
		t.Fatalf("malformed advisory truth lost: reason=%q reachability=%q", got.MatchReason, got.RuntimeReachability)
	}
	if !slices.Contains(got.MissingEvidence, supplyChainMissingMalformedRange) {
		t.Fatalf("malformed-range evidence missing: %#v", got.MissingEvidence)
	}
}
