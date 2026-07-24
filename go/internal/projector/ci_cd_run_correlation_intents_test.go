// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestBuildProjectionQueuesSingleCICDRunCorrelationIntentForRunFact proves one
// ci_cd_run_correlation intent is enqueued per scope generation that observed
// a CI/CD run, triggered by the ci.run fact. Before this builder existed
// (#5710), CICDRunCorrelationHandler was registered and wired in
// cmd/reducer/main.go but never received an intent in production: no builder
// in scope_generation_intents.go emitted Domain=ci_cd_run_correlation, so
// list_ci_cd_run_correlations always returned zero outside unit tests.
func TestBuildProjectionQueuesSingleCICDRunCorrelationIntentForRunFact(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "ci_cd_run://github/team/checkout",
		ScopeKind:    "ci_cd_run",
		SourceSystem: "github_actions",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "ci-generation-1",
		ObservedAt:   time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.July, 24, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	envelopes := []facts.Envelope{
		cicdRunEnvelope("fact-ci-run-1", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	var count int
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainCICDRunCorrelation {
			count++
		}
	}
	if got, want := count, 1; got != want {
		t.Fatalf("ci_cd_run_correlation intents = %d, want %d", got, want)
	}
	intent := requireCICDRunCorrelationIntent(t, projection.reducerIntents)
	if got, want := intent.EntityKey, "ci_cd_run_correlation:ci_cd_run://github/team/checkout"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-ci-run-1"; got != want {
		t.Fatalf("intent.FactID = %q, want the ci.run fact", got)
	}
	if got, want := intent.SourceSystem, "github_actions"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

// TestBuildProjectionQueuesNoCICDRunCorrelationIntentForArtifactOnlyGeneration
// proves an artifact-only generation (no ci.run) does NOT enqueue an intent.
// CICDRunCorrelationHandler.Handle loads facts strictly scoped to the
// triggering intent's own (scope, generation) and
// buildCICDRunCorrelationDecisionsWithQuarantine only emits a decision for
// evidence anchored by a ci.run (ev.run.FactID == "" is skipped) — so an
// artifact-only generation's intent would load only the artifact, produce
// ZERO decisions, and succeed as a silent no-op: the artifact's evidence is
// simply discarded, not deferred, because nothing ever re-visits this
// generation once the intent succeeds. Triggering on the anchor (ci.run)
// only avoids enqueuing that wasted intent. Correlating a later-generation
// artifact against an earlier-generation run is a real gap the
// single-generation handler cannot close; tracked as a follow-up (#5710).
func TestBuildProjectionQueuesNoCICDRunCorrelationIntentForArtifactOnlyGeneration(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "ci_cd_run://github/team/checkout",
		ScopeKind:    "ci_cd_run",
		SourceSystem: "github_actions",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "ci-generation-2",
		ObservedAt:   time.Date(2026, time.July, 24, 11, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.July, 24, 11, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	envelopes := []facts.Envelope{
		cicdArtifactEnvelope("fact-ci-artifact-1", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainCICDRunCorrelation {
			t.Fatalf("unexpected ci_cd_run_correlation intent for an artifact-only generation (no ci.run anchor): %+v", intent)
		}
	}
}

// TestBuildProjectionQueuesCICDRunCorrelationIntentForRunAndArtifactGeneration
// proves the trigger still fires when ci.run and ci.artifact land in the SAME
// generation: the anchor (ci.run) trigger enqueues one intent, and
// CICDRunCorrelationHandler's own bulk load of that (scope, generation) picks
// up the co-located artifact — no per-artifact trigger is needed for the
// common same-generation case.
func TestBuildProjectionQueuesCICDRunCorrelationIntentForRunAndArtifactGeneration(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "ci_cd_run://github/team/checkout",
		ScopeKind:    "ci_cd_run",
		SourceSystem: "github_actions",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "ci-generation-3",
		ObservedAt:   time.Date(2026, time.July, 24, 13, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.July, 24, 13, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	envelopes := []facts.Envelope{
		cicdArtifactEnvelope("fact-ci-artifact-2", scopeValue.ScopeID, generation.GenerationID),
		cicdRunEnvelope("fact-ci-run-2", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := requireCICDRunCorrelationIntent(t, projection.reducerIntents)
	if got, want := intent.FactID, "fact-ci-run-2"; got != want {
		t.Fatalf("intent.FactID = %q, want the ci.run fact (the anchor), even though the artifact fact appears first in inputFacts", got)
	}
}

// TestBuildProjectionQueuesNoCICDRunCorrelationIntentWithoutCICDFacts proves a
// generation carrying no CI/CD evidence enqueues no correlation intent.
func TestBuildProjectionQueuesNoCICDRunCorrelationIntentWithoutCICDFacts(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "k8s://prod-us-east-1",
		ScopeKind:    scope.KindCluster,
		SourceSystem: "kubernetes_live",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "k8s-generation-3",
		ObservedAt:   time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.July, 24, 12, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	envelopes := []facts.Envelope{
		kubernetesWarningEnvelope("fact-warn-1", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainCICDRunCorrelation {
			t.Fatalf("unexpected ci_cd_run_correlation intent for a generation with no CI/CD facts: %+v", intent)
		}
	}
}

func requireCICDRunCorrelationIntent(t *testing.T, intents []ReducerIntent) ReducerIntent {
	t.Helper()
	for _, intent := range intents {
		if intent.Domain == reducer.DomainCICDRunCorrelation {
			return intent
		}
	}
	t.Fatalf("ci_cd_run_correlation intent missing from %#v", intents)
	return ReducerIntent{}
}

func cicdRunEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.CICDRunFactKind,
		SchemaVersion:    facts.CICDSchemaVersion,
		CollectorKind:    "github_actions",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "github_actions",
		},
		Payload: map[string]any{
			"provider":      "github_actions",
			"run_id":        "run-1",
			"run_attempt":   "1",
			"repository_id": "github.com/team/checkout",
			"commit_sha":    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"status":        "completed",
			"result":        "success",
		},
	}
}

func cicdArtifactEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.CICDArtifactFactKind,
		SchemaVersion:    facts.CICDSchemaVersion,
		CollectorKind:    "github_actions",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.July, 24, 11, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "github_actions",
		},
		Payload: map[string]any{
			"provider":        "github_actions",
			"run_id":          "run-2",
			"run_attempt":     "1",
			"artifact_type":   "container_image",
			"artifact_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}
}
