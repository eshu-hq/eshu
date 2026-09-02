// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

type crossGenerationCICDRunFactLoader struct {
	currentFacts       []facts.Envelope
	historicalRunFacts []facts.Envelope
	activeFacts        []facts.Envelope
	historicalCalls    int
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

func (l *crossGenerationCICDRunFactLoader) ListCICDRunFactsForScopePatch(
	_ context.Context,
	_, _ string,
	_, _, _ []string,
) ([]facts.Envelope, error) {
	l.historicalCalls++
	return append([]facts.Envelope(nil), l.historicalRunFacts...), nil
}

func (l *crossGenerationCICDRunFactLoader) ListActiveCICDRunCorrelationFacts(
	context.Context,
	[]string,
	[]string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.activeFacts...), nil
}

func TestCICDRunCorrelationHandlerRebuildsCompleteSourceSnapshotForLaterArtifact(t *testing.T) {
	t.Parallel()

	const historicalDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	loader := &crossGenerationCICDRunFactLoader{
		currentFacts: []facts.Envelope{
			ciArtifactFact("artifact-later", "run-upgraded", testCICDDigest),
		},
		historicalRunFacts: []facts.Envelope{
			ciRunFact("run-upgraded", "github_actions", "repo-api", "abc123"),
			ciRunFact("run-unaffected", "github_actions", "repo-api", "def456"),
			ciArtifactFact("artifact-historical", "run-upgraded", historicalDigest),
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
		activeFacts: []facts.Envelope{
			containerImageIdentityFact(
				"image-historical-support",
				"repo-api",
				"registry.example.com/team/old@"+historicalDigest,
				historicalDigest,
			),
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

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-artifact-later",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "generation-2",
		SourceSystem: "ci_cd_run",
		Domain:       reducercontract.DomainCICDRunCorrelation,
		Cause:        "ci/cd run-scoped evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if !strings.Contains(result.EvidenceSummary, "evaluated=2 preserved=0") {
		t.Fatalf(
			"EvidenceSummary = %q, want the complete source snapshot rebuilt",
			result.EvidenceSummary,
		)
	}

	got := cicdDecisionsByRun(writer.write.Decisions)
	if len(got) != 2 {
		t.Fatalf("written decisions = %d, want the complete rebuilt source snapshot: %#v", len(got), got)
	}
	upgraded := got["github_actions:run-upgraded:1"]
	assertCICDDecision(t, upgraded, CICDRunCorrelationExact, 1)
	if upgraded.ArtifactDigest != testCICDDigest {
		t.Fatalf("upgraded ArtifactDigest = %q, want current-generation digest %q", upgraded.ArtifactDigest, testCICDDigest)
	}
	if upgraded.ImageRef != "registry.example.com/team/api@"+testCICDDigest {
		t.Fatalf("upgraded ImageRef = %q, want current-generation image", upgraded.ImageRef)
	}
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
	if upgraded.Environment != "prod" || upgraded.EnvironmentEvidence != reducercontract.CICDRunCorrelationEnvironmentEvidenceDeployEvent {
		t.Fatalf(
			"upgraded environment = %q (%q), want carried deploy-event environment",
			upgraded.Environment,
			upgraded.EnvironmentEvidence,
		)
	}
	if !stringSliceContains(upgraded.EvidenceFactIDs, "deployment-event:abc123") {
		t.Fatalf("upgraded EvidenceFactIDs = %#v, want carried deployment event", upgraded.EvidenceFactIDs)
	}
	for _, staleFactID := range []string{
		"artifact-old",
		"image-old",
		"artifact-historical",
		"image-historical-support",
	} {
		if stringSliceContains(upgraded.EvidenceFactIDs, staleFactID) {
			t.Fatalf("upgraded EvidenceFactIDs = %#v, must exclude stale %q", upgraded.EvidenceFactIDs, staleFactID)
		}
	}
	unaffected := got["github_actions:run-unaffected:1"]
	assertCICDDecision(t, unaffected, CICDRunCorrelationDerived, 0)
	if loader.historicalCalls != 1 {
		t.Fatalf("cross-generation calls = historical:%d, want 1", loader.historicalCalls)
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

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-artifact-later",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "generation-2",
		SourceSystem: "ci_cd_run",
		Domain:       reducercontract.DomainCICDRunCorrelation,
	})
	if err == nil || !strings.Contains(err.Error(), "does not support cross-generation evidence") {
		t.Fatalf("Handle() error = %v, want missing history-loader rejection", err)
	}
}

func TestCICDRunCorrelationHandlerRejectsOversizedPatchSnapshot(t *testing.T) {
	t.Parallel()

	historical := make([]facts.Envelope, 0, maxCICDRunCorrelationPatchDecisions+1)
	for index := 0; index <= maxCICDRunCorrelationPatchDecisions; index++ {
		historical = append(historical, ciRunFact(
			fmt.Sprintf("run-%d", index),
			"github_actions",
			"repo-api",
			fmt.Sprintf("commit-%d", index),
		))
	}
	loader := &crossGenerationCICDRunFactLoader{
		currentFacts: []facts.Envelope{
			ciArtifactFact("artifact-later", "run-0", testCICDDigest),
		},
		historicalRunFacts: historical,
	}
	handler := CICDRunCorrelationHandler{
		FactLoader: loader,
		Writer:     &recordingCICDRunCorrelationWriter{},
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-oversized-patch",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "generation-2",
		SourceSystem: "ci_cd_run",
		Domain:       reducercontract.DomainCICDRunCorrelation,
	})
	if err == nil || !strings.Contains(err.Error(), "decisions exceed safety cap 1000") {
		t.Fatalf("Handle() error = %v, want patch decision safety-cap rejection", err)
	}
}

