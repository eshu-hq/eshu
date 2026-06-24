// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSupplyChainImpactExactLockfileQualifiesForPreciseProfile(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-9001", 9.8),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-9001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-9001"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("DetectionProfile = %q, want %q for exact installed-version anchor", got.DetectionProfile, DetectionProfilePrecise)
	}
	if got.MatchReason == "" {
		t.Fatal("MatchReason = blank, want a documented precise match reason")
	}
}

func TestSupplyChainImpactRangeOnlyManifestIsComprehensiveOnly(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-9002", 5.3),
		vulnerabilityAffectedPackageRangeFact(
			"affected-1",
			"CVE-2026-9002",
			"pkg:npm/vite",
			"npm",
			"vite",
			"6.4.2",
		),
		packageConsumptionFactWithRange("consume-1", "pkg:npm/vite", testImpactRepositoryID, "^5.4.11"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-9002"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.DetectionProfile != DetectionProfileComprehensive {
		t.Fatalf("DetectionProfile = %q, want %q for range-only manifest evidence", got.DetectionProfile, DetectionProfileComprehensive)
	}
	if got.MatchReason != supplyChainVersionReasonRangeOnlyManifest {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonRangeOnlyManifest)
	}
	if len(got.MissingEvidence) == 0 {
		t.Fatalf("MissingEvidence = empty, want explicit reason for range-only manifest")
	}
}

func TestSupplyChainImpactProviderOnlyAlertEmitsNoImpactFinding(t *testing.T) {
	t.Parallel()

	// A provider-only security alert (no owned package, SBOM, or image
	// evidence) is not promoted into a vulnerability impact finding under
	// any detection profile. Provider alert state is reconciled separately,
	// not collapsed into Eshu impact truth.
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-9003", 7.5),
		vulnerabilityAffectedPackageFact(
			"affected-1",
			"CVE-2026-9003",
			"pkg:npm/provider-only",
			"npm",
			"provider-only",
			"1.0.0",
			"1.0.1",
		),
	})

	if len(findings) != 0 {
		t.Fatalf("len(findings) = %d, want 0 when only provider/advisory evidence exists: %#v", len(findings), findings)
	}
}

func TestSupplyChainImpactSBOMComponentDerivedIsComprehensiveOnly(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-9004", 8.0),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-9004", testImpactPackageID, "npm", "example", "9.9.9", "10.0.0"),
		sbomComponentImpactFact("component-1", "doc-1", testImpactPURL),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-9004"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedDerived)
	if got.DetectionProfile != DetectionProfileComprehensive {
		t.Fatalf("DetectionProfile = %q, want %q for SBOM-derived path without exact-version proof", got.DetectionProfile, DetectionProfileComprehensive)
	}
	if got.SubjectDigest != testImpactSubjectDigest {
		t.Fatalf("SubjectDigest = %q, want %q", got.SubjectDigest, testImpactSubjectDigest)
	}
}

func TestSupplyChainImpactMissingVersionIsComprehensiveOnly(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-9005", 7.0),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-9005", testImpactPackageID, "npm", "example", "", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-9005"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.DetectionProfile != DetectionProfileComprehensive {
		t.Fatalf("DetectionProfile = %q, want %q when advisory has no affected-version proof", got.DetectionProfile, DetectionProfileComprehensive)
	}
	if !containsMissingReason(got.MissingEvidence, "package version evidence missing") &&
		!containsMissingReason(got.MissingEvidence, "deployment exposure evidence missing") {
		t.Fatalf("MissingEvidence = %#v, want explicit missing-version reason", got.MissingEvidence)
	}
}

func TestSupplyChainImpactKnownFixedQualifiesForPreciseProfile(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-9006", 9.8),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-9006", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.3.0"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-9006"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactNotAffectedKnownFixed)
	if got.DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("DetectionProfile = %q, want %q for known-fixed exact version anchor", got.DetectionProfile, DetectionProfilePrecise)
	}
}

func TestSupplyChainImpactProductDerivedIsComprehensiveOnly(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-product", "CVE-2026-9007", 8.6),
		vulnerabilityAffectedProductFact(
			"product-1",
			"CVE-2026-9007",
			testImpactProductCriteria,
			testImpactMatchCriteriaID,
			true,
		),
		sbomComponentCPEImpactFact("component-1", "doc-1", testImpactProductCriteria),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	})

	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	got := findings[0]
	if got.DetectionProfile != DetectionProfileComprehensive {
		t.Fatalf("DetectionProfile = %q, want %q for CPE/product derived evidence", got.DetectionProfile, DetectionProfileComprehensive)
	}
}

func TestSupplyChainImpactProfileSerializedInPayload(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:            "CVE-2026-9100",
		PackageID:        testImpactPackageID,
		Status:           SupplyChainImpactAffectedExact,
		Confidence:       "exact",
		ObservedVersion:  "1.2.3",
		MatchReason:      supplyChainVersionReasonNPMSemverAffectedRange,
		DetectionProfile: DetectionProfilePrecise,
		RepositoryID:     testImpactRepositoryID,
	}
	write := SupplyChainImpactWrite{ScopeID: "scope-1", GenerationID: "generation-1"}
	payload := supplyChainImpactPayload(write, finding)
	if got, want := payload["detection_profile"], string(DetectionProfilePrecise); got != want {
		t.Fatalf("detection_profile = %#v, want %#v", got, want)
	}
}

func containsMissingReason(missing []string, want string) bool {
	for _, reason := range missing {
		if strings.Contains(reason, want) {
			return true
		}
	}
	return false
}
