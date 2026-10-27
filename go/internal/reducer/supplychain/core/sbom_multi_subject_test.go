// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSBOMMultiSubjectClassifiesEachImageVersion proves that one affected
// package can describe different installed versions in separate images.
func TestSBOMMultiSubjectClassifiesEachImageVersion(t *testing.T) {
	t.Parallel()
	const digestAffected = "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const digestFixed = "sha256:" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	base := []facts.Envelope{
		vulnerabilityCVEFact("cve-multi", "CVE-2026-91913", 8.0),
		vulnerabilityAffectedPackageMalformedRangeFact("affected-multi", "CVE-2026-91913", crossScopeAffectedPackageID, "npm", "lodash", "4.17.11", "4.17.21"),
		sbomComponentImpactFactWithPackageIDVersion("component-affected", "doc-affected", "pkg:npm/lodash@4.17.12", crossScopeAffectedPackageID, "4.17.12"),
		sbomAttachmentImpactFact("attachment-affected", "doc-affected", digestAffected),
		containerImageIdentityImpactFact("image-affected", digestAffected, testImpactRepositoryID),
		sbomComponentImpactFactWithPackageIDVersion("component-fixed", "doc-fixed", "pkg:npm/lodash@4.17.21", crossScopeAffectedPackageID, "4.17.21"),
		sbomAttachmentImpactFact("attachment-fixed", "doc-fixed", digestFixed),
		containerImageIdentityImpactFact("image-fixed", digestFixed, testImpactRepositoryID),
	}
	for _, reverse := range []bool{false, true} {
		input := slices.Clone(base)
		if reverse {
			slices.Reverse(input)
		}
		findings := BuildSupplyChainImpactFindings(input)
		if len(findings) != 2 {
			t.Fatalf("reverse=%t findings=%d, want two image findings: %#v", reverse, len(findings), findings)
		}
		byDigest := map[string]SupplyChainImpactFinding{}
		for _, finding := range findings {
			byDigest[finding.SubjectDigest] = finding
		}
		for _, tc := range []struct {
			digest, version string
			status          SupplyChainImpactStatus
		}{
			{digestAffected, "4.17.12", SupplyChainImpactAffectedExact},
			{digestFixed, "4.17.21", SupplyChainImpactNotAffectedKnownFixed},
		} {
			got, ok := byDigest[tc.digest]
			if !ok || got.ObservedVersion != tc.version || got.Status != tc.status || !strings.Contains(strings.Join(got.EvidenceFactIDs, ","), "component-") {
				t.Fatalf("reverse=%t digest=%s finding=%#v, want version=%s status=%s", reverse, tc.digest, got, tc.version, tc.status)
			}
		}
	}
}

// TestSBOMMultiSubjectSameDigestVersionsAndDuplicates keeps distinct installed
// versions for one image while coalescing equivalent duplicate observations.
func TestSBOMMultiSubjectSameDigestVersionsAndDuplicates(t *testing.T) {
	t.Parallel()
	facts := sbomVersionGuardEnvelopes("4.17.12", "pkg:npm/lodash@4.17.12")
	fixed := sbomComponentImpactFactWithPackageIDVersion("fixed", "doc-fixed", "pkg:npm/lodash@4.17.21", crossScopeAffectedPackageID, "4.17.21")
	facts = append(facts, fixed, sbomAttachmentImpactFact("fixed-attachment", "doc-fixed", testImpactSubjectDigest))
	duplicate := sbomComponentImpactFactWithPackageIDVersion("duplicate", "doc-duplicate", "pkg:npm/lodash@4.17.12", crossScopeAffectedPackageID, "4.17.12")
	facts = append(facts, duplicate, sbomAttachmentImpactFact("duplicate-attachment", "doc-duplicate", testImpactSubjectDigest))
	got := BuildSupplyChainImpactFindings(facts)
	if len(got) != 2 {
		t.Fatalf("findings=%d, want two versions after duplicate coalescing: %#v", len(got), got)
	}
	statuses := map[string]SupplyChainImpactStatus{}
	for _, finding := range got {
		statuses[finding.ObservedVersion] = finding.Status
	}
	if statuses["4.17.12"] != SupplyChainImpactAffectedExact || statuses["4.17.21"] != SupplyChainImpactNotAffectedKnownFixed {
		t.Fatalf("versions/statuses=%v", statuses)
	}
}

