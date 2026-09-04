// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// TestApplyCIRunDigestRevisionPopulatesBuildProvenanceRepositoryIDs is the
// #5810 regression for the THIRD of the three BuildProvenanceRepositoryIDs
// sources (OCI config source label, CI run, verified SLSA -- see
// container_image_identity_provenance.go and container_image_identity_slsa.go
// for the other two, both of which already write BOTH decision.SourceRepositoryIDs
// AND decision.BuildProvenanceRepositoryIDs symmetrically for the digest they
// resolve).
//
// applyCIRunDigestRevision is the "competing decision" half of the CI-run tier
// (container_image_identity_registry.go): a ci.run/ci.artifact pair anchors its
// build repository against the resolved DIGEST (ciRunDigestAnchor, keyed by
// digest, not by ref), so the anchor reaches whichever OTHER decision resolves
// that same digest -- e.g. a deploying repository's content_entity reference to
// a third-party-built image (#5423). Before this fix, that function appended
// the CI run's repository to decision.SourceRepositoryIDs only, never to
// decision.BuildProvenanceRepositoryIDs, unlike applySLSADigestRevision's
// symmetric append (container_image_identity_registry.go:381-399) and
// addCICDArtifactImageReference's own symmetric append for its OWN ref
// (container_image_identity_typed_evidence.go:210-214).
//
// That asymmetry was latent but rarely reachable before #5810: the
// repository-scoped intent never saw ci.artifact/ci.run facts cross-scope at
// all (issue #5810's own problem statement), so applyCIRunDigestRevision's
// digest-keyed anchor map was populated only within a ci.artifact's OWN
// CI-scoped intent, where the artifact's own ref already got both fields set
// directly by addCICDArtifactImageReference. An asymmetric append there left
// a competing decision ambiguous by SourceRepositoryIDs with empty
// BuildProvenanceRepositoryIDs -- a shape
// singleSupplyChainImageSourceRepositoryID (supply_chain_impact_anchor_tier.go)
// resolves to nothing, blanking the supply-chain finding's RepositoryID.
//
// This is a pure decision-level test, deliberately scope-free: with the #5810
// Handle()-level owner filter (loadActiveContainerImageCIFacts,
// container_image_identity_ci_loader.go), a repository-scoped intent only
// ever sees cross-scope CI facts whose run names its OWN repository, so in
// production this anchor map carries either a CI scope's own runs (a
// CI-scoped intent) or the owning repository's runs (an owner-matched
// repository intent) -- both shapes still require the symmetric append this
// test pins.
func TestApplyCIRunDigestRevisionPopulatesBuildProvenanceRepositoryIDs(t *testing.T) {
	t.Parallel()

	const (
		digest         = "sha256:5810c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1"
		deployRepoID   = "repository:r_deploy_5810ci"
		ciBuildRepoID  = "repository:r_ci_build_5810ci"
		deployImageRef = "registry.example.com/team/api@" + digest
	)

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		gitImageRefFact("content-declares-5810ci", deployImageRef),
		ociManifestFact("oci-manifest-5810ci", digest),
		ciRunFact("run-5810ci", "github_actions", ciBuildRepoID, ""),
		ciArtifactFact("artifact-5810ci", "run-5810ci", digest),
	})

	got := decisionsByRef(decisions)
	decision, ok := got[deployImageRef]
	if !ok {
		t.Fatalf("no decision for %q: %#v", deployImageRef, got)
	}
	if !stringSliceContains(decision.BuildProvenanceRepositoryIDs, ciBuildRepoID) {
		t.Fatalf(
			"BuildProvenanceRepositoryIDs = %#v, want %q: a ci.run that reported "+
				"building this digest is build evidence for whichever OTHER decision "+
				"resolves the digest too, matching applySLSADigestRevision's own "+
				"symmetric append",
			decision.BuildProvenanceRepositoryIDs, ciBuildRepoID,
		)
	}
}

