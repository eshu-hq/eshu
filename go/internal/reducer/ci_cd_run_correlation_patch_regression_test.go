// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCICDRunCorrelationArtifactPatchRetainsWorkflowImageEvidence(t *testing.T) {
	t.Parallel()

	const imageRef = "registry.example.com/team/api:latest"
	loader := &crossGenerationCICDRunFactLoader{
		currentFacts: []facts.Envelope{
			ciArtifactFact("artifact-later-generic", "run-workflow", ""),
		},
		historicalRunFacts: []facts.Envelope{
			ciRunFact("run-workflow", "github_actions", "repo-api", "abc123"),
			{
				FactID:   "workflow-image:abc123",
				FactKind: facts.CICDWorkflowImageEvidenceFactKind,
				Payload: map[string]any{
					"repository_id":  "repo-api",
					"commit_sha":     "abc123",
					"workflow_path":  ".github/workflows/release.yml",
					"command_kind":   "run",
					"evidence_class": "workflow_image_ref",
					"image_ref":      imageRef,
				},
			},
		},
		activeFacts: []facts.Envelope{
			containerImageIdentityFact(
				"image-workflow-support",
				"repo-api",
				imageRef,
				"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			),
		},
	}
	writer := &recordingCICDRunCorrelationWriter{}
	handler := CICDRunCorrelationHandler{FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-workflow-image-patch",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "generation-2",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	decision := cicdDecisionsByRun(writer.write.Decisions)["github_actions:run-workflow:1"]
	assertCICDDecision(t, decision, CICDRunCorrelationExact, 1)
	if decision.ImageRef != imageRef {
		t.Fatalf("ImageRef = %q, want retained workflow image %q", decision.ImageRef, imageRef)
	}
	if !stringSliceContains(decision.EvidenceFactIDs, "workflow-image:abc123") {
		t.Fatalf("EvidenceFactIDs = %#v, want retained workflow-image evidence", decision.EvidenceFactIDs)
	}
}

func TestCICDRunCorrelationArtifactTombstoneRetractsPriorArtifactDecision(t *testing.T) {
	t.Parallel()

	const digest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const stableKey = "opaque-artifact-stable-key"
	tombstone := facts.Envelope{
		FactID:        "artifact-tombstone",
		FactKind:      facts.CICDArtifactFactKind,
		StableFactKey: stableKey,
		IsTombstone:   true,
		Payload:       map[string]any{},
	}
	priorArtifact := ciArtifactFact("artifact-prior", "run-retired-artifact", digest)
	priorArtifact.StableFactKey = stableKey
	loader := &crossGenerationCICDRunFactLoader{
		currentFacts: []facts.Envelope{tombstone},
		historicalRunFacts: []facts.Envelope{
			ciRunFact("run-retired-artifact", "github_actions", "repo-api", "abc123"),
			priorArtifact,
		},
		activeFacts: []facts.Envelope{
			containerImageIdentityFact(
				"image-prior",
				"repo-api",
				"registry.example.com/team/api@"+digest,
				digest,
			),
		},
	}
	writer := &recordingCICDRunCorrelationWriter{}
	handler := CICDRunCorrelationHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-artifact-tombstone",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "generation-2",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	decision := cicdDecisionsByRun(writer.write.Decisions)["github_actions:run-retired-artifact:1"]
	assertCICDDecision(t, decision, CICDRunCorrelationDerived, 0)
	if decision.ArtifactDigest != "" || decision.ImageRef != "" {
		t.Fatalf("retired artifact decision = %#v, want no artifact identity", decision)
	}
	if stringSliceContains(decision.EvidenceFactIDs, "artifact-prior") ||
		stringSliceContains(decision.EvidenceFactIDs, "image-prior") {
		t.Fatalf("EvidenceFactIDs = %#v, must not resurrect retired artifact evidence", decision.EvidenceFactIDs)
	}
	if len(result.SubSignals) != 0 {
		t.Fatalf("SubSignals = %#v, want tombstone treated as control evidence", result.SubSignals)
	}
}

func TestCICDRunCorrelationArtifactTombstoneFailsClosedWithoutRetainedIdentity(t *testing.T) {
	t.Parallel()

	loader := &crossGenerationCICDRunFactLoader{
		currentFacts: []facts.Envelope{{
			FactID:        "artifact-tombstone",
			FactKind:      facts.CICDArtifactFactKind,
			StableFactKey: "missing-artifact-key",
			IsTombstone:   true,
			Payload:       map[string]any{},
		}},
	}
	handler := CICDRunCorrelationHandler{
		FactLoader: loader,
		Writer:     &recordingCICDRunCorrelationWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-unresolved-artifact-tombstone",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "generation-2",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
	})
	if err == nil || !strings.Contains(err.Error(), "has no retained payload identity") {
		t.Fatalf("Handle() error = %v, want fail-closed retained-identity error", err)
	}
}

func TestCICDRunCorrelationArtifactTombstoneRejectsBlankStableKey(t *testing.T) {
	t.Parallel()

	loader := &crossGenerationCICDRunFactLoader{
		currentFacts: []facts.Envelope{{
			FactID:      "artifact-tombstone",
			FactKind:    facts.CICDArtifactFactKind,
			IsTombstone: true,
			Payload:     map[string]any{},
		}},
	}
	handler := CICDRunCorrelationHandler{
		FactLoader: loader,
		Writer:     &recordingCICDRunCorrelationWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-blank-key-artifact-tombstone",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "generation-2",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
	})
	if err == nil || !strings.Contains(err.Error(), "has no stable fact key") {
		t.Fatalf("Handle() error = %v, want fail-closed stable-key error", err)
	}
}

func TestCICDRunCorrelationPayloadEmptyLiveArtifactRetractsRetainedIdentityAndQuarantines(t *testing.T) {
	t.Parallel()

	const digest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	const stableKey = "malformed-live-key"
	priorArtifact := ciArtifactFact("artifact-prior", "run-malformed-artifact", digest)
	priorArtifact.StableFactKey = stableKey
	loader := &crossGenerationCICDRunFactLoader{
		currentFacts: []facts.Envelope{{
			FactID:        "artifact-malformed-live",
			FactKind:      facts.CICDArtifactFactKind,
			StableFactKey: stableKey,
			Payload:       map[string]any{},
		}},
		historicalRunFacts: []facts.Envelope{
			ciRunFact("run-malformed-artifact", "github_actions", "repo-api", "abc123"),
			priorArtifact,
		},
		activeFacts: []facts.Envelope{
			containerImageIdentityFact(
				"image-prior",
				"repo-api",
				"registry.example.com/team/api@"+digest,
				digest,
			),
		},
	}
	writer := &recordingCICDRunCorrelationWriter{}
	handler := CICDRunCorrelationHandler{
		FactLoader: loader,
		Writer:     writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-malformed-live-artifact",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "generation-2",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want per-fact quarantine", err)
	}
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("input_invalid_facts = %v, want 1 malformed live artifact", got)
	}
	decision := cicdDecisionsByRun(writer.write.Decisions)["github_actions:run-malformed-artifact:1"]
	assertCICDDecision(t, decision, CICDRunCorrelationDerived, 0)
	if decision.ArtifactDigest != "" || decision.ImageRef != "" {
		t.Fatalf("malformed current artifact decision = %#v, want no retained artifact identity", decision)
	}
	if stringSliceContains(decision.EvidenceFactIDs, "artifact-prior") ||
		stringSliceContains(decision.EvidenceFactIDs, "image-prior") {
		t.Fatalf("EvidenceFactIDs = %#v, must not resurrect superseded artifact evidence", decision.EvidenceFactIDs)
	}
}
