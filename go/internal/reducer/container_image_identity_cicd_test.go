// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildContainerImageIdentityDecisionsConsumesCICDArtifactDigestWithRepositoryAnchor(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		ciRunFact("run-image", "github_actions", "repo-api", "abc123"),
		ciArtifactFact("artifact-image", "run-image", testContainerDigest),
		ociManifestFact("oci-manifest", testContainerDigest),
	})

	got := decisionsByRef(decisions)
	decision := got["registry.example.com/team/api@"+testContainerDigest]
	assertContainerImageDecision(t, decision,
		ContainerImageIdentityExactDigest, testContainerDigest, 1)
	if !stringSliceContains(decision.SourceRepositoryIDs, "repo-api") {
		t.Fatalf("SourceRepositoryIDs = %#v, want repo-api", decision.SourceRepositoryIDs)
	}
	if !stringSliceContains(decision.EvidenceFactIDs, "ci.run:run-image") ||
		!stringSliceContains(decision.EvidenceFactIDs, "artifact-image") ||
		!stringSliceContains(decision.EvidenceFactIDs, "oci-manifest") {
		t.Fatalf("EvidenceFactIDs = %#v, want run, artifact, and OCI evidence", decision.EvidenceFactIDs)
	}
}

func TestBuildContainerImageIdentityDecisionsPrefersCICDArtifactDigestOverMutableTag(t *testing.T) {
	t.Parallel()

	artifact := ciArtifactFact("artifact-image", "run-image", testContainerDigest)
	artifact.Payload["image_ref"] = "registry.example.com/team/api:prod"
	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		ciRunFact("run-image", "github_actions", "repo-api", "abc123"),
		artifact,
		ociManifestFact("oci-manifest", testContainerDigest),
	})

	got := decisionsByRef(decisions)
	decision := got["registry.example.com/team/api@"+testContainerDigest]
	assertContainerImageDecision(t, decision,
		ContainerImageIdentityExactDigest, testContainerDigest, 1)
	if !stringSliceContains(decision.SourceRepositoryIDs, "repo-api") {
		t.Fatalf("SourceRepositoryIDs = %#v, want repo-api", decision.SourceRepositoryIDs)
	}
}

func TestBuildContainerImageIdentityDecisionsIgnoresNonContainerCICDArtifacts(t *testing.T) {
	t.Parallel()

	artifact := ciArtifactFact("artifact-image", "run-image", testContainerDigest)
	artifact.Payload["artifact_type"] = "coverage_report"
	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		ciRunFact("run-image", "github_actions", "repo-api", "abc123"),
		artifact,
		ociManifestFact("oci-manifest", testContainerDigest),
	})

	if got := len(decisions); got != 0 {
		t.Fatalf("decisions count = %d, want non-container CI artifact ignored", got)
	}
}

func TestBuildContainerImageIdentityDecisionsRejectsDigestOnlyCICDArtifactWhenRegistryDigestIsAmbiguous(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		ciRunFact("run-image", "github_actions", "repo-api", "abc123"),
		ciArtifactFact("artifact-image", "run-image", testContainerDigest),
		ociManifestFact("oci-manifest-api", testContainerDigest),
		ociManifestFactForRepository("oci-manifest-worker", "registry.example.com", "team/worker", testContainerDigest),
	})

	got := decisionsByRef(decisions)
	decision := got["digest:"+testContainerDigest]
	assertContainerImageDecision(t, decision,
		ContainerImageIdentityAmbiguousTag, "", 0)
	if !stringSliceContains(decision.EvidenceFactIDs, "ci.run:run-image") ||
		!stringSliceContains(decision.EvidenceFactIDs, "artifact-image") ||
		!stringSliceContains(decision.EvidenceFactIDs, "oci-manifest-api") ||
		!stringSliceContains(decision.EvidenceFactIDs, "oci-manifest-worker") {
		t.Fatalf("EvidenceFactIDs = %#v, want run, artifact, and both OCI observations", decision.EvidenceFactIDs)
	}
}

func ociManifestFactForRepository(factID string, registry string, repository string, digest string) facts.Envelope {
	envelope := ociManifestFact(factID, digest)
	envelope.ScopeID = "oci-registry://" + registry + "/" + repository
	envelope.Payload["registry"] = registry
	envelope.Payload["repository"] = repository
	envelope.Payload["repository_id"] = envelope.ScopeID
	return envelope
}
