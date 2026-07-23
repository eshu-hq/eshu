// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// testScannerAnalysisImageDigest is a syntactically valid sha256 digest (64
// lowercase hex characters) standing in for a real scanner_worker.analysis
// ImageDigest. It is deliberately nothing like any os_package ScopeID used in
// this file, so a test asserting SubjectDigest equals this value (and never
// equals a ScopeID) cannot pass by accident.
const testScannerAnalysisImageDigest = "sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

// TestBuildSupplyChainImpactFindingsAnchorsOSPackageSubjectDigestOnScannerAnalysis
// is the issue #5463 acceptance fixture: an os_package finding whose sibling
// scanner_worker.analysis fact (joined by ScopeID+GenerationID) carries the
// real image digest/reference must anchor SubjectDigest/ImageRef on THAT
// evidence, never on the os_package's own opaque ScopeID. It also proves the
// resulting real digest lets a CI-declared deployment resolve
// RuntimeReachability to "deployed_image", which the old scope_id-as-digest
// value could only do by accidental string collision.
func TestBuildSupplyChainImpactFindingsAnchorsOSPackageSubjectDigestOnScannerAnalysis(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "scan-target-debian-app-os-package"
		generationID = "generation-5463"
		imageRef     = "registry.example/debian-app@" + testScannerAnalysisImageDigest
		repositoryID = "repo:debian-app"
	)

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFactWithProvenance(
			"debian-cve-digest",
			"CVE-2026-5463",
			"debian",
			"DSA-2026-5463",
			7.5,
			"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
			"HIGH",
			"2026-06-05T12:00:00Z",
		),
		vulnerabilityAffectedPackageFactWithSource(
			"debian-affected-digest",
			"CVE-2026-5463",
			"debian",
			"DSA-2026-5463",
			"pkg:deb/debian/openssl",
			"deb",
			"openssl",
			"3.0.11-1~deb12u2",
			"3.0.11-1~deb12u3",
		),
		{
			FactID:       "dpkg-os-openssl-digest",
			FactKind:     facts.VulnerabilityOSPackageFactKind,
			ScopeID:      scopeID,
			GenerationID: generationID,
			Payload: map[string]any{
				"distro":                 "debian",
				"distro_version":         "12",
				"package_manager":        "dpkg",
				"name":                   "openssl",
				"arch":                   "amd64",
				"repository_class":       "vendor",
				"vendor_advisory_source": "debian",
				"installed_version_raw":  "3.0.11-1~deb12u2",
				"purl":                   "pkg:deb/debian/openssl@3.0.11-1~deb12u2?arch=amd64&distro=debian-12",
			},
		},
		scannerWorkerAnalysisFact(scopeID, generationID, testScannerAnalysisImageDigest, imageRef),
		cicdRunCorrelationImpactFact(
			"deploy-digest",
			testScannerAnalysisImageDigest,
			imageRef,
			repositoryID,
			"prod",
			string(CICDRunCorrelationExact),
		),
	})

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
	}
	got := findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.SubjectDigest != testScannerAnalysisImageDigest {
		t.Fatalf("SubjectDigest = %q, want real scanner_worker.analysis digest %q (not the os_package scope_id %q)",
			got.SubjectDigest, testScannerAnalysisImageDigest, scopeID)
	}
	if got.SubjectDigest == scopeID {
		t.Fatalf("SubjectDigest = %q must never equal the os_package scope_id", got.SubjectDigest)
	}
	if got.ImageRef != imageRef {
		t.Fatalf("ImageRef = %q, want scanner_worker.analysis image reference %q", got.ImageRef, imageRef)
	}
	path := strings.Join(got.EvidencePath, " -> ")
	if !strings.Contains(path, facts.ScannerWorkerAnalysisFactKind) {
		t.Fatalf("EvidencePath = %#v, want scanner_worker.analysis evidence", got.EvidencePath)
	}
	if got.RuntimeReachability != "deployed_image" {
		t.Fatalf("RuntimeReachability = %q, want deployed_image now that the scanned digest %q matches the CI-declared deployment",
			got.RuntimeReachability, testScannerAnalysisImageDigest)
	}
}

// TestBuildSupplyChainImpactFindingsLeavesOSPackageSubjectDigestBlankWithoutScannerAnalysis
// is the regression proof that the fake-digest scope_id fallback is gone: an
// os_package finding with no sibling scanner_worker.analysis fact for its
// ScopeID must leave SubjectDigest empty (there is no SBOM component path
// here either), never fall back to the ScopeID string.
func TestBuildSupplyChainImpactFindingsLeavesOSPackageSubjectDigestBlankWithoutScannerAnalysis(t *testing.T) {
	t.Parallel()

	const scopeID = "scan-target-debian-app-no-analysis"

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFactWithProvenance(
			"debian-cve-no-analysis",
			"CVE-2026-5464",
			"debian",
			"DSA-2026-5464",
			7.5,
			"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
			"HIGH",
			"2026-06-05T12:00:00Z",
		),
		vulnerabilityAffectedPackageFactWithSource(
			"debian-affected-no-analysis",
			"CVE-2026-5464",
			"debian",
			"DSA-2026-5464",
			"pkg:deb/debian/openssl",
			"deb",
			"openssl",
			"3.0.11-1~deb12u2",
			"3.0.11-1~deb12u3",
		),
		osPackageFact("dpkg-os-openssl-no-analysis", scopeID, map[string]any{
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
		t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
	}
	got := findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.SubjectDigest != "" {
		t.Fatalf("SubjectDigest = %q, want empty: the deleted scope_id fallback must not resurrect scope_id %q as a fake digest",
			got.SubjectDigest, scopeID)
	}
	if got.SubjectDigest == scopeID {
		t.Fatalf("SubjectDigest = %q must never equal the os_package scope_id", got.SubjectDigest)
	}
}

// scannerWorkerAnalysisFact builds a scanner_worker.analysis fixture carrying
// a ScopeID/GenerationID matching a sibling os_package fact, so
// classifySupplyChainImpactPackage's ScopeID+GenerationID join
// (supplyChainScopeGenerationKey) resolves the os_package's real image
// digest/reference from this fact instead of the os_package's own ScopeID.
func scannerWorkerAnalysisFact(scopeID, generationID, imageDigest, imageReference string) facts.Envelope {
	return facts.Envelope{
		FactID:       "scanner-analysis:" + scopeID + ":" + generationID,
		FactKind:     facts.ScannerWorkerAnalysisFactKind,
		ScopeID:      scopeID,
		GenerationID: generationID,
		Payload: map[string]any{
			"analyzer":            "os-package",
			"target_kind":         "container_image",
			"target_locator_hash": "hash-" + scopeID,
			"analysis_status":     "completed",
			"coverage_status":     "scanned",
			"result_count":        1,
			"fact_count":          1,
			"image_reference":     imageReference,
			"image_digest":        imageDigest,
			"evidence_source":     "registry",
			"extraction_reason":   "scheduled_scan",
		},
	}
}