// TestContainerImageIdentityHandlerForeignCIRunStaysOutOfRepositoryScope is
// the Handle()-level #5810 owner-filter regression: a repository-scoped intent
// whose OWN scope-local evidence is a content_entity reference to a
// third-party digest, combined with a cross-scope ci.run/ci.artifact pair
// naming a DIFFERENT repository as builder. The cross-scope CI bridge exists
// for exactly one consumer — the owner-scoped DERIVED_FROM projection, whose
// child gate (containerImageDerivedFromRows) only ever reads build provenance
// EQUAL to the owning repository — so a foreign builder's CI facts have no
// consumer in this scope and MUST NOT be folded into its durable identity
// rows. Unfiltered, they did two things the B-7 corpus caught live:
//
//  1. applyCIRunDigestRevision turned the deploying repository's clean,
//     single-entry SourceRepositoryIDs into an ambiguous two-entry set (and,
//     after the symmetrize fix, stamped the foreign builder into
//     BuildProvenanceRepositoryIDs), demoting every deploy row out of the
//     anchor tier the supply-chain-impact pin depends on
//     (supplyChainImageIdentityAnchorTier, #5817) — the winner for the pinned
//     digest flipped from repository:r_217415d9 (the deploying repository) to
//     the builder, exactly the semantic flip #5817's merged adjudication says
//     needs owner sign-off.
//  2. addCICDArtifactImageReference minted a NEW bare-digest identity row in
//     EVERY repository scope that ran an identity refresh, multiplying one CI
//     run's claim across unrelated scopes with no consumer.
//
// The corrected expectation: the deploying repository's decision keeps its
// single-entry SourceRepositoryIDs, gains no foreign build provenance, and no
// bare-digest row for the foreign builder's digest is written in this scope.
func TestContainerImageIdentityHandlerForeignCIRunStaysOutOfRepositoryScope(t *testing.T) {
	t.Parallel()

	const (
		digest        = "sha256:5810c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2"
		deployRepoID  = "repository:r_deploy_5810ci_handler"
		ciBuildRepoID = "repository:r_ci_build_5810ci_handler"
		imageRef      = "registry.example.com/team/api@" + digest
	)

	deployRef := facts.Envelope{
		FactID:           "content-declares-5810ci-handler",
		ScopeID:          deployRepoID,
		GenerationID:     "generation-repo",
		FactKind:         factload.FactKindContentEntity,
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

	loader := &stubContainerImageIdentityFactLoader{
		scopeFacts: []facts.Envelope{deployRef},
		active:     []facts.Envelope{ociManifestFact("oci-manifest-5810ci-handler", digest)},
		ciActive: []facts.Envelope{
			ciRunFact("run-5810ci-handler", "github_actions", ciBuildRepoID, ""),
			ciArtifactFact("artifact-5810ci-handler", "run-5810ci-handler", digest),
		},
	}
	writer := &recordingContainerImageIdentityWriter{}
	handler := ContainerImageIdentityHandler{FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-5810ci-handler",
		ScopeID:      deployRepoID,
		GenerationID: "generation-repo",
		SourceSystem: "git",
		Domain:       reducercontract.DomainContainerImageIdentity,
		Cause:        "deploy manifest observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	got := decisionsByRef(writer.write.Decisions)
	decision, ok := got[imageRef]
	if !ok {
		t.Fatalf("no written decision for %q: %#v", imageRef, got)
	}
	if len(decision.SourceRepositoryIDs) != 1 || decision.SourceRepositoryIDs[0] != deployRepoID {
		t.Fatalf(
			"SourceRepositoryIDs = %#v, want exactly [%q]: a foreign builder's "+
				"cross-scope ci.run must not ambiguate the deploying repository's "+
				"own identity row (it demotes the row out of the supply-chain "+
				"anchor tier the #5817 B-12 pin depends on)",
			decision.SourceRepositoryIDs, deployRepoID,
		)
	}
	if len(decision.BuildProvenanceRepositoryIDs) != 0 {
		t.Fatalf(
			"BuildProvenanceRepositoryIDs = %#v, want empty: this scope's owner "+
				"is not the CI-confirmed builder, and the owner-scoped "+
				"DERIVED_FROM projection can never consume a foreign builder's "+
				"provenance here",
			decision.BuildProvenanceRepositoryIDs,
		)
	}
	// No written decision in this scope — the content ref's, or any row a
	// foreign ci.artifact could have minted — may carry the foreign builder in
	// either repository field.
	for ref, written := range got {
		if stringSliceContains(written.SourceRepositoryIDs, ciBuildRepoID) ||
			stringSliceContains(written.BuildProvenanceRepositoryIDs, ciBuildRepoID) {
			t.Fatalf(
				"decision %q carries foreign builder %q: %#v — cross-scope CI "+
					"facts for a different repository must be filtered out of a "+
					"repository-scoped intent",
				ref, ciBuildRepoID, written,
			)
		}
	}
}

// TestContainerImageIdentityHandlerOwnerMatchedCIRunProvenanceReachesRepositoryScope
// is the positive companion: when the cross-scope ci.run names THIS intent's
// owning repository as builder (the rc-170 container-ci-lineage shape), the
// bridge must deliver the pair, and the bare-digest decision it mints must
// carry the owner in BuildProvenanceRepositoryIDs — that row is what the
// owner-scoped DERIVED_FROM child gate reads.
func TestContainerImageIdentityHandlerOwnerMatchedCIRunProvenanceReachesRepositoryScope(t *testing.T) {
	t.Parallel()

	const (
		digest      = "sha256:5810c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4"
		ownerRepoID = "repository:r_owner_5810ci_handler"
	)

	loader := &stubContainerImageIdentityFactLoader{
		active: []facts.Envelope{ociManifestFact("oci-manifest-5810ci-owner", digest)},
		ciActive: []facts.Envelope{
			ciRunFact("run-5810ci-owner", "github_actions", ownerRepoID, "commit-5810"),
			ciArtifactFact("artifact-5810ci-owner", "run-5810ci-owner", digest),
		},
	}
	writer := &recordingContainerImageIdentityWriter{}
	handler := ContainerImageIdentityHandler{FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-5810ci-owner",
		ScopeID:      ownerRepoID,
		GenerationID: "generation-repo",
		SourceSystem: "git",
		Domain:       reducercontract.DomainContainerImageIdentity,
		Cause:        "repository refresh",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	// The ci.artifact's bare-digest ref resolves against the registry manifest,
	// so the written decision is keyed by the resolved image ref — find it by
	// digest rather than assuming the raw "digest:" ref key survives.
	var decision ContainerImageIdentityDecision
	found := false
	for _, written := range writer.write.Decisions {
		if written.Digest == digest {
			decision = written
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no written decision resolving digest %q: %#v", digest, writer.write.Decisions)
	}
	if !stringSliceContains(decision.BuildProvenanceRepositoryIDs, ownerRepoID) {
		t.Fatalf(
			"BuildProvenanceRepositoryIDs = %#v, want %q: an owner-matched "+
				"cross-scope ci.run is exactly what the #5810 bridge exists to "+
				"deliver (the DERIVED_FROM child gate reads it)",
			decision.BuildProvenanceRepositoryIDs, ownerRepoID,
		)
	}
}