func TestExcludeSupersededCICDFactsUsesCurrentArtifactKeyWithoutDigest(t *testing.T) {
	t.Parallel()

	historical := []facts.Envelope{
		ciRunFact("run-upgraded", "github_actions", "repo-api", "abc123"),
		ciArtifactFact("artifact-historical", "run-upgraded", testCICDDigest),
		ciArtifactFact("artifact-other-run", "run-other", testCICDDigest),
	}
	current := []facts.Envelope{
		ciArtifactFact("artifact-current-without-digest", "run-upgraded", ""),
	}

	directives, err := cicdArtifactPatchDirectivesFromCurrent(current)
	if err != nil {
		t.Fatalf("build patch directives: %v", err)
	}
	filtered, err := excludeSupersededCICDFacts(
		historical,
		current,
		directives.liveRunKeys,
		directives.liveStableKeys,
	)
	if err != nil {
		t.Fatalf("exclude superseded facts: %v", err)
	}
	got := make(map[string]struct{}, len(filtered))
	for _, envelope := range filtered {
		got[envelope.FactID] = struct{}{}
	}
	if _, exists := got["artifact-historical"]; exists {
		t.Fatalf("filtered facts = %#v, must not resurrect a retained digest when the current artifact omits it", got)
	}
	for _, factID := range []string{"ci.run:run-upgraded", "artifact-other-run"} {
		if _, exists := got[factID]; !exists {
			t.Fatalf("filtered facts = %#v, want unaffected %q", got, factID)
		}
	}
}

