// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// This file is the #5451 regression suite: it proves that an aws_image_reference
// fact shaped like the one the ECS scanner now emits per running task container
// (go/internal/collector/awscloud/services/ecs/image_reference.go) resolves
// through the SAME digest-keyed evidence path the ECR scanner's
// aws_image_reference already exercises (addAWSImageReference,
// container_image_identity_typed_evidence.go). No reducer production code
// changes for #5451 — the dispatch in extractContainerImageRefsWithQuarantine
// already matches on facts.AWSImageReferenceFactKind regardless of which AWS
// service scanner produced the fact; this suite is the proof that promise
// actually holds for an ECS-shaped observation.

const (
	ecsImageAccountID      = "123456789012"
	ecsImageRegion         = "us-east-1"
	ecsImageRepositoryName = "supply-chain-demo"
	ecsImageRegistry       = ecsImageAccountID + ".dkr.ecr." + ecsImageRegion + ".amazonaws.com"
)

// awsECSTaskImageReferenceFact builds the aws_image_reference fact an ECS task
// container's running digest produces (see runningContainerImageReferences),
// keyed on the same account/region/repository/digest/tag fields
// addAWSImageReference reads.
func awsECSTaskImageReferenceFact(factID, digest, tag string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.AWSImageReferenceFactKind,
		SourceConfidence: facts.SourceConfidenceObserved,
		Payload: map[string]any{
			"account_id":      ecsImageAccountID,
			"region":          ecsImageRegion,
			"service_kind":    "ecs",
			"repository_name": ecsImageRepositoryName,
			"registry_id":     ecsImageAccountID,
			"image_digest":    digest,
			"manifest_digest": digest,
			"tag":             tag,
		},
	}
}

// TestBuildContainerImageIdentityDecisionsResolvesECSTaskImageReferenceExactDigest
// is the #5451 non-vacuity proof: a running ECS task container's
// aws_image_reference, joined to a matching oci_registry.image_manifest at the
// same registry/repository@digest, resolves ExactDigest with one canonical
// write — the running task's digest lands on the graph identity, not just
// buried in the task's aws_resource containers[] attribute.
func TestBuildContainerImageIdentityDecisionsResolvesECSTaskImageReferenceExactDigest(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		awsECSTaskImageReferenceFact("aws-image-ecs-task", testContainerDigest, "latest"),
		ociManifestFactForRepository("oci-manifest-ecs", ecsImageRegistry, ecsImageRepositoryName, testContainerDigest),
	})

	got := decisionsByRef(decisions)
	decision := got[ecsImageRegistry+"/"+ecsImageRepositoryName+"@"+testContainerDigest]
	assertContainerImageDecision(t, decision,
		ContainerImageIdentityExactDigest, testContainerDigest, 1)
}

// TestBuildContainerImageIdentityDecisionsJoinsECSTaskImageReferenceToCICDRunCommit
// proves the ECS-sourced aws_image_reference also joins repo/commit: a ci.run +
// ci.artifact carrying the same digest attaches SourceRevision with
// ci_run_commit provenance, and the run's repository lands in
// SourceRepositoryIDs, exactly like the pre-existing ECR-sourced path
// (TestBuildContainerImageIdentityDecisionsThreadsCICDRunCommitAsSourceRevision).
func TestBuildContainerImageIdentityDecisionsJoinsECSTaskImageReferenceToCICDRunCommit(t *testing.T) {
	t.Parallel()

	const synthethicRepositoryID = "repository:r_ecs_supply_chain_demo"
	const synthethicCommitSHA = "abc123ecs456def789"

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		awsECSTaskImageReferenceFact("aws-image-ecs-task", testContainerDigest, "latest"),
		ociManifestFactForRepository("oci-manifest-ecs", ecsImageRegistry, ecsImageRepositoryName, testContainerDigest),
		ciRunFact("run-ecs-image", "github_actions", synthethicRepositoryID, synthethicCommitSHA),
		ciArtifactFact("artifact-ecs-image", "run-ecs-image", testContainerDigest),
	})

	got := decisionsByRef(decisions)
	decision := got[ecsImageRegistry+"/"+ecsImageRepositoryName+"@"+testContainerDigest]
	assertContainerImageDecision(t, decision,
		ContainerImageIdentityExactDigest, testContainerDigest, 1)
	if decision.SourceRevision != synthethicCommitSHA {
		t.Fatalf("SourceRevision = %q, want the digest-matched run commit %q", decision.SourceRevision, synthethicCommitSHA)
	}
	if decision.SourceRevisionProvenance != containerImageSourceRevisionCIRunCommit {
		t.Fatalf("SourceRevisionProvenance = %q, want %q",
			decision.SourceRevisionProvenance, containerImageSourceRevisionCIRunCommit)
	}
	if !stringSliceContains(decision.SourceRepositoryIDs, synthethicRepositoryID) {
		t.Fatalf("SourceRepositoryIDs = %#v, want %q", decision.SourceRepositoryIDs, synthethicRepositoryID)
	}
}