// TestSBOMMultiSubjectRejectsInvalidSiblingAndConflictingAttachment checks
// invalid or contradictory document bindings cannot erase a valid sibling.
func TestSBOMMultiSubjectRejectsInvalidSiblingAndConflictingAttachment(t *testing.T) {
	t.Parallel()
	base := sbomVersionGuardEnvelopes("4.17.12", "pkg:npm/lodash@4.17.12")
	invalid := sbomComponentImpactFactWithPackageIDVersion("invalid", "doc-invalid", "pkg:npm/lodash@4.17.21", crossScopeAffectedPackageID, "4.17.21")
	attachment := sbomAttachmentImpactFact("invalid-attachment", "doc-invalid", "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	attachment.Payload["attachment_status"] = "subject_mismatch"
	base = append(base, invalid, attachment)
	conflict := sbomAttachmentImpactFact("conflict-attachment", "doc-invalid", testImpactSubjectDigest)
	base = append(base, conflict)
	for _, reverse := range []bool{false, true} {
		input := slices.Clone(base)
		if reverse {
			slices.Reverse(input)
		}
		got := BuildSupplyChainImpactFindings(input)
		if len(got) != 1 || got[0].SubjectDigest != testImpactSubjectDigest || got[0].ObservedVersion != "4.17.12" {
			t.Fatalf("reverse=%t findings=%#v", reverse, got)
		}
	}
}

// TestSBOMMultiSubjectKeepsUnassociatedConsumptionSeparate prevents a
// package-level manifest version or repository from being assigned to an image.
func TestSBOMMultiSubjectKeepsUnassociatedConsumptionSeparate(t *testing.T) {
	t.Parallel()
	facts := sbomVersionGuardEnvelopes("4.17.21", "pkg:npm/lodash@4.17.21")
	consume := packageConsumptionFactWithRange("consume-unrelated", crossScopeAffectedPackageID, "repo://unrelated", "4.17.12")
	facts = append(facts, consume)
	got := BuildSupplyChainImpactFindings(facts)
	if len(got) != 2 {
		t.Fatalf("findings=%d, want image plus manifest: %#v", len(got), got)
	}
	for _, finding := range got {
		if finding.SubjectDigest == testImpactSubjectDigest {
			if finding.RepositoryID == "repo://unrelated" || slices.Contains(finding.EvidenceFactIDs, consume.FactID) || finding.Status != SupplyChainImpactNotAffectedKnownFixed {
				t.Fatalf("unrelated consumption leaked into image: %#v", finding)
			}
		} else if finding.RepositoryID != "repo://unrelated" || finding.Status != SupplyChainImpactAffectedExact {
			t.Fatalf("separate manifest path lost: %#v", finding)
		}
	}
}

// TestSBOMSubjectOSJoinRequiresSameScanDigest checks scanner scope and
// generation bind OS evidence to the image before it may affect that image.
func TestSBOMSubjectOSJoinRequiresSameScanDigest(t *testing.T) {
	t.Parallel()
	const (
		packageID = "pkg:rpm/redhat/openssl"
		digestA   = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		digestB   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	for _, tc := range []struct {
		name, analysisDigest string
		wantJoined           bool
	}{
		{"same digest", digestA, true},
		{"other digest", digestB, false},
		{"missing digest", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope, generation := "scan-os-subject", "generation-os-subject"
			os := osPackageFact("os-subject", scope, map[string]any{
				"distro": "redhat", "distro_version": "9.2", "package_manager": "rpm",
				"name": "openssl", "epoch": "1", "upstream_version": "3.0.7",
				"distro_release": "18.el9_2", "arch": "x86_64", "source_package": "openssl",
				"repository_class": "vendor", "vendor_advisory_source": "redhat",
				"installed_version_raw": "1:3.0.7-18.el9_2",
				"purl":                  "pkg:rpm/redhat/openssl@1:3.0.7-18.el9_2?arch=x86_64&distro=redhat-9.2",
			})
			os.GenerationID = generation
			input := []facts.Envelope{
				vulnerabilityCVEFactWithProvenance("cve-os-subject", "CVE-2026-91914", "redhat", "RHSA-2026:91914", 7.8, "", "HIGH", "2026-05-31T12:00:00Z"),
				vulnerabilityAffectedPackageFactWithSource("affected-os-subject", "CVE-2026-91914", "redhat", "RHSA-2026:91914", packageID, "redhat", "openssl", "1:3.0.7-18.el9_2", "1:3.0.7-20.el9_2"),
				sbomComponentImpactFactWithPackageIDVersion("component-os-subject", "doc-os-subject", "pkg:rpm/redhat/openssl@1:3.0.7-18.el9_2", packageID, "1:3.0.7-18.el9_2"),
				sbomAttachmentImpactFact("attachment-os-subject", "doc-os-subject", digestA),
				containerImageIdentityImpactFact("image-os-subject", digestA, testImpactRepositoryID),
				os,
			}
			if tc.analysisDigest != "" {
				input = append(input, scannerWorkerAnalysisFact(scope, generation, tc.analysisDigest, "registry.example/os@"+tc.analysisDigest))
			}
			findings := BuildSupplyChainImpactFindings(input)
			var image *SupplyChainImpactFinding
			for i := range findings {
				if findings[i].SubjectDigest == digestA {
					image = &findings[i]
				}
			}
			if image == nil || slices.Contains(image.EvidenceFactIDs, os.FactID) != tc.wantJoined {
				t.Fatalf("analysis=%q image=%#v findings=%#v", tc.analysisDigest, image, findings)
			}
			if !tc.wantJoined && len(findings) != 2 {
				t.Fatalf("analysis=%q findings=%d, want separate OS path", tc.analysisDigest, len(findings))
			}
		})
	}
}

func supplyChainImpactFindingForDigest(t *testing.T, findings []SupplyChainImpactFinding, digest string) SupplyChainImpactFinding {
	t.Helper()
	for _, finding := range findings {
		if finding.SubjectDigest == digest {
			return finding
		}
	}
	t.Fatalf("no finding for digest %q in %#v", digest, findings)
	return SupplyChainImpactFinding{}
}

// TestSBOMSubjectConflictingOSVersionKeepsSBOMIdentity prevents a matching
// scan digest from silently replacing the SBOM's versioned package identity.
func TestSBOMSubjectConflictingOSVersionKeepsSBOMIdentity(t *testing.T) {
	t.Parallel()
	const (
		packageID = "pkg:rpm/redhat/openssl"
		digest    = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	os := osPackageFact("os-conflict", "scan-os-conflict", map[string]any{
		"distro": "redhat", "distro_version": "9.2", "package_manager": "rpm",
		"name": "openssl", "epoch": "1", "upstream_version": "3.0.7",
		"distro_release": "19.el9_2", "arch": "x86_64", "source_package": "openssl",
		"repository_class": "vendor", "vendor_advisory_source": "redhat",
		"installed_version_raw": "1:3.0.7-19.el9_2",
		"purl":                  "pkg:rpm/redhat/openssl@1:3.0.7-19.el9_2?arch=x86_64&distro=redhat-9.2",
	})
	os.GenerationID = "generation-conflict"
	const sbomPURL = "pkg:rpm/redhat/openssl@1:3.0.7-18.el9_2"
	input := []facts.Envelope{
		vulnerabilityCVEFactWithProvenance("cve-conflict", "CVE-2026-91914", "redhat", "RHSA-2026:91914", 7.8, "", "HIGH", "2026-05-31T12:00:00Z"),
		vulnerabilityAffectedPackageFactWithSource("affected-conflict", "CVE-2026-91914", "redhat", "RHSA-2026:91914", packageID, "redhat", "openssl", "1:3.0.7-18.el9_2", "1:3.0.7-20.el9_2"),
		sbomComponentImpactFactWithPackageIDVersion("component-conflict", "doc-conflict", sbomPURL, packageID, "1:3.0.7-18.el9_2"),
		sbomAttachmentImpactFact("attachment-conflict", "doc-conflict", digest),
		containerImageIdentityImpactFact("image-conflict", digest, testImpactRepositoryID),
		os,
		scannerWorkerAnalysisFact(os.ScopeID, os.GenerationID, digest, "registry.example/os@"+digest),
	}
	got := supplyChainImpactFindingForDigest(t, BuildSupplyChainImpactFindings(input), digest)
	if got.Status != SupplyChainImpactPossiblyAffected || got.MatchReason != supplyChainVersionReasonSBOMVersionConflict || got.ObservedVersion != "1:3.0.7-18.el9_2" || got.PURL != sbomPURL {
		t.Fatalf("conflicting OS version overwrote SBOM identity: status=%q reason=%q version=%q purl=%q", got.Status, got.MatchReason, got.ObservedVersion, got.PURL)
	}
}

// TestSBOMSubjectRejectsConflictingScannerDigests requires one scan scope and
// generation to resolve one digest before its OS fact may join an SBOM image.
func TestSBOMSubjectRejectsConflictingScannerDigests(t *testing.T) {
	t.Parallel()
	const (
		packageID = "pkg:rpm/redhat/openssl"
		digestA   = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		digestB   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	os := osPackageFact("os-two-digests", "scan-two-digests", map[string]any{
		"distro": "redhat", "distro_version": "9.2", "package_manager": "rpm",
		"name": "openssl", "epoch": "1", "upstream_version": "3.0.7",
		"distro_release": "18.el9_2", "arch": "x86_64", "source_package": "openssl",
		"repository_class": "vendor", "vendor_advisory_source": "redhat",
		"installed_version_raw": "1:3.0.7-18.el9_2",
		"purl":                  "pkg:rpm/redhat/openssl@1:3.0.7-18.el9_2?arch=x86_64&distro=redhat-9.2",
	})
	os.GenerationID = "generation-two-digests"
	analysisA := scannerWorkerAnalysisFact(os.ScopeID, os.GenerationID, digestA, "registry.example/os@"+digestA)
	analysisB := scannerWorkerAnalysisFact(os.ScopeID, os.GenerationID, digestB, "registry.example/os@"+digestB)
	analysisB.FactID += ":other-analyzer"
	base := []facts.Envelope{
		vulnerabilityCVEFactWithProvenance("cve-two-digests", "CVE-2026-91914", "redhat", "RHSA-2026:91914", 7.8, "", "HIGH", "2026-05-31T12:00:00Z"),
		vulnerabilityAffectedPackageFactWithSource("affected-two-digests", "CVE-2026-91914", "redhat", "RHSA-2026:91914", packageID, "redhat", "openssl", "1:3.0.7-18.el9_2", "1:3.0.7-20.el9_2"),
		sbomComponentImpactFactWithPackageIDVersion("component-two-digests", "doc-two-digests", "pkg:rpm/redhat/openssl@1:3.0.7-18.el9_2", packageID, "1:3.0.7-18.el9_2"),
		sbomAttachmentImpactFact("attachment-two-digests", "doc-two-digests", digestA),
		containerImageIdentityImpactFact("image-two-digests", digestA, testImpactRepositoryID),
		os,
	}
	for _, analyses := range [][]facts.Envelope{{analysisA, analysisB}, {analysisB, analysisA}} {
		input := append(slices.Clone(base), analyses...)
		findings := BuildSupplyChainImpactFindings(input)
		image := supplyChainImpactFindingForDigest(t, findings, digestA)
		if slices.Contains(image.EvidenceFactIDs, os.FactID) {
			t.Fatalf("conflicting scan digest joined OS evidence: %#v", image)
		}
	}
}
