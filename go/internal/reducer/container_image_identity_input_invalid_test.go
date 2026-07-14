// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// This file is the flagship regression suite for the cross-provider
// image_reference family's typed-decode migration (#4685, Contract System v1,
// deferred from Wave 4a/4d — see container_image_identity_evidence.go). It
// proves the accuracy guarantee the migration exists to protect AND the
// per-fact isolation contract every prior wave established: a fact of any of
// the migrated kinds (aws_image_reference, azure_image_reference,
// gcp_image_reference, ci.artifact, ci.workflow_image_evidence, ci.run)
// missing its required identity field is QUARANTINED as a visible
// input_invalid dead-letter — never silently producing an empty or malformed
// image identity — while a VALID sibling fact in the same batch still
// produces a container-image-identity decision.
//
// Before this migration, addAWSImageReference built a registry string from
// whatever payloadStr returned for "region" (including "" for an absent key),
// silently producing a malformed reference like
// "123456789012.dkr.ecr..amazonaws.com/team/api@sha256:...", with no
// operator-visible signal the fact was malformed.

// TestContainerImageIdentityHandlerQuarantinesAWSImageReferenceMissingRegion
// proves the flagship silent-bug the migration fixes: an aws_image_reference
// fact missing its required region key must dead-letter, not build a
// malformed two-dot registry hostname.
func TestContainerImageIdentityHandlerQuarantinesAWSImageReferenceMissingRegion(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-aws-image",
		FactKind: facts.AWSImageReferenceFactKind,
		Payload: map[string]any{
			// "region" intentionally absent.
			"account_id":      "123456789012",
			"repository_name": "team/api",
			"image_digest":    testContainerDigest,
			"manifest_digest": testContainerDigest,
		},
	}
	valid := gitImageRefFact("git-valid", "registry.example.com/team/api@"+testContainerDigest)

	result, writer := runContainerImageIdentityHandler(t, malformed, valid)
	assertOneInputInvalidFact(t, result)
	assertWriterHasImageRefDecision(t, writer, "registry.example.com/team/api@"+testContainerDigest)
}

// TestContainerImageIdentityHandlerQuarantinesAzureImageReferenceMissingOwningARMResourceID
// proves the same guarantee for azure_image_reference: OwningARMResourceID is
// the field the collector always populates (azurecloud.NewImageReferenceEnvelope
// fails closed on it), so its absence is malformed input, not a valid runtime
// observation.
func TestContainerImageIdentityHandlerQuarantinesAzureImageReferenceMissingOwningARMResourceID(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-azure-image",
		FactKind: facts.AzureImageReferenceFactKind,
		Payload: map[string]any{
			// "owning_arm_resource_id" intentionally absent.
			"owning_normalized_id":  "/subscriptions/demo/resourcegroups/rg/providers/microsoft.app/containerapps/api",
			"owning_resource_type":  "microsoft.app/containerapps",
			"tag_digest_confidence": "digest",
			"image_reference":       testAzureImageRepository + ":prod",
			"image_digest":          testContainerDigest,
		},
	}
	valid := gitImageRefFact("git-valid", "registry.example.com/team/api@"+testContainerDigest)

	result, writer := runContainerImageIdentityHandler(t, malformed, valid)
	assertOneInputInvalidFact(t, result)
	assertWriterHasImageRefDecision(t, writer, "registry.example.com/team/api@"+testContainerDigest)
}

// TestContainerImageIdentityHandlerQuarantinesGCPImageReferenceMissingOwningFullResourceName
// proves the same guarantee for gcp_image_reference.
func TestContainerImageIdentityHandlerQuarantinesGCPImageReferenceMissingOwningFullResourceName(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-gcp-image",
		FactKind: facts.GCPImageReferenceFactKind,
		Payload: map[string]any{
			// "owning_full_resource_name" intentionally absent.
			"tag_digest_confidence": "digest",
			"image_reference":       testGCPImageRepository + ":prod",
			"image_digest":          testContainerDigest,
		},
	}
	valid := gitImageRefFact("git-valid", "registry.example.com/team/api@"+testContainerDigest)

	result, writer := runContainerImageIdentityHandler(t, malformed, valid)
	assertOneInputInvalidFact(t, result)
	assertWriterHasImageRefDecision(t, writer, "registry.example.com/team/api@"+testContainerDigest)
}