func TestExcludeSupersededCICDFactsHonorsTypedCurrentTombstones(t *testing.T) {
	t.Parallel()

	factKinds := []string{
		facts.CICDArtifactFactKind,
		facts.CICDEnvironmentObservationFactKind,
		facts.CICDDeploymentEventFactKind,
		facts.CICDTriggerEdgeFactKind,
		facts.CICDStepFactKind,
		facts.CICDWorkflowImageEvidenceFactKind,
	}
	historical := make([]facts.Envelope, 0, len(factKinds)+3)
	current := make([]facts.Envelope, 0, len(factKinds))
	for _, factKind := range factKinds {
		stableKey := factKind + "-retired-key"
		historical = append(historical, facts.Envelope{
			FactID:        factKind + "-retained",
			FactKind:      factKind,
			StableFactKey: stableKey,
		})
		current = append(current, facts.Envelope{
			FactID:        factKind + "-tombstone",
			FactKind:      factKind,
			StableFactKey: stableKey,
			IsTombstone:   true,
		})
	}
	historical = append(historical, facts.Envelope{
		FactID:        "run-with-colliding-stable-key",
		FactKind:      facts.CICDRunFactKind,
		StableFactKey: facts.CICDWorkflowImageEvidenceFactKind + "-retired-key",
	})
	historical = append(historical, facts.Envelope{
		FactID:        "environment-different-key",
		FactKind:      facts.CICDEnvironmentObservationFactKind,
		StableFactKey: "environment-still-live-key",
	})
	historical = append(historical, facts.Envelope{
		FactID:        "workflow-image-whitespace-distinct-key",
		FactKind:      facts.CICDWorkflowImageEvidenceFactKind,
		StableFactKey: " " + facts.CICDWorkflowImageEvidenceFactKind + "-retired-key",
	})

	directives, err := cicdArtifactPatchDirectivesFromCurrent(current)
	if err != nil {
		t.Fatalf("build patch directives: %v", err)
	}
	filtered, err := excludeSupersededCICDFacts(
		historical,
		current,
		nil,
		mergeCICDArtifactStableKeys(directives.liveStableKeys, directives.tombstoneStableKeys),
	)
	if err != nil {
		t.Fatalf("exclude superseded facts: %v", err)
	}
	got := make(map[string]struct{}, len(filtered))
	for _, envelope := range filtered {
		got[envelope.FactID] = struct{}{}
	}
	for _, factID := range []string{
		"run-with-colliding-stable-key",
		"environment-different-key",
		"workflow-image-whitespace-distinct-key",
	} {
		if _, exists := got[factID]; !exists {
			t.Fatalf("filtered facts = %#v, want unaffected %q", filtered, factID)
		}
	}
	if len(filtered) != 3 {
		t.Fatalf("filtered facts = %#v, want only different-kind and unrelated-key evidence", filtered)
	}
}

func TestExcludeSupersededCICDFactsHonorsTypedCurrentLiveFacts(t *testing.T) {
	t.Parallel()

	const stableKey = "shared-live-key"
	historical := []facts.Envelope{
		{
			FactID:        "environment-retained",
			FactKind:      facts.CICDEnvironmentObservationFactKind,
			StableFactKey: stableKey,
		},
		{
			FactID:        "workflow-image-same-key",
			FactKind:      facts.CICDWorkflowImageEvidenceFactKind,
			StableFactKey: stableKey,
		},
		{
			FactID:        "environment-whitespace-distinct-key",
			FactKind:      facts.CICDEnvironmentObservationFactKind,
			StableFactKey: " " + stableKey,
		},
	}
	current := []facts.Envelope{{
		FactID:        "environment-current",
		FactKind:      facts.CICDEnvironmentObservationFactKind,
		StableFactKey: stableKey,
	}}

	filtered, err := excludeSupersededCICDFacts(historical, current, nil, nil)
	if err != nil {
		t.Fatalf("exclude superseded facts: %v", err)
	}
	got := make(map[string]struct{}, len(filtered))
	for _, envelope := range filtered {
		got[envelope.FactID] = struct{}{}
	}
	if _, exists := got["environment-retained"]; exists {
		t.Fatalf("filtered facts = %#v, must remove the exact typed retained identity", filtered)
	}
	for _, factID := range []string{
		"workflow-image-same-key",
		"environment-whitespace-distinct-key",
	} {
		if _, exists := got[factID]; !exists {
			t.Fatalf("filtered facts = %#v, want unaffected %q", filtered, factID)
		}
	}
}

func TestExcludeSupersededCICDFactsRejectsUnidentifiableTombstone(t *testing.T) {
	t.Parallel()

	_, err := excludeSupersededCICDFacts(nil, []facts.Envelope{{
		FactID:      "workflow-image-tombstone",
		FactKind:    facts.CICDWorkflowImageEvidenceFactKind,
		IsTombstone: true,
	}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "has no stable fact key") {
		t.Fatalf("exclude superseded facts error = %v, want fail-closed stable-key error", err)
	}
}
