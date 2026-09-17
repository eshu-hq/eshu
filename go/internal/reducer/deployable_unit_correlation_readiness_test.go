// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// stubResolutionActiveLookup reports generation activation from a fixed map.
// A generation missing from the map reports inactive, matching the
// fail-safe contract of the production PK lookup.
func stubResolutionActiveLookup(active map[string]bool) func(string) (bool, error) {
	return func(generationID string) (bool, error) {
		return active[strings.TrimSpace(generationID)], nil
	}
}

// dockerfileCandidateEnvelopesEquations builds the Dockerfile-candidate fixture
// used across the readiness tests: one candidate-bearing scope with no foreign
// repos linked, so the gate exercises the own-generation check.
func dockerfileCandidateEnvelopes() []facts.Envelope {
	return deployableUnitCorrelationEnvelopes(
		"repo-edge-api",
		"edge-api",
		[]map[string]any{
			{
				"repo_id":       "repo-edge-api",
				"language":      "dockerfile",
				"relative_path": "Dockerfile",
				"parsed_file_data": map[string]any{
					"dockerfile_stages": []any{
						map[string]any{"name": "runtime"},
					},
				},
			},
		},
	)
}

func TestDeployableUnitCorrelationHandleDefersWhenOwnResolutionInactive(t *testing.T) {
	t.Parallel()

	resolvedLoader := &stubDeployableUnitResolvedLoader{}
	handler := DeployableUnitCorrelationHandler{
		FactLoader:     &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader: resolvedLoader,
		// Own generation intentionally absent: resolution has not activated it.
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{}),
	}

	_, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err == nil {
		t.Fatal("Handle() error = nil, want resolution-not-ready deferral")
	}
	var deferral deployableUnitCorrelationResolutionNotReadyError
	if !errors.As(err, &deferral) {
		t.Fatalf("Handle() error type = %T, want deployableUnitCorrelationResolutionNotReadyError", err)
	}
	if !deferral.Retryable() {
		t.Fatal("deferral Retryable() = false, want true so the queue re-runs after activation")
	}
	if got, want := deferral.FailureClass(), DeployableUnitCorrelationResolutionNotReadyFailureClass; got != want {
		t.Fatalf("deferral FailureClass() = %q, want %q", got, want)
	}
	if resolvedLoader.calls != 0 {
		t.Fatalf("resolved loader calls = %d, want 0: the gate must fire before the fail-open read", resolvedLoader.calls)
	}
}

func TestDeployableUnitCorrelationHandleProceedsWhenOwnResolutionActive(t *testing.T) {
	t.Parallel()

	handler := DeployableUnitCorrelationHandler{
		FactLoader:             &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader:         &stubDeployableUnitResolvedLoader{},
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"generation-1": true}),
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil when own generation is active", err)
	}
	if got.Status != ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want %q", got.Status, ResultStatusSucceeded)
	}
}

func TestDeployableUnitCorrelationHandleGateOpenWithoutLookup(t *testing.T) {
	t.Parallel()

	// No ResolutionActiveLookup wired: the gate stays open so existing unit
	// wiring and the direct-call seam keep their behavior.
	handler := DeployableUnitCorrelationHandler{
		FactLoader:     &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader: &stubDeployableUnitResolvedLoader{},
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil with the gate unwired", err)
	}
	if got.Status != ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want %q", got.Status, ResultStatusSucceeded)
	}
}

