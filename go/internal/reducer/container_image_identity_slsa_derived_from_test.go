// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestContainerImageIdentityHandlerProjectsDerivedFromEdgeFromSLSAOnlyEvidenceNoRefSmuggling
// is the #5810 folded-in Part B regression: a verified SLSA attestation alone
// must produce a lineage edge. Before this fix,
// extractSLSADigestAnchorsWithQuarantine only ever built a digest->anchor MAP
// (container_image_identity_slsa.go); the complete set of ref creators in
// extractContainerImageRefsWithQuarantine (container_image_identity_evidence.go)
// never treated the three attestation kinds as a ref source, so a digest
// evidenced ONLY by a verified SLSA attestation (no content_entity, no
// ci.artifact, no OCI config source label) never became a
// containerImageRefEvidence entry -- BuildContainerImageIdentityDecisionsWithQuarantine
// only classifies existing refs, so it produced ZERO decisions for that
// digest, hence zero DERIVED_FROM/BUILT_FROM edges, no matter how strong the
// attestation was.
//
// Every existing SLSA test smuggles in a ref via gitImageRefFact (a
// content_entity) or ciArtifactFact, so they only ever proved attribution ON
// AN EXISTING decision, never ref existence. This fixture deliberately
// carries NEITHER: the child digest's only evidence anywhere in these
// envelopes is the verified SLSA attestation naming ownerRepoID as the
// build's config-source repository. It must FAIL before the fix (zero
// DERIVED_FROM rows) and pass after (exactly one row pairing the Dockerfile
// base to the SLSA-attested child).
func TestContainerImageIdentityHandlerProjectsDerivedFromEdgeFromSLSAOnlyEvidenceNoRefSmuggling(t *testing.T) {
	t.Parallel()

	const (
		baseDigest  = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		childDigest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
		ownerRepoID = "repository:team-slsa-api"
	)

	loader := &stubContainerImageIdentityFactLoader{
		// The repository-owned intent's OWN scope-local facts: the Dockerfile
		// alone, declaring its runtime base by digest -- exactly like the
		// #5810 CI regression fixture, so the ONLY difference under test is
		// the child's evidence source.
		scopeFacts: []facts.Envelope{
			dockerfileStagesFact(
				"fact-dockerfile-slsa", ownerRepoID, "Dockerfile",
				map[string]any{
					"stage_index": 0,
					"base_image":  "registry.example.com/team/api@" + baseDigest,
					"base_tag":    "",
				},
			),
		},
		// The existing cross-scope OCI/identity bridge: registry observations
		// for both digests, so both can resolve to exact_digest once a ref
		// exists for each.
		active: []facts.Envelope{
			ociManifestFact("oci-manifest-base-slsa", baseDigest),
			ociManifestFact("oci-manifest-child-slsa", childDigest),
			repositoryRemoteFact(ownerRepoID, slsaProofRepoURL+".git"),
		},
		// The SLSA cross-scope bridge: a verified attestation naming
		// ownerRepoID as the child digest's build-provenance repository.
		// NOTHING else references childDigest -- no content_entity, no
		// ci.artifact, no OCI config source label. This is the whole point:
		// before the fix, this evidence alone can never become a ref.
		slsaActive: []facts.Envelope{
			slsaImageStatementFact("statement-slsa-derived", "stmt-slsa-derived", childDigest),
			slsaConfigSourceProvenanceFact("provenance-slsa-derived", "stmt-slsa-derived", slsaProofRepoURL, slsaProofCommit),
			slsaPassedVerificationFact("verification-slsa-derived", "stmt-slsa-derived"),
		},
	}
	writer := &recordingContainerImageIdentityWriter{}
	derivedFromWriter := &recordingContainerImageDerivedFromEdgeWriter{}
	handler := ContainerImageIdentityHandler{
		FactLoader:            loader,
		Writer:                writer,
		DerivedFromEdgeWriter: derivedFromWriter,
		FencingTokenIssuer:    &stubContainerImageIdentityFencingTokenIssuer{tokens: []int64{1}},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-derived-from-slsa-only",
		ScopeID:      ownerRepoID,
		GenerationID: "generation-repo-slsa",
		SourceSystem: "git",
		Domain:       DomainContainerImageIdentity,
		Cause:        "dockerfile observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if got, want := len(derivedFromWriter.writeRows), 1; got != want {
		t.Fatalf("WriteDerivedFromEdges rows = %#v, want exactly %d row", derivedFromWriter.writeRows, want)
	}
	rows := derivedFromWriter.writeRows[0]
	if got, want := len(rows), 1; got != want {
		t.Fatalf("WriteDerivedFromEdges row count = %d, want %d (a verified SLSA attestation alone must produce a lineage edge): %#v", got, want, rows)
	}
	row := rows[0]
	if got, want := row["digest"], childDigest; got != want {
		t.Fatalf("row[digest] = %v, want %q (the SLSA-attested child)", got, want)
	}
	if got, want := row["base_digest"], baseDigest; got != want {
		t.Fatalf("row[base_digest] = %v, want %q (the Dockerfile base)", got, want)
	}
}
