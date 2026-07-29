// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

var benchmarkDetectionProfile DetectionProfile

func BenchmarkClassifySupplyChainImpactDetectionProfile(b *testing.B) {
	tests := []struct {
		name    string
		finding SupplyChainImpactFinding
	}{
		{
			name: "rpm exact affected",
			finding: SupplyChainImpactFinding{
				Status:          SupplyChainImpactAffectedExact,
				ObservedVersion: "1.2.3",
				MatchReason:     supplyChainVersionReasonRPMExactAffected,
			},
		},
		{
			name: "dpkg exact affected",
			finding: SupplyChainImpactFinding{
				Status:          SupplyChainImpactAffectedExact,
				ObservedVersion: "1.2.3",
				MatchReason:     supplyChainVersionReasonDPKGExactAffected,
			},
		},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			var got DetectionProfile
			for b.Loop() {
				got = classifySupplyChainImpactDetectionProfile(test.finding)
			}
			benchmarkDetectionProfile = got
		})
	}
}

func BenchmarkEvaluateOSPackageVersionMatchAndClassify(b *testing.B) {
	tests := []struct {
		name         string
		observed     string
		fixedVersion string
		packages     []supplyChainAffectedPackage
		compare      versionCompareFunc
	}{
		{
			name:         "dpkg exact affected",
			observed:     "3.0.11-1~deb12u2",
			fixedVersion: "3.0.11-1~deb12u3",
			packages: []supplyChainAffectedPackage{{
				affectedVersions: []string{"3.0.11-1~deb12u2"},
			}},
			compare: compareDPKGVersion,
		},
		{
			name:         "dpkg exact known fixed",
			observed:     "3.0.11-1~deb12u3",
			fixedVersion: "3.0.11-1~deb12u3",
			compare:      compareDPKGVersion,
		},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			var got DetectionProfile
			for b.Loop() {
				decision := evaluateOSPackageVersionMatch(
					test.observed,
					test.fixedVersion,
					test.packages,
					supplyChainVersionReasonDPKGExactAffected,
					supplyChainVersionReasonDPKGExactKnownFixed,
					supplyChainVersionReasonDPKGAffectedRange,
					supplyChainVersionReasonDPKGKnownFixed,
					test.compare,
				)
				got = classifySupplyChainImpactDetectionProfile(SupplyChainImpactFinding{
					Status:          decision.Status,
					ObservedVersion: test.observed,
					MatchReason:     decision.Reason,
				})
			}
			benchmarkDetectionProfile = got
		})
	}
}

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

func TestSupplyChainImpactExactOSPackageReasonsQualifyForPreciseProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		observedVersion       string
		fixedVersion          string
		affectedVersions      []string
		affectedReason        string
		knownFixedReason      string
		rangeAffectedReason   string
		rangeKnownFixedReason string
		compare               versionCompareFunc
		wantStatus            SupplyChainImpactStatus
		wantReason            string
	}{
		{
			name:                  "dpkg affected",
			observedVersion:       "3.0.11-1~deb12u2",
			fixedVersion:          "3.0.11-1~deb12u3",
			affectedVersions:      []string{"3.0.11-1~deb12u2"},
			affectedReason:        supplyChainVersionReasonDPKGExactAffected,
			knownFixedReason:      supplyChainVersionReasonDPKGExactKnownFixed,
			rangeAffectedReason:   supplyChainVersionReasonDPKGAffectedRange,
			rangeKnownFixedReason: supplyChainVersionReasonDPKGKnownFixed,
			compare:               compareDPKGVersion,
			wantStatus:            SupplyChainImpactAffectedExact,
			wantReason:            supplyChainVersionReasonDPKGExactAffected,
		},
		{
			name:                  "dpkg known fixed",
			observedVersion:       "3.0.11-1~deb12u3",
			fixedVersion:          "3.0.11-1~deb12u3",
			affectedReason:        supplyChainVersionReasonDPKGExactAffected,
			knownFixedReason:      supplyChainVersionReasonDPKGExactKnownFixed,
			rangeAffectedReason:   supplyChainVersionReasonDPKGAffectedRange,
			rangeKnownFixedReason: supplyChainVersionReasonDPKGKnownFixed,
			compare:               compareDPKGVersion,
			wantStatus:            SupplyChainImpactNotAffectedKnownFixed,
			wantReason:            supplyChainVersionReasonDPKGExactKnownFixed,
		},
		{
			name:                  "apk affected",
			observedVersion:       "3.1.4-r5",
			fixedVersion:          "3.1.4-r6",
			affectedVersions:      []string{"3.1.4-r5"},
			affectedReason:        supplyChainVersionReasonAPKExactAffected,
			knownFixedReason:      supplyChainVersionReasonAPKExactKnownFixed,
			rangeAffectedReason:   supplyChainVersionReasonAPKAffectedRange,
			rangeKnownFixedReason: supplyChainVersionReasonAPKKnownFixed,
			compare:               compareAPKVersion,
			wantStatus:            SupplyChainImpactAffectedExact,
			wantReason:            supplyChainVersionReasonAPKExactAffected,
		},
		{
			name:                  "apk known fixed",
			observedVersion:       "3.1.4-r6",
			fixedVersion:          "3.1.4-r6",
			affectedReason:        supplyChainVersionReasonAPKExactAffected,
			knownFixedReason:      supplyChainVersionReasonAPKExactKnownFixed,
			rangeAffectedReason:   supplyChainVersionReasonAPKAffectedRange,
			rangeKnownFixedReason: supplyChainVersionReasonAPKKnownFixed,
			compare:               compareAPKVersion,
			wantStatus:            SupplyChainImpactNotAffectedKnownFixed,
			wantReason:            supplyChainVersionReasonAPKExactKnownFixed,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			decision := evaluateOSPackageVersionMatch(
				test.observedVersion,
				test.fixedVersion,
				[]supplyChainAffectedPackage{{affectedVersions: test.affectedVersions}},
				test.affectedReason,
				test.knownFixedReason,
				test.rangeAffectedReason,
				test.rangeKnownFixedReason,
				test.compare,
			)
			if decision.Status != test.wantStatus {
				t.Fatalf("Status = %q, want %q", decision.Status, test.wantStatus)
			}
			if decision.Reason != test.wantReason {
				t.Fatalf("Reason = %q, want %q", decision.Reason, test.wantReason)
			}
			got := classifySupplyChainImpactDetectionProfile(SupplyChainImpactFinding{
				Status:          decision.Status,
				ObservedVersion: test.observedVersion,
				MatchReason:     decision.Reason,
			})
			if got != DetectionProfilePrecise {
				t.Fatalf(
					"classifySupplyChainImpactDetectionProfile() = %q, want %q for %q",
					got,
					DetectionProfilePrecise,
					decision.Reason,
				)
			}
		})
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
