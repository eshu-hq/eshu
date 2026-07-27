// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// containerImageIdentityImpactFactWithBuildProvenance builds a
// reducer_container_image_identity fact carrying BOTH source_repository_ids
// (the #5464 os_package join's anchor field) and build_provenance_repository_ids
// (the #5801 fix's stronger-evidence field). It models the exact scenario the
// #5801 fix reconciles: an image whose OCI config source label names one
// repository (build evidence, the sole entry in build_provenance_repository_ids)
// while a DIFFERENT repository's Kubernetes/deploy manifest merely references
// the same digest (a weaker scope anchor that also lands in
// source_repository_ids, making that field carry two distinct repositories).
func containerImageIdentityImpactFactWithBuildProvenance(
	factID string,
	digest string,
	decoyRepositoryID string,
	buildProvenanceRepositoryID string,
	sourceRepositoryIDs ...string,
) facts.Envelope {
	envelope := containerImageIdentityImpactFact(factID, digest, decoyRepositoryID)
	envelope.Payload["source_repository_ids"] = append([]string(nil), sourceRepositoryIDs...)
	envelope.Payload["build_provenance_repository_ids"] = []string{buildProvenanceRepositoryID}
	return envelope
}

// TestSupplyChainImpactHandlerPrefersLabelDerivedRepositoryOverConflictingScopeAnchor
// is the #5801 reconciliation regression guard: deduping matchOCIConfigSourceRepository
// activates the oci_config_source_label_with_digest identity tier repo-wide, which
// makes source_repository_ids carry the label-derived repository ALONGSIDE any
// weaker scope/deploy anchor already present for the same image — a shape that
// used to make singleSupplyChainImageSourceRepositoryID see two distinct entries
// and blank out RepositoryID (and therefore the finding's whole
// workload/service/environment context) even though the label unambiguously named
// the image's build repository. build_provenance_repository_ids carries only
// genuine build evidence (an OCI config label, a CI run, or verified SLSA
// provenance), so when it names exactly one repository that repository MUST win —
// ranking evidence strength instead of treating the disagreement as ambiguity.
func TestSupplyChainImpactHandlerPrefersLabelDerivedRepositoryOverConflictingScopeAnchor(t *testing.T) {
	t.Parallel()

	const (
		intentScopeID      = "vuln-intel:debian:openssl-repo-anchor-label"
		intentGenerationID = "generation-intel-5801-label"
		scanScopeID        = "scan-target-debian-app-os-package-repo-anchor-label"
		scanGenerationID   = "generation-scan-5801-label"
		labelRepositoryID  = "repository:r_label_built"
		scopeRepositoryID  = "repository:r_scope_deploy_only"
		decoyRepositoryID  = "oci-registry://registry.example/repo-anchor-app-label"
		// supplyChainWorkloadIDsFromPayload only accepts entity_keys carrying
		// the "workload:" entity-key prefix (workload_id is the only other
		// accepted shape), so this must be prefixed to be indexed at all.
		workloadID = "workload:label-derived"
		imageRef   = "registry.example/repo-anchor-app-label@" + testScannerAnalysisImageDigest
	)

	osPackage := repositoryAnchorDebianOSPackageFact("dpkg-os-openssl-repo-anchor-label", scanScopeID, scanGenerationID)
	scannerAnalysis := scannerWorkerAnalysisFact(scanScopeID, scanGenerationID, testScannerAnalysisImageDigest, imageRef)
	identity := containerImageIdentityImpactFactWithBuildProvenance(
		"identity-repo-anchor-label", testScannerAnalysisImageDigest, decoyRepositoryID,
		labelRepositoryID, labelRepositoryID, scopeRepositoryID,
	)

	loader := &repositoryAnchorSupplyChainImpactFactLoader{
		factsByScope: map[string][]facts.Envelope{
			scanScopedFactLoaderKey(intentScopeID, intentGenerationID): {
				vulnerabilityCVEFactWithProvenance(
					"debian-cve-repo-anchor-label",
					"CVE-2026-5801",
					"debian",
					"DSA-2026-5801",
					7.5,
					"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
					"HIGH",
					"2026-06-05T12:00:00Z",
				),
				vulnerabilityAffectedPackageFactWithSource(
					"debian-affected-repo-anchor-label",
					"CVE-2026-5801",
					"debian",
					"DSA-2026-5801",
					"pkg:deb/debian/openssl",
					"deb",
					"openssl",
					"3.0.11-1~deb12u2",
					"3.0.11-1~deb12u3",
				),
				// Tied to the label-derived repository, proving RepositoryID
				// resolved to the stronger build-evidence repository, not the
				// scope-anchor repository (which has no matching workload here).
				workloadIdentityImpactFact("workload-repo-anchor-label-1", labelRepositoryID, workloadID),
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
		for _, digest := range filter.SubjectDigests {
			if digest == testScannerAnalysisImageDigest {
				return []facts.Envelope{identity}
			}
		}
		return nil
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-repo-anchor-label",
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
		t.Fatalf("len(Findings) = %d, want 1: %#v", got, writer.write.Findings)
	}
	got := writer.write.Findings[0]
	if got.RepositoryID != labelRepositoryID {
		t.Fatalf(
			"RepositoryID = %q, want the label-derived %q to win over the weaker scope-anchor %q: a source label is stronger evidence than a deploy/scope reference",
			got.RepositoryID, labelRepositoryID, scopeRepositoryID,
		)
	}
	assertContainsString(t, got.WorkloadIDs, workloadID)
}

// TestSupplyChainImpactHandlerSourceConsensusBeatsSingleBuildProvenanceRow is
// the #5813 handler-level golden-corpus regression guard. Unlike
// TestSupplyChainImpactHandlerPrefersLabelDerivedRepositoryOverConflictingScopeAnchor
// above (a single reducer_container_image_identity row, so
// preferSupplyChainImageIdentity's cross-row selection never runs), this test
// returns THREE identity rows for the SAME digest from the loader's
// digest-seeded call -- reproducing the live 20-repo corpus shape for digest
// sha256:abcdef...ab: fifteen rows agree on the deploying repository via
// source_repository_ids alone, and one row names both the deploying and its
// own building repository (ambiguous by source_repository_ids) but resolves
// unambiguously via build_provenance_repository_ids. The B-12 golden snapshot
// pins the deploying repository as the winner and calls the two-repository
// row "the ambiguous row this tie-break must reject" -- so this test fails
// exactly the way the live golden-corpus gate did if
// preferSupplyChainImageIdentity regresses to treating that row as
// equal-tier with the genuinely-agreeing rows.
func TestSupplyChainImpactHandlerSourceConsensusBeatsSingleBuildProvenanceRow(t *testing.T) {
	t.Parallel()

	const (
		intentScopeID      = "vuln-intel:debian:openssl-repo-anchor-consensus"
		intentGenerationID = "generation-intel-5813-consensus"
		scanScopeID        = "scan-target-debian-app-os-package-repo-anchor-consensus"
		scanGenerationID   = "generation-scan-5813-consensus"
		deployRepositoryID = "repository:r_217415d9"
		buildRepositoryID  = "repository:r_69256c06"
		decoyRepositoryID  = "oci-registry://registry.example/repo-anchor-app-consensus"
		workloadID         = "workload:consensus-deploy"
		imageRef           = "registry.example/repo-anchor-app-consensus@" + testScannerAnalysisImageDigest
	)

	osPackage := repositoryAnchorDebianOSPackageFact("dpkg-os-openssl-repo-anchor-consensus", scanScopeID, scanGenerationID)
	scannerAnalysis := scannerWorkerAnalysisFact(scanScopeID, scanGenerationID, testScannerAnalysisImageDigest, imageRef)

	// Two rows genuinely agree on the deploying repository via
	// source_repository_ids alone (mirroring the corpus's fifteen agreeing rows).
	agreeingRowOne := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		"identity-5813-consensus-agree-1", testScannerAnalysisImageDigest, decoyRepositoryID, deployRepositoryID,
	)
	agreeingRowTwo := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		"identity-5813-consensus-agree-2", testScannerAnalysisImageDigest, decoyRepositoryID, deployRepositoryID,
	)
	// The one row ambiguous by source_repository_ids (names BOTH repositories)
	// but resolvable via build_provenance_repository_ids alone. Its factID is
	// deliberately the lexicographically smallest of the three, so a passing
	// test proves the tier rule -- not the plain tie-break -- decided the
	// winner.
	ambiguousSourceButBuildResolvable := containerImageIdentityImpactFactWithBuildProvenance(
		"identity-5813-consensus-0-build", testScannerAnalysisImageDigest, decoyRepositoryID,
		buildRepositoryID, buildRepositoryID, deployRepositoryID,
	)

	loader := &repositoryAnchorSupplyChainImpactFactLoader{
		factsByScope: map[string][]facts.Envelope{
			scanScopedFactLoaderKey(intentScopeID, intentGenerationID): {
				vulnerabilityCVEFactWithProvenance(
					"debian-cve-repo-anchor-consensus",
					"CVE-2026-5813",
					"debian",
					"DSA-2026-5813",
					7.5,
					"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
					"HIGH",
					"2026-06-05T12:00:00Z",
				),
				vulnerabilityAffectedPackageFactWithSource(
					"debian-affected-repo-anchor-consensus",
					"CVE-2026-5813",
					"debian",
					"DSA-2026-5813",
					"pkg:deb/debian/openssl",
					"deb",
					"openssl",
					"3.0.11-1~deb12u2",
					"3.0.11-1~deb12u3",
				),
				// Tied to the DEPLOYING repository, so a correct resolution (not
				// the builder) is the only way this workload is reachable.
				workloadIdentityImpactFact("workload-repo-anchor-consensus-1", deployRepositoryID, workloadID),
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
		for _, digest := range filter.SubjectDigests {
			if digest == testScannerAnalysisImageDigest {
				// All three rows for the same digest, mirroring the corpus's
				// 11-rows-per-digest shape (trimmed to 3 here).
				return []facts.Envelope{agreeingRowOne, ambiguousSourceButBuildResolvable, agreeingRowTwo}
			}
		}
		return nil
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-repo-anchor-consensus",
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
		t.Fatalf("len(Findings) = %d, want 1: %#v", got, writer.write.Findings)
	}
	got := writer.write.Findings[0]
	if got.RepositoryID != deployRepositoryID {
		t.Fatalf(
			"RepositoryID = %q, want the deploying repository %q: the one row resolvable only via its own build provenance (build repository %q, smallest factID) must not outrank the two rows that already agree by source_repository_ids -- this is the exact failure the live B-7 golden-corpus gate caught (mcp:list_supply_chain_impact_findings result item missing required field \"repository_id\")",
			got.RepositoryID, deployRepositoryID, buildRepositoryID,
		)
	}
	assertContainsString(t, got.WorkloadIDs, workloadID)
}
