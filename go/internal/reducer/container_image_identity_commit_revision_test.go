// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildContainerImageIdentityDecisionsThreadsCICDRunCommitAsSourceRevision
// is the #5423 regression: an unlabeled image (no OCI config source label) whose
// digest is matched to a ci.run must carry that run's commit SHA as the
// decision's SourceRevision, with provenance recorded as ci_run_commit so the
// CI-derived tier stays distinguishable from an OCI-config-label revision.
func TestBuildContainerImageIdentityDecisionsThreadsCICDRunCommitAsSourceRevision(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		ciRunFact("run-image", "github_actions", "repo-api", "abc123def456"),
		ciArtifactFact("artifact-image", "run-image", testContainerDigest),
		ociManifestFact("oci-manifest", testContainerDigest),
	})

	got := decisionsByRef(decisions)
	decision := got["registry.example.com/team/api@"+testContainerDigest]
	assertContainerImageDecision(t, decision,
		ContainerImageIdentityExactDigest, testContainerDigest, 1)
	if decision.SourceRevision != "abc123def456" {
		t.Fatalf("SourceRevision = %q, want the digest-matched run commit abc123def456", decision.SourceRevision)
	}
	if decision.SourceRevisionProvenance != containerImageSourceRevisionCIRunCommit {
		t.Fatalf("SourceRevisionProvenance = %q, want %q",
			decision.SourceRevisionProvenance, containerImageSourceRevisionCIRunCommit)
	}
}

// TestBuildContainerImageIdentityDecisionsThreadsCICDRunCommitWhenImageAlsoDeclaredByContentEntity
// is the golden-corpus regression: a deploy repo's content_entity declares the
// same image by digest, producing a competing identity decision that resolves to
// the same image_ref and outcome (and therefore the same durable stable fact
// key) as the ci.artifact's bare-digest decision. The ci.run commit must survive
// on the resolved image regardless of which decision wins the write-time upsert.
func TestBuildContainerImageIdentityDecisionsThreadsCICDRunCommitWhenImageAlsoDeclaredByContentEntity(t *testing.T) {
	t.Parallel()

	imageRef := "registry.example.com/team/api@" + testContainerDigest
	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		ciRunFact("run-image", "github_actions", "repository:r_build", "abc123def456"),
		ciArtifactFact("artifact-image", "run-image", testContainerDigest),
		// A deploy repo declares the same image by digest, competing for the
		// resolved image_ref with no commit of its own.
		gitImageRefFact("content-declares", imageRef),
		ociManifestFact("oci-manifest", testContainerDigest),
	})

	var resolved int
	for _, decision := range decisions {
		if decision.Digest != testContainerDigest || decision.Outcome != ContainerImageIdentityExactDigest {
			continue
		}
		resolved++
		if decision.SourceRevision != "abc123def456" {
			t.Fatalf("decision %q SourceRevision = %q, want the run commit to survive the competing content_entity decision",
				decision.ImageRef, decision.SourceRevision)
		}
		if decision.SourceRevisionProvenance != containerImageSourceRevisionCIRunCommit {
			t.Fatalf("decision %q SourceRevisionProvenance = %q, want %q",
				decision.ImageRef, decision.SourceRevisionProvenance, containerImageSourceRevisionCIRunCommit)
		}
		if !stringSliceContains(decision.SourceRepositoryIDs, "repository:r_build") {
			t.Fatalf("decision %q SourceRepositoryIDs = %#v, want the ci.run build repo",
				decision.ImageRef, decision.SourceRepositoryIDs)
		}
	}
	if resolved == 0 {
		t.Fatal("no exact_digest decision resolved for the shared digest")
	}
}

// TestBuildContainerImageIdentityDecisionsPrefersOCILabelRevisionOverCICDRunCommit
// pins the tier ordering: when an OCI config source label already supplies the
// revision, that higher-strength provenance wins and the CI-run commit does not
// override it.
func TestBuildContainerImageIdentityDecisionsPrefersOCILabelRevisionOverCICDRunCommit(t *testing.T) {
	t.Parallel()

	manifest := ociManifestWithConfigLabels("oci-manifest", testContainerDigest, map[string]string{
		"org.opencontainers.image.source":   "https://github.com/acme/payments-api",
		"org.opencontainers.image.revision": "0123456789abcdef0123456789abcdef01234567",
	})
	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		repositoryRemoteFact("repo://acme/payments-api", "https://github.com/acme/payments-api.git"),
		ciRunFact("run-image", "github_actions", "repo-api", "abc123def456"),
		ciArtifactFact("artifact-image", "run-image", testContainerDigest),
		manifest,
	})

	got := decisionsByRef(decisions)
	decision := got["registry.example.com/team/api@"+testContainerDigest]
	if decision.SourceRevision != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("SourceRevision = %q, want OCI label revision to win", decision.SourceRevision)
	}
	if decision.SourceRevisionProvenance != containerImageSourceRevisionOCIConfigLabel {
		t.Fatalf("SourceRevisionProvenance = %q, want %q",
			decision.SourceRevisionProvenance, containerImageSourceRevisionOCIConfigLabel)
	}
}

// TestBuildContainerImageIdentityDecisionsRefusesAmbiguousCICDRunCommits proves
// the reducer will not invent a revision: two runs whose artifacts carry the
// same digest but different commits (a rebuild) leave SourceRevision empty
// rather than picking one arbitrarily.
func TestBuildContainerImageIdentityDecisionsRefusesAmbiguousCICDRunCommits(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		ciRunFact("run-a", "github_actions", "repo-api", "commit-a"),
		ciArtifactFact("artifact-a", "run-a", testContainerDigest),
		ciRunFact("run-b", "github_actions", "repo-api", "commit-b"),
		ciArtifactFact("artifact-b", "run-b", testContainerDigest),
		ociManifestFact("oci-manifest", testContainerDigest),
	})

	got := decisionsByRef(decisions)
	decision := got["registry.example.com/team/api@"+testContainerDigest]
	if decision.SourceRevision != "" {
		t.Fatalf("SourceRevision = %q, want empty when two commits claim the same digest", decision.SourceRevision)
	}
	if decision.SourceRevisionProvenance != "" {
		t.Fatalf("SourceRevisionProvenance = %q, want empty under ambiguity", decision.SourceRevisionProvenance)
	}
}
