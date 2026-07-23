// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSupplyChainImpactHandlerLeavesRepositoryBlankWhenImageIdentitySourceRepositoriesAmbiguous
// is the ambiguity regression guard for #5464: when the scanned image's
// identity fact names MORE THAN ONE git source_repository_id, RepositoryID
// MUST stay blank rather than guess one of them — matching the #5463 "never
// invent an anchor" discipline singleSupplyChainImageSourceRepositoryID
// implements. An unrelated workload tied to one of the two candidate
// repositories must not accidentally match either.
//
// Split into its own file (rather than
// supply_chain_impact_repository_anchor_test.go, where the other #5464
// repository-anchor tests and the shared
// containerImageIdentityImpactFactWithSourceRepositoryIDs helper live) to
// keep both files under the repo's 500-line cap.
func TestSupplyChainImpactHandlerLeavesRepositoryBlankWhenImageIdentitySourceRepositoriesAmbiguous(t *testing.T) {
	t.Parallel()

	const (
		intentScopeID      = "vuln-intel:debian:openssl-repo-anchor-ambiguous"
		intentGenerationID = "generation-intel-5464-ambiguous"
		scanScopeID        = "scan-target-debian-app-os-package-repo-anchor-ambiguous"
		scanGenerationID   = "generation-scan-5464-ambiguous"
		repositoryOne      = "repository:r_ambiguous_one"
		repositoryTwo      = "repository:r_ambiguous_two"
		decoyRepositoryID  = "oci-registry://registry.example/repo-anchor-app-ambiguous"
		workloadID         = "workload-ambiguous"
		imageRef           = "registry.example/repo-anchor-app-ambiguous@" + testScannerAnalysisImageDigest
	)

	osPackage := repositoryAnchorDebianOSPackageFact("dpkg-os-openssl-repo-anchor-ambiguous", scanScopeID, scanGenerationID)
	scannerAnalysis := scannerWorkerAnalysisFact(scanScopeID, scanGenerationID, testScannerAnalysisImageDigest, imageRef)
	identity := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		"identity-repo-anchor-ambiguous", testScannerAnalysisImageDigest, decoyRepositoryID,
		repositoryOne, repositoryTwo,
	)

	loader := &repositoryAnchorSupplyChainImpactFactLoader{
		factsByScope: map[string][]facts.Envelope{
			scanScopedFactLoaderKey(intentScopeID, intentGenerationID): {
				vulnerabilityCVEFactWithProvenance(
					"debian-cve-repo-anchor-ambiguous",
					"CVE-2026-5464",
					"debian",
					"DSA-2026-5464",
					7.5,
					"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
					"HIGH",
					"2026-06-05T12:00:00Z",
				),
				vulnerabilityAffectedPackageFactWithSource(
					"debian-affected-repo-anchor-ambiguous",
					"CVE-2026-5464",
					"debian",
					"DSA-2026-5464",
					"pkg:deb/debian/openssl",
					"deb",
					"openssl",
					"3.0.11-1~deb12u2",
					"3.0.11-1~deb12u3",
				),
				// Tied to one of the two candidate repositories, proving a blank
				// finding.RepositoryID never accidentally matches a workload just
				// because it happens to be one of the ambiguous candidates.
				workloadIdentityImpactFact("workload-ambiguous-1", repositoryOne, workloadID),
			},
			scanScopedFactLoaderKey(scanScopeID, scanGenerationID): {scannerAnalysis},
		},
	}
	osPackageServed := false
	loader.activeForFilter = func(filter SupplyChainImpactFactFilter) []facts.Envelope {
		if !osPackageServed {
			osPackageServed = true
			return []facts.Envelope{osPackage}
		}
		if slices.Contains(filter.SubjectDigests, testScannerAnalysisImageDigest) {
			return []facts.Envelope{identity}
		}
		return nil
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-repo-anchor-ambiguous",
		ScopeID:      intentScopeID,
		GenerationID: intentGenerationID,
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "debian advisory observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if got := len(writer.write.Findings); got != 1 {
		t.Fatalf("len(Findings) = %d, want 1 (retained via the owned-anchor rule): %#v", got, writer.write.Findings)
	}
	got := writer.write.Findings[0]
	if got.RepositoryID != "" {
		t.Fatalf("RepositoryID = %q, want empty: two distinct source repositories must never be guessed down to one", got.RepositoryID)
	}
	if len(got.WorkloadIDs) != 0 {
		t.Fatalf("WorkloadIDs = %#v, want none: an ambiguous repository must never resolve workload context", got.WorkloadIDs)
	}
	assertNotContainsString(t, got.EvidenceFactIDs, "identity-repo-anchor-ambiguous")
	assertNotContainsString(t, got.EvidencePath, containerImageIdentityFactKind)
	assertContainsString(t, got.EvidencePath, facts.VulnerabilityOSPackageFactKind)
}
