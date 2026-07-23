// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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

// TestPostgresContainerImageIdentityWriterDedupSurvivorForConvergedECSAndCICDArtifactDecisions
// is the hostile-review P1 dedup-survivor verification for #5451. When BOTH
// the ECS scanner's aws_image_reference AND a ci.artifact resolve the same
// digest to the same final image_ref+outcome, they raise TWO raw
// ContainerImageIdentityDecisions that converge on the same
// containerImageIdentityStableFactKey/FactID (scope+generation+image_ref+
// outcome — identity_strength is NOT part of that key). reducerBatchInsertFacts
// -> dedupeReducerFactRowsByFactID then collapses them to ONE persisted row,
// keeping the LAST occurrence by index (reducer_fact_batch_insert.go
// last-write-wins).
//
// This test proves which decision survives is DETERMINISTIC, not order luck:
// extractContainerImageRefsWithQuarantine's byRef map is keyed by two
// LEXICOGRAPHICALLY DISTINCT strings before classification (the aws ref's
// full "<registry>/<repo>@<digest>" vs the ci.artifact's bare "digest:<digest>"
// key), so the first sort.SliceStable orders them by that distinct key
// regardless of Go's randomized map iteration; the digest-prefixed bare-digest
// ref always sorts after a registry-hostname ref that starts with a digit.
// Both are then classified in that fixed order, the ci.artifact one gets its
// ImageRef REWRITTEN to the same value as the aws one
// (classifyContainerImageRef's single-registry-observation branch), and the
// final stable sort (now comparing EQUAL keys) preserves that fixed relative
// order. So the ci.artifact-derived decision (identity_strength =
// "artifact_digest_with_registry_observation") is always LAST and always wins
// the write-time dedup — the emitter's own "explicit_digest" decision is
// silently dropped from the persisted fact, even though the emitter fact
// itself was present and correctly classified upstream.
//
// This is why the golden-corpus assertion MUST NOT key on identity_strength
// == explicit_digest while the ci.run/ci.artifact fixtures for this same
// digest are present (see PR review, #5451 P1): that persisted value would
// never be explicit_digest in the golden corpus fixture set, so the fix is to
// drop the converging ci.artifact/ci.run fixtures for the ECS digest instead
// (see the cassette/snapshot changes), leaving the emitter's
// aws_image_reference + OCI registry observation as the ONLY evidence for that
// digest so the persisted identity_strength is unambiguously explicit_digest.
func TestPostgresContainerImageIdentityWriterDedupSurvivorForConvergedECSAndCICDArtifactDecisions(t *testing.T) {
	t.Parallel()

	const synthethicRepositoryID = "repository:r_ecs_supply_chain_demo"
	const synthethicCommitSHA = "abc123ecs456def789"
	imageRef := ecsImageRegistry + "/" + ecsImageRepositoryName + "@" + testContainerDigest

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		awsECSTaskImageReferenceFact("aws-image-ecs-task", testContainerDigest, "latest"),
		ociManifestFactForRepository("oci-manifest-ecs", ecsImageRegistry, ecsImageRepositoryName, testContainerDigest),
		ciRunFact("run-ecs-image", "github_actions", synthethicRepositoryID, synthethicCommitSHA),
		ciArtifactFact("artifact-ecs-image", "run-ecs-image", testContainerDigest),
	})

	// Confirm the premise: two raw decisions actually converge on the same
	// image_ref+outcome before write-time dedup, one from each evidence
	// source, with the two distinct identity_strength values the review
	// flagged.
	var converged []ContainerImageIdentityDecision
	for _, decision := range decisions {
		if decision.ImageRef == imageRef && decision.Outcome == ContainerImageIdentityExactDigest {
			converged = append(converged, decision)
		}
	}
	if len(converged) != 2 {
		t.Fatalf("raw decisions converging on %q = %d, want 2 (aws_image_reference + ci.artifact)", imageRef, len(converged))
	}
	if converged[0].IdentityStrength != "explicit_digest" {
		t.Fatalf("converged[0].IdentityStrength = %q, want %q (the aws_image_reference-derived decision)",
			converged[0].IdentityStrength, "explicit_digest")
	}
	if converged[1].IdentityStrength != "artifact_digest_with_registry_observation" {
		t.Fatalf("converged[1].IdentityStrength = %q, want %q (the ci.artifact-derived decision)",
			converged[1].IdentityStrength, "artifact_digest_with_registry_observation")
	}

	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{DB: db, Now: func() time.Time { return now }}

	if _, err := writer.WriteContainerImageIdentityDecisions(context.Background(), ContainerImageIdentityWrite{
		IntentID:     "intent-ecs-image-identity",
		ScopeID:      "aws:123456789012:us-east-1:ecs",
		GenerationID: "generation-ecs",
		SourceSystem: "aws",
		Cause:        "container image references observed",
		Decisions:    decisions,
	}); err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}

	var persisted []decodedBatchedFactRow
	for _, row := range decodeBatchedFactCalls(t, db.execs) {
		var payload map[string]any
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload["image_ref"] == imageRef {
			persisted = append(persisted, row)
		}
	}
	if len(persisted) != 1 {
		t.Fatalf("persisted rows for %q = %d, want 1 (write-time dedup must collapse the converged pair to one fact_id)",
			imageRef, len(persisted))
	}
	var payload map[string]any
	if err := json.Unmarshal(persisted[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal persisted payload: %v", err)
	}
	// The critical, currently-true assertion: the SURVIVING persisted row
	// carries the ci.artifact's identity_strength, not the emitter's. A golden
	// assertion that expects "explicit_digest" here would fail even with the
	// #5451 emitter present and working correctly, which is exactly the P1
	// the hostile review caught.
	if got, want := payload["identity_strength"], "artifact_digest_with_registry_observation"; got != want {
		t.Fatalf("persisted identity_strength = %#v, want %q (the ci.artifact decision, not the emitter's explicit_digest, survives write-time dedup)",
			got, want)
	}
}