// TestContainerImageIdentityHandlerQuarantinesCICDArtifactMissingRunID proves
// the guarantee for ci.artifact: a missing run_id could never join to its
// owning run, so it must dead-letter rather than silently contribute a
// digest-only reference with no repository anchor.
func TestContainerImageIdentityHandlerQuarantinesCICDArtifactMissingRunID(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-artifact",
		FactKind: facts.CICDArtifactFactKind,
		Payload: map[string]any{
			// "run_id" intentionally absent.
			"provider":        "github_actions",
			"run_attempt":     "1",
			"artifact_type":   "container_image",
			"artifact_digest": testContainerDigest,
		},
	}
	valid := gitImageRefFact("git-valid", "registry.example.com/team/api@"+testContainerDigest)

	result, writer := runContainerImageIdentityHandler(t, malformed, valid)
	assertOneInputInvalidFact(t, result)
	assertWriterHasImageRefDecision(t, writer, "registry.example.com/team/api@"+testContainerDigest)
}

// TestContainerImageIdentityHandlerQuarantinesWorkflowImageEvidenceMissingRepositoryID
// proves the guarantee for ci.workflow_image_evidence: RepositoryID is the
// reducer's sole join key attaching workflow image evidence to source
// anchors, so its absence must dead-letter rather than silently contribute an
// unanchored image reference.
func TestContainerImageIdentityHandlerQuarantinesWorkflowImageEvidenceMissingRepositoryID(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-workflow-image",
		FactKind: facts.CICDWorkflowImageEvidenceFactKind,
		Payload: map[string]any{
			// "repository_id" intentionally absent.
			"workflow_path":  ".github/workflows/build.yml",
			"evidence_class": "workflow_image_ref",
			"image_ref":      "registry.example.com/team/other:prod",
		},
	}
	valid := gitImageRefFact("git-valid", "registry.example.com/team/api@"+testContainerDigest)

	result, writer := runContainerImageIdentityHandler(t, malformed, valid)
	assertOneInputInvalidFact(t, result)
	assertWriterHasImageRefDecision(t, writer, "registry.example.com/team/api@"+testContainerDigest)
}

// TestContainerImageIdentityHandlerQuarantinesCICDRunMissingRunID proves the
// guarantee for ci.run: containerImageCIRuns indexes every run by its
// provider/run_id/run_attempt join key so addCICDArtifactImageReference can
// attach a repository anchor to an artifact; a run missing run_id must
// dead-letter rather than silently contributing no anchor (or colliding with
// another run under an empty-string key) with no operator-visible signal.
func TestContainerImageIdentityHandlerQuarantinesCICDRunMissingRunID(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-run",
		FactKind: facts.CICDRunFactKind,
		Payload: map[string]any{
			// "run_id" intentionally absent.
			"provider":      "github_actions",
			"run_attempt":   "1",
			"repository_id": "repo-api",
			"commit_sha":    "abc123",
		},
	}
	valid := gitImageRefFact("git-valid", "registry.example.com/team/api@"+testContainerDigest)

	result, writer := runContainerImageIdentityHandler(t, malformed, valid)
	assertOneInputInvalidFact(t, result)
	assertWriterHasImageRefDecision(t, writer, "registry.example.com/team/api@"+testContainerDigest)
}

