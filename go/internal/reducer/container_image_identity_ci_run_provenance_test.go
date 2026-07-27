// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
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
// directly by addCICDArtifactImageReference. #5810's cross-scope CI loader
// (container_image_identity_ci_loader.go) now routinely puts a repository's
// OWN content_entity/deploy reference and a cross-scope ci.artifact for the
// SAME digest in the SAME intent's envelope batch together, so
// applyCIRunDigestRevision's asymmetric branch now fires routinely for a real
// deploying repository's decision: it turns a clean, single-entry
// SourceRepositoryIDs (the deploy reference alone) into an ambiguous
// two-entry field (deploy repo + CI-build repo) while leaving
// BuildProvenanceRepositoryIDs empty. singleSupplyChainImageSourceRepositoryID
// (supply_chain_impact_anchor_tier.go) then finds neither field resolvable and
// blanks the supply-chain finding's RepositoryID -- exactly the live B-7
// golden-corpus gate failure ("mcp:list_supply_chain_impact_findings: result
// item missing required field \"repository_id\"").
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

// TestContainerImageIdentityHandlerCrossScopeCIRunDoesNotBlankBuildProvenance is
// the Handle()-level counterpart, exercising the real #5810 cross-scope shape
// end to end rather than a single BuildContainerImageIdentityDecisions call: a
// repository-scoped intent whose OWN scope-local evidence is a content_entity
// reference to a third-party digest, combined with a CI run/artifact pair
// (for a DIFFERENT repository) reachable ONLY through the new
// activeContainerImageCIFactLoader cross-scope bridge -- exactly the shape a
// single-call decision test cannot exercise, since it collapses scope
// separation.
func TestContainerImageIdentityHandlerCrossScopeCIRunDoesNotBlankBuildProvenance(t *testing.T) {
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

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-5810ci-handler",
		ScopeID:      deployRepoID,
		GenerationID: "generation-repo",
		SourceSystem: "git",
		Domain:       DomainContainerImageIdentity,
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
	if !stringSliceContains(decision.BuildProvenanceRepositoryIDs, ciBuildRepoID) {
		t.Fatalf(
			"BuildProvenanceRepositoryIDs = %#v, want %q: the cross-scope ci.run "+
				"join must not leave build provenance empty while ambiguating "+
				"SourceRepositoryIDs (%#v) -- that shape blanks the downstream "+
				"supply-chain finding's RepositoryID",
			decision.BuildProvenanceRepositoryIDs, ciBuildRepoID, decision.SourceRepositoryIDs,
		)
	}
}