// awsTaskDefinitionUsesImageRelationshipFact builds the aws_relationship fact
// the ECS scanner already emits per task-definition container image
// (taskDefinitionImageRelationships, RelationshipECSTaskDefinitionUsesImage):
// target_type "container_image", target_resource_id the raw TAG-only image
// string (never a digest), and no resolved_image_uri attribute (ECS does not
// set that; only the Lambda scanner does). This is present in the real
// golden-corpus cassette alongside the new #5451 aws_image_reference fact, so
// the fixed golden-corpus fixture set (post-P1-fix, no ci.artifact/ci.run for
// this digest) is proven here to still resolve to exactly one persisted
// explicit_digest identity — this tag-based relationship ref does not collide
// with or dilute the digest-based one.
func awsTaskDefinitionUsesImageRelationshipFact(factID, image string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.AWSRelationshipFactKind,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"relationship_type":  "ecs_task_definition_uses_image",
			"source_resource_id": "arn:aws:ecs:us-east-1:123456789012:task-definition/supply-chain-demo:1",
			"target_resource_id": image,
			"target_type":        "container_image",
			"attributes": map[string]any{
				"container_names": []string{"supply-chain-demo"},
			},
		},
	}
}

// TestPostgresContainerImageIdentityWriterPersistsExplicitDigestForFixedECSGoldenFixtureSet
// is the #5451 P1-fix re-proof: this is the ACTUAL fixture combination the
// corrected golden corpus now carries for the ECS digest (aws_image_reference
// + the matching oci_registry.image_manifest + the pre-existing ECS
// task-definition-to-image aws_relationship, but NO ci.artifact/ci.run for
// this digest — that convergence was removed specifically because it caused
// the ci.artifact decision to win write-time dedup, per
// TestPostgresContainerImageIdentityWriterDedupSurvivorForConvergedECSAndCICDArtifactDecisions
// above). With the ci.artifact collision removed, exactly one row persists for
// the digest-based image_ref and it carries identity_strength=explicit_digest
// — proving the corrected golden assertion (group_by=identity_strength,
// digest filter, required explicit_digest bucket) will actually observe what
// it claims to.
func TestPostgresContainerImageIdentityWriterPersistsExplicitDigestForFixedECSGoldenFixtureSet(t *testing.T) {
	t.Parallel()

	imageRef := ecsImageRegistry + "/" + ecsImageRepositoryName + "@" + testContainerDigest
	tagOnlyImage := ecsImageRegistry + "/" + ecsImageRepositoryName + ":latest"

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		awsECSTaskImageReferenceFact("aws-image-ecs-task", testContainerDigest, "latest"),
		ociManifestFactForRepository("oci-manifest-ecs", ecsImageRegistry, ecsImageRepositoryName, testContainerDigest),
		awsTaskDefinitionUsesImageRelationshipFact("aws-relationship-ecs-taskdef-image", tagOnlyImage),
	})

	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{DB: db, Now: func() time.Time { return now }}

	if _, err := writer.WriteContainerImageIdentityDecisions(context.Background(), ContainerImageIdentityWrite{
		IntentID:     "intent-ecs-image-identity-fixed",
		ScopeID:      "aws:123456789012:us-east-1:ecs",
		GenerationID: "generation-ecs",
		SourceSystem: "aws",
		Cause:        "container image references observed",
		Decisions:    decisions,
	}); err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}

	var persisted []decodedBatchedFactRow
	for _, row := range decodeBatchedFactCalls(t, db.execs) {
		var payload map[string]any
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload["digest"] == testContainerDigest {
			persisted = append(persisted, row)
		}
	}
	if len(persisted) != 1 {
		t.Fatalf("persisted rows for digest %q = %d, want 1 (the tag-only relationship ref must not collide with the digest ref)",
			testContainerDigest, len(persisted))
	}
	var payload map[string]any
	if err := json.Unmarshal(persisted[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal persisted payload: %v", err)
	}
	if got, want := payload["image_ref"], imageRef; got != want {
		t.Fatalf("persisted image_ref = %#v, want %q", got, want)
	}
	if got, want := payload["identity_strength"], "explicit_digest"; got != want {
		t.Fatalf("persisted identity_strength = %#v, want %q", got, want)
	}
	if got, want := payload["outcome"], string(ContainerImageIdentityExactDigest); got != want {
		t.Fatalf("persisted outcome = %#v, want %q", got, want)
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
