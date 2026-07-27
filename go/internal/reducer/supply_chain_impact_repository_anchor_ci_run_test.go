// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSupplyChainImpactHandlerEndToEndCrossScopeCIRunDoesNotBlankRepositoryID
// is the full #5810 reproduction of the live B-7 golden-corpus gate failure:
//
//	[FAIL] mcp:list_supply_chain_impact_findings: result item missing
//	required field "repository_id"
//
// Unlike every other test in this file (and #5817's own regression suite),
// this one does NOT hand-author the reducer_container_image_identity fact
// payload. It runs the REAL ContainerImageIdentityHandler first -- with the
// exact #5810 cross-scope shape (a repository's own content_entity reference
// to a third-party digest, plus a ci.run/ci.artifact pair for a DIFFERENT
// repository reachable only through the new activeContainerImageCIFactLoader
// bridge) -- captures the decision it durably writes, reshapes it through the
// production containerImageIdentityPayload function (container_image_identity_writer.go,
// the exact shape the reducer persists), and feeds THAT into the real
// SupplyChainImpactHandler. A single-call construction of either handler
// cannot exercise this: the bug is specifically that TWO decisions
// (deploy-only vs. deploy+CI-build) collapse into ONE ambiguous
// SourceRepositoryIDs field with an empty BuildProvenanceRepositoryIDs field
// only when scope separation is real and the cross-scope CI loader is the one
// bridging them -- exactly the false-green shape #5810 itself warns about.
//
// Before the applyCIRunDigestRevision fix (container_image_identity_registry.go),
// finding.RepositoryID is "" here: SourceRepositoryIDs carries both the deploy
// repository and the CI-build repository (ambiguous), and
// BuildProvenanceRepositoryIDs is empty, so singleSupplyChainImageSourceRepositoryID
// (supply_chain_impact_anchor_tier.go) resolves neither field and blanks the
// finding -- workload/service/environment context is lost with it. After the
// fix, BuildProvenanceRepositoryIDs carries the CI-confirmed builder
// unambiguously, so the finding resolves to it (the strongest available
// evidence for this digest, matching the #5801 precedent that build evidence
// outranks a mere deploy/scope reference) and its workload joins.
func TestSupplyChainImpactHandlerEndToEndCrossScopeCIRunDoesNotBlankRepositoryID(t *testing.T) {
	t.Parallel()

	const (
		digest        = "sha256:5810c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3"
		deployRepoID  = "repository:r_deploy_5810ci_e2e"
		ciBuildRepoID = "repository:r_ci_build_5810ci_e2e"
		imageRef      = "registry.example.com/team/api@" + digest
	)

	// Step 1: run the REAL container-image-identity reducer over the exact
	// #5810 cross-scope shape.
	deployRef := facts.Envelope{
		FactID:           "content-declares-5810ci-e2e",
		ScopeID:          deployRepoID,
		GenerationID:     "generation-repo",
		FactKind:         factKindContentEntity,
		SchemaVersion:    "1.0.0",
		CollectorKind:    "git",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
		SourceRef:        facts.Ref{SourceSystem: "git"},
		Payload: map[string]any{
			"uid":         "entity:deployment",
			"entity_type": "KubernetesResource",
			"metadata": map[string]any{
				"container_images": []string{imageRef},
			},
		},
	}
	identityLoader := &stubContainerImageIdentityFactLoader{
		scopeFacts: []facts.Envelope{deployRef},
		active:     []facts.Envelope{ociManifestFact("oci-manifest-5810ci-e2e", digest)},
		ciActive: []facts.Envelope{
			ciRunFact("run-5810ci-e2e", "github_actions", ciBuildRepoID, ""),
			ciArtifactFact("artifact-5810ci-e2e", "run-5810ci-e2e", digest),
		},
	}
	identityWriter := &recordingContainerImageIdentityWriter{}
	identityHandler := ContainerImageIdentityHandler{FactLoader: identityLoader, Writer: identityWriter}

	if _, err := identityHandler.Handle(context.Background(), Intent{
		IntentID:     "intent-5810ci-e2e-identity",
		ScopeID:      deployRepoID,
		GenerationID: "generation-repo",
		SourceSystem: "git",
		Domain:       DomainContainerImageIdentity,
		Cause:        "deploy manifest observed",
	}); err != nil {
		t.Fatalf("ContainerImageIdentityHandler.Handle() error = %v, want nil", err)
	}

	decisionsByImageRef := decisionsByRef(identityWriter.write.Decisions)
	decision, ok := decisionsByImageRef[imageRef]
	if !ok {
		t.Fatalf("no written identity decision for %q: %#v", imageRef, decisionsByImageRef)
	}

	// Step 2: reshape the captured decision through the EXACT production
	// payload function, then feed it into the supply-chain-impact reducer as
	// its digest-seeded cross-scope evidence, exactly as the durable fact
	// would be loaded in production.
	identityFact := facts.Envelope{
		FactID:   "identity-5810ci-e2e",
		FactKind: containerImageIdentityFactKind,
		Payload:  containerImageIdentityPayload(identityWriter.write, decision, "canonical-5810ci-e2e"),
	}

	const (
		intentScopeID      = "vuln-intel:debian:openssl-5810ci-e2e"
		intentGenerationID = "generation-intel-5810ci-e2e"
		scanScopeID        = "scan-target-debian-app-os-package-5810ci-e2e"
		scanGenerationID   = "generation-scan-5810ci-e2e"
		workloadID         = "workload:5810ci-e2e-build"
	)

	osPackage := repositoryAnchorDebianOSPackageFact("dpkg-os-openssl-5810ci-e2e", scanScopeID, scanGenerationID)
	scannerAnalysis := scannerWorkerAnalysisFact(scanScopeID, scanGenerationID, digest, imageRef)

	impactLoader := &repositoryAnchorSupplyChainImpactFactLoader{
		factsByScope: map[string][]facts.Envelope{
			scanScopedFactLoaderKey(intentScopeID, intentGenerationID): {
				vulnerabilityCVEFactWithProvenance(
					"debian-cve-5810ci-e2e",
					"CVE-2026-5810",
					"debian",
					"DSA-2026-5810",
					7.5,
					"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
					"HIGH",
					"2026-06-05T12:00:00Z",
				),
				vulnerabilityAffectedPackageFactWithSource(
					"debian-affected-5810ci-e2e",
					"CVE-2026-5810",
					"debian",
					"DSA-2026-5810",
					"pkg:deb/debian/openssl",
					"deb",
					"openssl",
					"3.0.11-1~deb12u2",
					"3.0.11-1~deb12u3",
				),
				// Tied to the CI-confirmed builder, so a correct resolution (not a
				// blank RepositoryID) is the only way this workload is reachable.
				workloadIdentityImpactFact("workload-5810ci-e2e-1", ciBuildRepoID, workloadID),
			},
			scanScopedFactLoaderKey(scanScopeID, scanGenerationID): {scannerAnalysis},
		},
	}
	osPackageServed := false
	impactLoader.activeForFilter = func(filter SupplyChainImpactFactFilter) []facts.Envelope {
		if !osPackageServed {
			osPackageServed = true
			return []facts.Envelope{osPackage}
		}
		for _, candidate := range filter.SubjectDigests {
			if candidate == digest {
				return []facts.Envelope{identityFact}
			}
		}
		return nil
	}
	impactWriter := &recordingSupplyChainImpactWriter{}
	impactHandler := SupplyChainImpactHandler{FactLoader: impactLoader, Writer: impactWriter}

	result, err := impactHandler.Handle(context.Background(), Intent{
		IntentID:     "intent-5810ci-e2e-impact",
		ScopeID:      intentScopeID,
		GenerationID: intentGenerationID,
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "debian advisory observed",
	})
	if err != nil {
		t.Fatalf("SupplyChainImpactHandler.Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if got := len(impactWriter.write.Findings); got != 1 {
		t.Fatalf("len(Findings) = %d, want 1: %#v", got, impactWriter.write.Findings)
	}
	got := impactWriter.write.Findings[0]
	if got.RepositoryID == "" {
		t.Fatalf(
			"RepositoryID is blank -- exactly the live B-7 golden-corpus gate failure "+
				"(mcp:list_supply_chain_impact_findings: result item missing required "+
				"field \"repository_id\"). Finding = %#v, identity fact payload = %#v",
			got, identityFact.Payload,
		)
	}
	if got.RepositoryID != ciBuildRepoID {
		t.Fatalf("RepositoryID = %q, want the CI-confirmed builder %q", got.RepositoryID, ciBuildRepoID)
	}
	assertContainsString(t, got.WorkloadIDs, workloadID)
}