// TestBuildContainerImageIdentityDecisionsMeasuresECSDigestJoinCardinalityBeforeAndAfterEmitter
// is the #5451 before/after cardinality proof: the SAME envelope corpus
// (an ECR-registry OCI manifest for the ECS task's digest, with no CI
// evidence at all — the common real-world case for a running ECS task with no
// recorded ci.artifact) is built twice, toggling only whether the ECS
// scanner's aws_image_reference fact is present. Before: zero
// ExactDigest/canonical-write decisions for the digest, because the digest
// existed only inside the ecs.task aws_resource containers[] attribute the
// resolver never reads. After: exactly one, with one canonical write.
func TestBuildContainerImageIdentityDecisionsMeasuresECSDigestJoinCardinalityBeforeAndAfterEmitter(t *testing.T) {
	t.Parallel()

	ociEvidence := ociManifestFactForRepository("oci-manifest-ecs", ecsImageRegistry, ecsImageRepositoryName, testContainerDigest)

	before := BuildContainerImageIdentityDecisions([]facts.Envelope{ociEvidence})
	beforeExactDigest, beforeCanonicalWrites := countExactDigestDecisions(before, testContainerDigest)
	if beforeExactDigest != 0 || beforeCanonicalWrites != 0 {
		t.Fatalf("before emitter: exact_digest decisions = %d, canonical writes = %d, want 0 and 0 (buried aws_resource digest is not an evidence source)",
			beforeExactDigest, beforeCanonicalWrites)
	}

	after := BuildContainerImageIdentityDecisions([]facts.Envelope{
		ociEvidence,
		awsECSTaskImageReferenceFact("aws-image-ecs-task", testContainerDigest, "latest"),
	})
	afterExactDigest, afterCanonicalWrites := countExactDigestDecisions(after, testContainerDigest)
	if afterExactDigest != 1 || afterCanonicalWrites != 1 {
		t.Fatalf("after emitter: exact_digest decisions = %d, canonical writes = %d, want 1 and 1",
			afterExactDigest, afterCanonicalWrites)
	}
}

func countExactDigestDecisions(decisions []ContainerImageIdentityDecision, digest string) (exactDigestCount int, canonicalWrites int) {
	for _, decision := range decisions {
		if decision.Digest != digest || decision.Outcome != ContainerImageIdentityExactDigest {
			continue
		}
		exactDigestCount++
		canonicalWrites += decision.CanonicalWrites
	}
	return exactDigestCount, canonicalWrites
}

// TestBuildContainerImageIdentityDecisionsLeavesECSTaskImageReferenceUnresolvedWithoutOCIEvidence
// pins the BEFORE state this issue fixes: without a matching
// oci_registry.image_manifest, the ECS aws_image_reference alone does not
// produce an ExactDigest decision for its image_ref — it needs the OCI
// registry join, same as any other aws_image_reference. This is the
// non-vacuity control: it is what the golden-corpus cassette looked like
// before the #5451 OCI/ci.run fixtures were added.
func TestBuildContainerImageIdentityDecisionsLeavesECSTaskImageReferenceUnresolvedWithoutOCIEvidence(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		awsECSTaskImageReferenceFact("aws-image-ecs-task", testContainerDigest, "latest"),
	})

	got := decisionsByRef(decisions)
	imageRef := ecsImageRegistry + "/" + ecsImageRepositoryName + "@" + testContainerDigest
	if decision, ok := got[imageRef]; ok && decision.Outcome == ContainerImageIdentityExactDigest {
		t.Fatalf("decision for %q = %#v, want no ExactDigest outcome without OCI registry evidence", imageRef, decision)
	}
}