func TestDeployableUnitCorrelationHandleDefersWhileCanonicalReposRebuild(t *testing.T) {
	t.Parallel()

	resolvedLoader := &stubDeployableUnitResolvedLoader{
		resolved: []relationships.ResolvedRelationship{
			{
				SourceRepoID:     "repo-deployments",
				TargetRepoID:     "repo-edge-api",
				RelationshipType: relationships.RelDeploysFrom,
				Confidence:       0.94,
			},
		},
	}
	writer := &recordingDeployableUnitEdgeWriter{}
	publisher := &recordingGraphProjectionPhasePublisher{}
	handler := DeployableUnitCorrelationHandler{
		FactLoader:             &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader:         resolvedLoader,
		PhasePublisher:         publisher,
		EdgeWriter:             writer,
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"generation-1": true}),
		CanonicalQuiescence:    staticReducerGraphDrain{uncommittedCanonical: true},
	}

	_, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err == nil {
		t.Fatal("Handle() error = nil, want canonical-nodes-not-ready deferral")
	}
	var deferral deployableUnitCorrelationCanonicalNodesNotReadyError
	if !errors.As(err, &deferral) {
		t.Fatalf("Handle() error type = %T, want deployableUnitCorrelationCanonicalNodesNotReadyError", err)
	}
	if !deferral.Retryable() {
		t.Fatal("deferral Retryable() = false, want true so the queue re-runs after projector convergence")
	}
	if got, want := deferral.FailureClass(), DeployableUnitCorrelationCanonicalNodesNotReadyFailureClass; got != want {
		t.Fatalf("deferral FailureClass() = %q, want %q", got, want)
	}
	if deferral.scopeID != "repository:test-scope" || deferral.generationID != "generation-1" {
		t.Fatalf("deferral scope/generation = %q/%q, want repository:test-scope/generation-1",
			deferral.scopeID, deferral.generationID)
	}
	if resolvedLoader.calls != 0 {
		t.Fatalf("resolved loader calls = %d, want 0 before canonical repository quiescence", resolvedLoader.calls)
	}
	if len(writer.retractCalls) != 0 || len(writer.writeCalls) != 0 {
		t.Fatalf("edge calls while projector can detach Repository nodes = retract:%d write:%d, want 0/0",
			len(writer.retractCalls), len(writer.writeCalls))
	}
	if len(publisher.calls) != 0 {
		t.Fatalf("phase publisher calls = %d, want 0 before canonical repository quiescence", len(publisher.calls))
	}
}

func TestDeployableUnitCorrelationHandleWritesAfterCanonicalReposQuiesce(t *testing.T) {
	t.Parallel()

	writer := &recordingDeployableUnitEdgeWriter{}
	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader: &stubDeployableUnitResolvedLoader{
			resolved: []relationships.ResolvedRelationship{
				{
					SourceRepoID:     "repo-deployments",
					TargetRepoID:     "repo-edge-api",
					RelationshipType: relationships.RelDeploysFrom,
					Confidence:       0.94,
					Details: map[string]any{
						"evidence_kinds": []string{string(relationships.EvidenceKindArgoCDAppSource)},
					},
				},
			},
		},
		PhasePublisher:         &recordingGraphProjectionPhasePublisher{},
		EdgeWriter:             writer,
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"generation-1": true}),
		CanonicalQuiescence:    staticReducerGraphDrain{},
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil after canonical repository quiescence", err)
	}
	if got.CanonicalWrites != 1 || len(writer.retractCalls) != 1 || len(writer.writeCalls) != 1 {
		t.Fatalf("post-quiescence writes = canonical:%d retract:%d write:%d, want 1/1/1",
			got.CanonicalWrites, len(writer.retractCalls), len(writer.writeCalls))
	}
}

func TestDeployableUnitCorrelationHandleFailsClosedOnCanonicalQuiescenceError(t *testing.T) {
	t.Parallel()

	writer := &recordingDeployableUnitEdgeWriter{}
	handler := DeployableUnitCorrelationHandler{
		FactLoader:             &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader:         &stubDeployableUnitResolvedLoader{},
		EdgeWriter:             writer,
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"generation-1": true}),
		CanonicalQuiescence:    staticReducerGraphDrain{err: errors.New("quiescence unavailable")},
	}

	_, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err == nil || !strings.Contains(err.Error(), "check canonical repository quiescence") {
		t.Fatalf("Handle() error = %v, want canonical repository quiescence failure", err)
	}
	if len(writer.retractCalls) != 0 || len(writer.writeCalls) != 0 {
		t.Fatalf("edge calls after quiescence error = retract:%d write:%d, want 0/0",
			len(writer.retractCalls), len(writer.writeCalls))
	}
}

// dockerfileLoaderIntent builds a workload-materialization intent over the
// same Dockerfile-candidate fixture the CDU gate tests use: candidates exist
// pre-admission, so the loader gate exercises the own-generation check.
func dockerfileLoaderIntent() Intent {
	now := time.Now().UTC()
	return Intent{
		IntentID:        "intent-loader-1",
		ScopeID:         "scope-service",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo-service"},
		RelatedScopeIDs: []string{"scope-service"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}
}

func dockerfileLoaderEnvelopes() []facts.Envelope {
	now := time.Now().UTC()
	return []facts.Envelope{
		{
			FactID:   "fact-repo-1",
			ScopeID:  "scope-service",
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id": "repo-service",
				"name":     "service-repo",
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-file-1",
			ScopeID:  "scope-service",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-service",
				"language":      "dockerfile",
				"relative_path": "Dockerfile",
				"parsed_file_data": map[string]any{
					"dockerfile_stages": []any{map[string]any{"name": "runtime"}},
				},
			},
			ObservedAt: now,
		},
	}
}

