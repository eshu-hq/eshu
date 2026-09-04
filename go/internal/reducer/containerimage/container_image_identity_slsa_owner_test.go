// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestContainerImageIdentityHandlerForeignSLSAAnchorStaysOutOfRepositoryScope
// is the SLSA sibling of
// TestContainerImageIdentityHandlerForeignCIRunStaysOutOfRepositoryScope
// (container_image_identity_ci_run_provenance_test.go): the cross-scope SLSA
// bridge (activeContainerImageSLSAFactLoader) was left unfiltered while the
// #5810 follow-up owner-gated the CI bridge for exactly the same reason --
// addSLSADigestRefs (container_image_identity_slsa_refs.go) synthesizes a
// bare-digest ref for EVERY verified SLSA anchor, including one naming a
// repository the current intent does not own, so a repository-scoped
// refresh with NO evidence at all for a digest could still mint a durable
// identity row for it purely because some OTHER repository's SLSA
// attestation was active somewhere in the system.
//
// This fixture gives the owner repository's intent NO scope-local evidence
// whatsoever for childDigest -- no content_entity, no OCI manifest, no
// ci.artifact -- so the ONLY way a decision for childDigest can appear is
// the bare-digest synthesis path. The active SLSA anchor names a DIFFERENT
// repository (foreignRepoID) as the build's config-source repository. The
// owning repository must see nothing: no decision, no BuildProvenanceRepositoryIDs
// leak, and definitely no foreign repository ID anywhere in what gets
// written.
func TestContainerImageIdentityHandlerForeignSLSAAnchorStaysOutOfRepositoryScope(t *testing.T) {
	t.Parallel()

	const (
		childDigest   = "sha256:5810s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1s1"
		ownerRepoID   = "repository:r_owner_5810slsa_handler"
		foreignRepoID = "repository:r_foreign_5810slsa_handler"
	)

	loader := &stubContainerImageIdentityFactLoader{
		// The owning intent's OWN scope-local evidence: deliberately empty --
		// no content_entity, no OCI manifest, no ci.artifact -- so any
		// decision naming childDigest can only have come from the cross-scope
		// SLSA bridge.
		// The foreign repository's identity (for matchOCIConfigSourceRepository
		// to resolve the SLSA provenance's config source to it) plus an OCI
		// registry observation for childDigest, so the synthesized ref -- if
		// the gate did not exist -- would resolve to exact_digest and reach a
		// durable canonical write, not merely an "unresolved" in-memory row.
		active: []facts.Envelope{
			repositoryRemoteFact(foreignRepoID, slsaProofRepoURL+".git"),
			ociManifestFact("oci-manifest-slsa-foreign", childDigest),
		},
		// The cross-scope SLSA bridge: a verified attestation naming
		// foreignRepoID -- NOT ownerRepoID -- as childDigest's build
		// provenance. Nothing else in this fixture references childDigest.
		slsaActive: []facts.Envelope{
			slsaImageStatementFact("statement-slsa-foreign", "stmt-slsa-foreign", childDigest),
			slsaConfigSourceProvenanceFact("provenance-slsa-foreign", "stmt-slsa-foreign", slsaProofRepoURL, slsaProofCommit),
			slsaPassedVerificationFact("verification-slsa-foreign", "stmt-slsa-foreign"),
		},
	}
	writer := &recordingContainerImageIdentityWriter{}
	handler := ContainerImageIdentityHandler{FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-5810slsa-foreign",
		ScopeID:      ownerRepoID,
		GenerationID: "generation-repo-slsa-foreign",
		SourceSystem: "git",
		Domain:       reducercontract.DomainContainerImageIdentity,
		Cause:        "repository refresh",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	for _, written := range writer.write.Decisions {
		if written.Digest == childDigest {
			t.Fatalf(
				"unexpected decision for foreign-SLSA-only digest %q: %#v -- a "+
					"repository-scoped intent with NO evidence of its own must not "+
					"gain a bare-digest identity row purely from another "+
					"repository's SLSA attestation",
				childDigest, written,
			)
		}
		if stringSliceContains(written.SourceRepositoryIDs, foreignRepoID) ||
			stringSliceContains(written.BuildProvenanceRepositoryIDs, foreignRepoID) {
			t.Fatalf(
				"decision %#v carries foreign repository %q: cross-scope SLSA "+
					"evidence for a different repository must be filtered out of a "+
					"repository-scoped intent's synthesized refs",
				written, foreignRepoID,
			)
		}
	}
}
