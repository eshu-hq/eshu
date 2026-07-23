// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestContainerImageIdentityHandlerAppliesSLSATierFromCrossScopeActiveFacts is
// the #5456 PR #5707 P1-b regression: attestation.statement/slsa_provenance/
// signature_verification facts live in the SBOM-attestation collector's OWN
// scope, a DIFFERENT scope than the OCI registry manifest they must override
// a weaker tier for. Handle() is exercised here for an OCI-scope-triggered
// intent whose OWN scope-local facts (loader.scopeFacts) carry no attestation
// evidence at all — only the cross-scope activeContainerImageSLSAFactLoader
// (loader.slsaActive, simulating facts persisted in the separate
// SBOM-attestation scope) supplies it. Before the P1-b fix, Handle() never
// called that loader, so the anchor map built from envelopes was always
// empty on this path and the SLSA tier could never reach a PERSISTED
// decision outside a same-scope unit test.
func TestContainerImageIdentityHandlerAppliesSLSATierFromCrossScopeActiveFacts(t *testing.T) {
	t.Parallel()

	imageRef := "registry.example.com/team/api@" + testContainerDigest
	loader := &stubContainerImageIdentityFactLoader{
		// The OCI-scope refresh's OWN scope-local facts: an image reference
		// declaration and the OCI manifest observation. No attestation
		// evidence here — that lives in a different scope entirely.
		scopeFacts: []facts.Envelope{
			gitImageRefFact("content-declares", imageRef),
			ociManifestFact("oci-manifest", testContainerDigest),
		},
		// The attestation facts, simulating persistence in the separate
		// SBOM-attestation scope, reachable ONLY via the new cross-scope
		// SLSA loader.
		slsaActive: []facts.Envelope{
			slsaImageStatementFact("statement-slsa-refresh", "stmt-slsa-refresh", testContainerDigest),
			slsaConfigSourceProvenanceFact("provenance-slsa-refresh", "stmt-slsa-refresh", slsaProofRepoURL, slsaProofCommit),
			slsaPassedVerificationFact("verification-slsa-refresh", "stmt-slsa-refresh"),
		},
	}
	writer := &recordingContainerImageIdentityWriter{}
	handler := ContainerImageIdentityHandler{
		FactLoader: loader,
		Writer:     writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-image-identity-oci-refresh",
		ScopeID:      "oci-registry://registry.example.com/team/api",
		GenerationID: "generation-oci",
		SourceSystem: "oci_registry",
		Domain:       DomainContainerImageIdentity,
		Cause:        "oci manifest observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteContainerImageIdentityDecisions() calls = %d, want 1", writer.calls)
	}
	if loader.slsaActiveCall != 1 {
		t.Fatalf("ListActiveContainerImageSLSAFacts() calls = %d, want 1", loader.slsaActiveCall)
	}
	if result.CanonicalWrites < 1 {
		t.Fatalf("CanonicalWrites = %d, want at least 1", result.CanonicalWrites)
	}

	var decision ContainerImageIdentityDecision
	var found bool
	for _, candidate := range writer.write.Decisions {
		if candidate.ImageRef == imageRef {
			decision = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no persisted decision for %q: %#v", imageRef, writer.write.Decisions)
	}
	if decision.SourceRevisionProvenance != containerImageSourceRevisionSLSAProvenanceCommit {
		t.Fatalf(
			"persisted SourceRevisionProvenance = %q, want %q (cross-scope SLSA evidence must reach the durable decision)",
			decision.SourceRevisionProvenance, containerImageSourceRevisionSLSAProvenanceCommit,
		)
	}
	if decision.SourceRevision != slsaProofCommit {
		t.Fatalf("persisted SourceRevision = %q, want %q", decision.SourceRevision, slsaProofCommit)
	}
}