func TestWorkloadProjectionInputsDeferWhenOwnResolutionInactive(t *testing.T) {
	t.Parallel()

	resolvedLoader := &stubResolvedRelationshipLoader{}
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:     &stubFactLoader{envelopes: dockerfileLoaderEnvelopes()},
		ResolvedLoader: resolvedLoader,
		// Own generation intentionally absent: resolution has not activated it.
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{}),
	}

	_, _, err := loader.LoadWorkloadProjectionInputs(context.Background(), dockerfileLoaderIntent())
	if err == nil {
		t.Fatal("LoadWorkloadProjectionInputs() error = nil, want resolution-not-ready deferral")
	}
	var deferral workloadMaterializationResolutionNotReadyError
	if !errors.As(err, &deferral) {
		t.Fatalf("LoadWorkloadProjectionInputs() error type = %T, want workloadMaterializationResolutionNotReadyError", err)
	}
	if !deferral.Retryable() {
		t.Fatal("deferral Retryable() = false, want true so the queue re-runs after activation")
	}
	if got, want := deferral.FailureClass(), WorkloadMaterializationResolutionNotReadyFailureClass; got != want {
		t.Fatalf("deferral FailureClass() = %q, want %q", got, want)
	}
	if resolvedLoader.calls != 0 || resolvedLoader.repoCalls != 0 {
		t.Fatalf("resolved loader calls = %d/%d, want 0/0: the gate must fire before the fail-open read",
			resolvedLoader.calls, resolvedLoader.repoCalls)
	}
}

func TestWorkloadProjectionInputsProceedWhenOwnResolutionActive(t *testing.T) {
	t.Parallel()

	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:             &stubFactLoader{envelopes: dockerfileLoaderEnvelopes()},
		ResolvedLoader:         &stubResolvedRelationshipLoader{},
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"gen-1": true}),
	}

	_, _, err := loader.LoadWorkloadProjectionInputs(context.Background(), dockerfileLoaderIntent())
	if err != nil {
		t.Fatalf("LoadWorkloadProjectionInputs() error = %v, want nil when own generation is active", err)
	}
}

func TestDeployableUnitCorrelationHandleDefersWhileCorpusResolutionIncomplete(t *testing.T) {
	t.Parallel()

	resolvedLoader := &stubDeployableUnitResolvedLoader{}
	handler := DeployableUnitCorrelationHandler{
		FactLoader:     &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader: resolvedLoader,
		// Own generation is active, but a foreign scope's resolution has not
		// activated yet: the by-repos read would serve a partial set (#6184).
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"generation-1": true}),
		ResolutionsCompleteLookup: func() (bool, error) {
			return false, nil
		},
	}

	_, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err == nil {
		t.Fatal("Handle() error = nil, want resolution-not-ready deferral while a foreign generation is inactive")
	}
	var deferral deployableUnitCorrelationResolutionNotReadyError
	if !errors.As(err, &deferral) {
		t.Fatalf("Handle() error type = %T, want deployableUnitCorrelationResolutionNotReadyError", err)
	}
	if !deferral.Retryable() {
		t.Fatal("deferral Retryable() = false, want true so the queue re-runs after activation")
	}
	if got, want := deferral.FailureClass(), DeployableUnitCorrelationResolutionNotReadyFailureClass; got != want {
		t.Fatalf("deferral FailureClass() = %q, want %q", got, want)
	}
	if resolvedLoader.calls != 0 {
		t.Fatalf("resolved loader calls = %d, want 0: the gate must fire before the fail-open read", resolvedLoader.calls)
	}
}

func TestDeployableUnitCorrelationHandleProceedsWhenCorpusResolutionComplete(t *testing.T) {
	t.Parallel()

	handler := DeployableUnitCorrelationHandler{
		FactLoader:             &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader:         &stubDeployableUnitResolvedLoader{},
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"generation-1": true}),
		ResolutionsCompleteLookup: func() (bool, error) {
			return true, nil
		},
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil when own and corpus resolution are complete", err)
	}
	if got.Status != ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want %q", got.Status, ResultStatusSucceeded)
	}
}
