// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type crossGenerationCICDRunFactLoader struct {
	currentFacts       []facts.Envelope
	historicalRunFacts []facts.Envelope
	previousDecisions  []facts.Envelope
	activeFacts        []facts.Envelope
	historicalCalls    int
	previousCalls      int
}

func (l *crossGenerationCICDRunFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.currentFacts...), nil
}

func (l *crossGenerationCICDRunFactLoader) ListFactsByKind(
	context.Context,
	string,
	string,
	[]string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.currentFacts...), nil
}

func (l *crossGenerationCICDRunFactLoader) ListCICDRunFactsForRunKeys(
	_ context.Context,
	_, _ string,
	_, _, _ []string,
) ([]facts.Envelope, error) {
	l.historicalCalls++
	return append([]facts.Envelope(nil), l.historicalRunFacts...), nil
}

func (l *crossGenerationCICDRunFactLoader) ListPreviousCICDRunCorrelationFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	l.previousCalls++
	return append([]facts.Envelope(nil), l.previousDecisions...), nil
}

func (l *crossGenerationCICDRunFactLoader) ListActiveCICDRunCorrelationFacts(
	context.Context,
	[]string,
	[]string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.activeFacts...), nil
}

func TestCICDRunCorrelationHandlerPatchesLaterArtifactAndCarriesPreviousSnapshot(t *testing.T) {
	t.Parallel()

	loader := &crossGenerationCICDRunFactLoader{
		currentFacts: []facts.Envelope{
			ciArtifactFact("artifact-later", "run-upgraded", testCICDDigest),
		},
		historicalRunFacts: []facts.Envelope{
			ciRunFact("run-upgraded", "github_actions", "repo-api", "abc123"),
			{
				FactID:   "deployment-event:abc123",
				FactKind: facts.CICDDeploymentEventFactKind,
				Payload: map[string]any{
					"provider":      "github_actions",
					"deployment_id": "deployment-1",
					"environment":   "production",
					"sha":           "abc123",
					"state":         "success",
					"repository_id": "repo-api",
				},
			},
		},
		previousDecisions: []facts.Envelope{
			priorCICDRunCorrelationFact("prior-upgraded", CICDRunCorrelationDecision{
				Provider:            "github_actions",
				RunID:               "run-upgraded",
				RunAttempt:          "1",
				RepositoryID:        "repo-api",
				CommitSHA:           "abc123",
				Environment:         "prod",
				EnvironmentEvidence: supplyChainEnvironmentEvidenceDeployEvent,
				ArtifactDigest:      "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				ImageRef:            "registry.example.com/team/old@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				Outcome:             CICDRunCorrelationExact,
				Reason:              "prior artifact matched old image identity",
				CanonicalWrites:     1,
				EvidenceFactIDs: []string{
					"ci.run:run-upgraded",
					"deployment-event:abc123",
					"artifact-old",
					"image-old",
				},
			}),
			priorCICDRunCorrelationFact("prior-unaffected", CICDRunCorrelationDecision{
				Provider:        "github_actions",
				RunID:           "run-unaffected",
				RunAttempt:      "1",
				RepositoryID:    "repo-api",
				CommitSHA:       "def456",
				Outcome:         CICDRunCorrelationDerived,
				Reason:          "run has provider evidence but no explicit artifact identity anchor",
				ProvenanceOnly:  true,
				EvidenceFactIDs: []string{"ci.run:run-unaffected"},
			}),
		},
		activeFacts: []facts.Envelope{
			containerImageIdentityFact(
				"image-upgraded-support-1",
				"repo-api",
				"registry.example.com/team/api@"+testCICDDigest,
				testCICDDigest,
			),
			containerImageIdentityFact(
				"image-upgraded-support-2",
				"repo-deployer",
				"registry.example.com/team/api@"+testCICDDigest,
				testCICDDigest,
			),
		},
	}
	writer := &recordingCICDRunCorrelationWriter{}
	handler := CICDRunCorrelationHandler{FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-artifact-later",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "generation-2",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
		Cause:        "ci/cd run-scoped evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	got := cicdDecisionsByRun(writer.write.Decisions)
	if len(got) != 2 {
		t.Fatalf("written decisions = %d, want patched run plus carried prior snapshot: %#v", len(got), got)
	}
	upgraded := got["github_actions:run-upgraded:1"]
	assertCICDDecision(t, upgraded, CICDRunCorrelationExact, 1)
	for _, factID := range []string{
		"ci.run:run-upgraded",
		"artifact-later",
		"image-upgraded-support-1",
		"image-upgraded-support-2",
	} {
		if !stringSliceContains(upgraded.EvidenceFactIDs, factID) {
			t.Fatalf("upgraded EvidenceFactIDs = %#v, want %q", upgraded.EvidenceFactIDs, factID)
		}
	}
	if upgraded.Environment != "prod" || upgraded.EnvironmentEvidence != supplyChainEnvironmentEvidenceDeployEvent {
		t.Fatalf(
			"upgraded environment = %q (%q), want carried deploy-event environment",
			upgraded.Environment,
			upgraded.EnvironmentEvidence,
		)
	}
	if !stringSliceContains(upgraded.EvidenceFactIDs, "deployment-event:abc123") {
		t.Fatalf("upgraded EvidenceFactIDs = %#v, want carried deployment event", upgraded.EvidenceFactIDs)
	}
	for _, staleFactID := range []string{"artifact-old", "image-old"} {
		if stringSliceContains(upgraded.EvidenceFactIDs, staleFactID) {
			t.Fatalf("upgraded EvidenceFactIDs = %#v, must exclude stale %q", upgraded.EvidenceFactIDs, staleFactID)
		}
	}
	carried := got["github_actions:run-unaffected:1"]
	assertCICDDecision(t, carried, CICDRunCorrelationDerived, 0)
	if loader.historicalCalls != 1 || loader.previousCalls != 1 {
		t.Fatalf(
			"cross-generation calls = historical:%d previous:%d, want 1 each",
			loader.historicalCalls,
			loader.previousCalls,
		)
	}
}

func TestCICDRunCorrelationHandlerRejectsArtifactPatchWithoutHistoryLoader(t *testing.T) {
	t.Parallel()

	handler := CICDRunCorrelationHandler{
		FactLoader: &stubCICDRunCorrelationFactLoader{scopeFacts: []facts.Envelope{
			ciArtifactFact("artifact-later", "run-upgraded", testCICDDigest),
		}},
		Writer: &recordingCICDRunCorrelationWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-artifact-later",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "generation-2",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
	})
	if err == nil || !strings.Contains(err.Error(), "does not support cross-generation evidence") {
		t.Fatalf("Handle() error = %v, want missing history-loader rejection", err)
	}
}

func TestMergeCICDRunCorrelationPatchDecisionsRejectsFractionalCanonicalWrites(t *testing.T) {
	t.Parallel()

	prior := priorCICDRunCorrelationFact("prior-corrupt", CICDRunCorrelationDecision{
		Provider:        "github_actions",
		RunID:           "run-1",
		RunAttempt:      "1",
		Outcome:         CICDRunCorrelationDerived,
		CanonicalWrites: 1,
	})
	prior.Payload["canonical_writes"] = 1.5

	_, err := mergeCICDRunCorrelationPatchDecisions([]facts.Envelope{prior}, nil)
	if err == nil || !strings.Contains(err.Error(), "canonical_writes") {
		t.Fatalf("mergeCICDRunCorrelationPatchDecisions() error = %v, want invalid canonical_writes rejection", err)
	}
}

func priorCICDRunCorrelationFact(factID string, decision CICDRunCorrelationDecision) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: cicdRunCorrelationFactKind,
		Payload:  cicdRunCorrelationPayload(CICDRunCorrelationWrite{}, decision),
	}
}