// TestContainerImageIdentityHandlerQuarantineReplayIsIdempotent proves
// replaying the exact same batch (including the quarantined malformed
// aws_image_reference fact) through Handle twice converges on the same
// quarantine count and canonical-write count each time — the typed-decode
// migration introduces no new source of nondeterminism into the reducer's
// at-least-once delivery / idempotent-convergence contract
// (docs/internal/design/contract-system-v1.md §3.4).
func TestContainerImageIdentityHandlerQuarantineReplayIsIdempotent(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-aws-image-replay",
		FactKind: facts.AWSImageReferenceFactKind,
		Payload: map[string]any{
			// "region" intentionally absent.
			"account_id":      "123456789012",
			"repository_name": "team/api",
			"image_digest":    testContainerDigest,
			"manifest_digest": testContainerDigest,
		},
	}
	valid := gitImageRefFact("git-valid-replay", "registry.example.com/team/api@"+testContainerDigest)

	intent := Intent{
		IntentID:     "intent-container-image-replay",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-replay",
		SourceSystem: "aws",
		Domain:       DomainContainerImageIdentity,
		Cause:        "container image references observed",
	}

	var results []Result
	for i := 0; i < 2; i++ {
		loader := &stubContainerImageIdentityFactLoader{scopeFacts: []facts.Envelope{malformed, valid}}
		writer := &recordingContainerImageIdentityWriter{}
		handler := ContainerImageIdentityHandler{FactLoader: loader, Writer: writer}
		result, err := handler.Handle(context.Background(), intent)
		if err != nil {
			t.Fatalf("replay %d: Handle returned error %v, want nil (per-fact quarantine, never a whole-intent failure)", i, err)
		}
		results = append(results, result)
	}

	if results[0].SubSignals["input_invalid_facts"] != results[1].SubSignals["input_invalid_facts"] {
		t.Fatalf("input_invalid_facts differs across replays: %v vs %v; the quarantine decision must be deterministic",
			results[0].SubSignals["input_invalid_facts"], results[1].SubSignals["input_invalid_facts"])
	}
	if results[0].CanonicalWrites != results[1].CanonicalWrites {
		t.Fatalf("CanonicalWrites differs across replays: %d vs %d", results[0].CanonicalWrites, results[1].CanonicalWrites)
	}
	if results[0].Status != ResultStatusSucceeded || results[1].Status != ResultStatusSucceeded {
		t.Fatalf("both replays must succeed despite the quarantined fact: got %v and %v", results[0].Status, results[1].Status)
	}
}

// runContainerImageIdentityHandler runs ContainerImageIdentityHandler.Handle
// over exactly the two given facts and fails the test if Handle itself
// returns an error (per-fact isolation means a single malformed fact must
// never fail the whole intent).
func runContainerImageIdentityHandler(
	t *testing.T,
	envelopes ...facts.Envelope,
) (Result, *recordingContainerImageIdentityWriter) {
	t.Helper()

	loader := &stubContainerImageIdentityFactLoader{scopeFacts: envelopes}
	writer := &recordingContainerImageIdentityWriter{}
	handler := ContainerImageIdentityHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-container-image-quarantine",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-quarantine",
		SourceSystem: "test",
		Domain:       DomainContainerImageIdentity,
		Cause:        "container image references observed",
	})
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed fact must be quarantined per-fact, not fail the whole intent", err)
	}
	return result, writer
}

// assertOneInputInvalidFact asserts the handler recorded exactly one
// input_invalid quarantine on the intent's Result.SubSignals (each
// quarantined fact is also on the eshu_dp_reducer_input_invalid_facts_total
// counter and a structured error log — see recordQuarantinedFacts).
func assertOneInputInvalidFact(t *testing.T, result Result) {
	t.Helper()
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1", got)
	}
}

// assertWriterHasImageRefDecision asserts the writer received exactly one
// call carrying a decision for wantImageRef, proving the valid sibling fact
// still produced a decision despite the malformed fact sharing the batch —
// per-fact isolation, not whole-intent failure.
func assertWriterHasImageRefDecision(
	t *testing.T,
	writer *recordingContainerImageIdentityWriter,
	wantImageRef string,
) {
	t.Helper()
	if writer.calls != 1 {
		t.Fatalf("WriteContainerImageIdentityDecisions() calls = %d, want 1", writer.calls)
	}
	for _, decision := range writer.write.Decisions {
		if decision.ImageRef == wantImageRef {
			return
		}
		if decision.ImageRef == "" {
			t.Fatalf("a decision was produced under the empty-string image ref; the quarantined fact must never surface graph identity: %+v", decision)
		}
	}
	t.Fatalf("no decision produced for the valid sibling image ref %q; got %+v", wantImageRef, writer.write.Decisions)
}
