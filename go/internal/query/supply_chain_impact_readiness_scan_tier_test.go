// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"testing"
)

// TestBuildSupplyChainImpactReadinessScanTierProvesScannedImage is issue
// #5467's core acceptance: a scanner_worker.analysis fact for the requested
// image proves the image was actually scanned, even when the scan found no
// installed OS packages to match against advisories (a distroless image, or
// one whose package manager Eshu does not decode). Without this family the
// envelope had no way to distinguish "never scanned" from "scanned and
// clean" for a subject-digest/image-ref anchored request: both looked
// identical (zero evidence, not_configured/evidence_incomplete).
func TestBuildSupplyChainImpactReadinessScanTierProvesScannedImage(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{SubjectDigest: "sha256:scanned-distroless"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				// vulnerability.advisory evidence is a separate, general
				// precondition (the CVE database itself must be populated
				// before any zero-finding answer is trustworthy) — it is not
				// what this test is proving. Set it so the only variable
				// under test is the scan-tier family.
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 5, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyScannerWorkerAnalysis, FactCount: 1, Freshness: FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != ReadinessStateReadyZeroFindings {
		t.Fatalf("state = %q, want %q (a scanner_worker.analysis fact alone proves the image was scanned with zero findings)", envelope.State, ReadinessStateReadyZeroFindings)
	}
	if len(envelope.MissingEvidence) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty for a scanned image with zero findings", envelope.MissingEvidence)
	}
}

// TestBuildSupplyChainImpactReadinessScanTierEvidenceAvoidsNotConfigured
// proves the scan-tier families count as real coverage for the
// not_configured gate: an image scanned by the OS-package tier (os_package +
// its sibling scanner_worker.analysis), with no other evidence at all, must
// not report not_configured — that would incorrectly claim Eshu has no
// ingestion for the scope. It still legitimately reports
// evidence_incomplete here because no vulnerability.advisory (CVE database)
// evidence is present, a general precondition unrelated to the scan tier;
// the point of this test is narrowly that not_configured must not fire.
func TestBuildSupplyChainImpactReadinessScanTierEvidenceAvoidsNotConfigured(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{SubjectDigest: "sha256:scanned-with-packages"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyScannerWorkerAnalysis, FactCount: 1, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyVulnerabilityOSPackage, FactCount: 42, Freshness: FreshnessLabelFresh},
			},
		},
	)
	if envelope.State == ReadinessStateNotConfigured {
		t.Fatalf("state = %q, want anything but %q: OS-package scan-tier evidence is real observed coverage", envelope.State, ReadinessStateNotConfigured)
	}
}

// TestBuildSupplyChainImpactReadinessNeverScannedStaysEvidenceIncomplete is
// the negative companion: an image-anchored request with real advisory
// evidence elsewhere but NO scanner_worker.analysis (and no SBOM/image
// identity) fact for the requested digest must still report
// evidence_incomplete with sbom_or_image_evidence — "never scanned" must not
// look like a clean answer just because the scan-tier families exist now.
func TestBuildSupplyChainImpactReadinessNeverScannedStaysEvidenceIncomplete(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{SubjectDigest: "sha256:never-scanned"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 1, Freshness: FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != ReadinessStateEvidenceIncomplete {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateEvidenceIncomplete)
	}
	if !readinessMissingContains(envelope.MissingEvidence, MissingEvidenceSBOMOrImage) {
		t.Fatalf("missing_evidence = %#v, want sbom_or_image_evidence for a never-scanned image", envelope.MissingEvidence)
	}
}

// TestBuildSupplyChainImpactReadinessNormalizesScanTierFamilies proves the
// two new families survive the closed evidence-family allowlist
// (normalizeEvidenceSources) instead of being silently dropped like an
// unrecognized family string would be.
func TestBuildSupplyChainImpactReadinessNormalizesScanTierFamilies(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{SubjectDigest: "sha256:normalize-scan-tier"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityOSPackage, FactCount: 3},
				{Family: EvidenceFamilyScannerWorkerAnalysis, FactCount: 1},
			},
		},
	)
	got := make([]string, 0, len(envelope.EvidenceSources))
	for _, source := range envelope.EvidenceSources {
		got = append(got, source.Family)
	}
	want := []string{EvidenceFamilyScannerWorkerAnalysis, EvidenceFamilyVulnerabilityOSPackage}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("evidence_sources = %#v, want %#v", got, want)
	}
}
