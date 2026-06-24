// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestWorkloadMaterializationHandlerEnqueuesRepairWhenIntentPhasePublishFails(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-routes-only",
					"name":     "routes-only",
				},
				ObservedAt: now,
			},
		},
	}
	publisher := &recordingGraphProjectionPhasePublisher{err: errors.New("phase publish failed")}
	repairQueue := &recordingSemanticEntityRepairQueue{}
	handler := WorkloadMaterializationHandler{
		FactLoader:     loader,
		Materializer:   NewWorkloadMaterializer(&recordingCypherExecutor{}),
		PhasePublisher: publisher,
		RepairQueue:    repairQueue,
	}

	intent := Intent{
		IntentID:     "intent-wm-zero",
		ScopeID:      "scope-routes",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainWorkloadMaterialization,
		Cause:        "facts projected",
		EntityKeys:   []string{"repo:routes-only"},
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	}

	_, err := handler.Handle(context.Background(), intent)
	if err == nil {
		t.Fatal("Handle() error = nil, want publish failure")
	}
	repairs := graphProjectionPhaseRepairsForTest(repairQueue.calls)
	if got, want := len(repairs), 2; got != want {
		t.Fatalf("repair rows = %d, want %d", got, want)
	}
	wantState, ok := graphProjectionPhaseStateForIntent(
		intent,
		GraphProjectionKeyspaceServiceUID,
		GraphProjectionPhaseWorkloadMaterialization,
		time.Time{},
	)
	if !ok {
		t.Fatal("intent phase state not derivable")
	}
	if !graphProjectionPhaseRepairKeysContain(repairs, wantState.Key) {
		t.Fatalf("repair keys = %+v, want intent key %+v", graphProjectionPhaseRepairKeysForTest(repairs), wantState.Key)
	}
	wantRepoKey := workloadMaterializationRepoReadinessKey("scope-routes", "repo-routes-only", "gen-1")
	if !graphProjectionPhaseRepairKeysContain(repairs, wantRepoKey) {
		t.Fatalf("repair keys = %+v, want repo readiness key %+v", graphProjectionPhaseRepairKeysForTest(repairs), wantRepoKey)
	}
}

func TestWorkloadMaterializationHandlerEnqueuesRepoReadinessRepairWhenIntentPublishFailsAfterGraphWrite(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-payments",
					"name":     "payments",
				},
				ObservedAt: now,
			},
			{
				FactID:   "fact-file",
				FactKind: "file",
				Payload: map[string]any{
					"repo_id": "repo-payments",
					"parsed_file_data": map[string]any{
						"k8s_resources": []any{
							map[string]any{"name": "payments", "kind": "Deployment", "namespace": "production"},
						},
					},
				},
				ObservedAt: now,
			},
		},
	}
	publisher := &recordingGraphProjectionPhasePublisher{err: errors.New("phase publish failed")}
	repairQueue := &recordingSemanticEntityRepairQueue{}
	handler := WorkloadMaterializationHandler{
		FactLoader:     loader,
		Materializer:   NewWorkloadMaterializer(&recordingCypherExecutor{}),
		PhasePublisher: publisher,
		RepairQueue:    repairQueue,
	}

	intent := Intent{
		IntentID:     "intent-wm-repo-key",
		ScopeID:      "scope-payments",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainWorkloadMaterialization,
		Cause:        "facts projected",
		EntityKeys:   []string{"repo:payments"},
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	}

	_, err := handler.Handle(context.Background(), intent)
	if err == nil {
		t.Fatal("Handle() error = nil, want publish failure")
	}
	repairs := graphProjectionPhaseRepairsForTest(repairQueue.calls)
	if got, want := len(repairs), 2; got != want {
		t.Fatalf("repair rows = %d, want %d", got, want)
	}
	wantIntentState, ok := graphProjectionPhaseStateForIntent(
		intent,
		GraphProjectionKeyspaceServiceUID,
		GraphProjectionPhaseWorkloadMaterialization,
		time.Time{},
	)
	if !ok {
		t.Fatal("intent phase state not derivable")
	}
	if !graphProjectionPhaseRepairKeysContain(repairs, wantIntentState.Key) {
		t.Fatalf("repair keys = %+v, want intent key %+v", graphProjectionPhaseRepairKeysForTest(repairs), wantIntentState.Key)
	}
	wantRepoKey := workloadMaterializationRepoReadinessKey("scope-payments", "repo-payments", "gen-1")
	if !graphProjectionPhaseRepairKeysContain(repairs, wantRepoKey) {
		t.Fatalf("repair keys = %+v, want repo readiness key %+v", graphProjectionPhaseRepairKeysForTest(repairs), wantRepoKey)
	}
}

func TestWorkloadMaterializationHandlerEnqueuesRepairWhenRepoReadinessPublishFails(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-payments",
					"name":     "payments",
				},
				ObservedAt: now,
			},
			{
				FactID:   "fact-file",
				FactKind: "file",
				Payload: map[string]any{
					"repo_id": "repo-payments",
					"parsed_file_data": map[string]any{
						"k8s_resources": []any{
							map[string]any{"name": "payments", "kind": "Deployment", "namespace": "production"},
						},
					},
				},
				ObservedAt: now,
			},
		},
	}
	publisher := &sequenceGraphProjectionPhasePublisher{
		errors: []error{nil, errors.New("repo readiness publish failed")},
	}
	repairQueue := &recordingSemanticEntityRepairQueue{}
	handler := WorkloadMaterializationHandler{
		FactLoader:     loader,
		Materializer:   NewWorkloadMaterializer(&recordingCypherExecutor{}),
		PhasePublisher: publisher,
		RepairQueue:    repairQueue,
	}

	intent := Intent{
		IntentID:     "intent-wm-repo-key",
		ScopeID:      "scope-payments",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainWorkloadMaterialization,
		Cause:        "facts projected",
		EntityKeys:   []string{"repo:payments"},
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	}

	_, err := handler.Handle(context.Background(), intent)
	if err == nil {
		t.Fatal("Handle() error = nil, want repo readiness publish failure")
	}
	if got, want := len(repairQueue.calls), 1; got != want {
		t.Fatalf("repair queue calls = %d, want %d", got, want)
	}
	if got, want := len(repairQueue.calls[0]), 1; got != want {
		t.Fatalf("repair rows = %d, want %d", got, want)
	}
	wantKey := workloadMaterializationRepoReadinessKey("scope-payments", "repo-payments", "gen-1")
	gotRepair := repairQueue.calls[0][0]
	if gotRepair.Key != wantKey {
		t.Fatalf("repair key = %+v, want %+v", gotRepair.Key, wantKey)
	}
	if gotRepair.Phase != GraphProjectionPhaseWorkloadMaterialization {
		t.Fatalf("repair phase = %q, want %q", gotRepair.Phase, GraphProjectionPhaseWorkloadMaterialization)
	}
}

func TestWorkloadMaterializationHandlerEnqueuesRepairWhenZeroCandidateRepoReadinessPublishFails(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-routes-only",
					"name":     "routes-only",
				},
				ObservedAt: now,
			},
		},
	}
	publisher := &sequenceGraphProjectionPhasePublisher{
		errors: []error{nil, errors.New("repo readiness publish failed")},
	}
	repairQueue := &recordingSemanticEntityRepairQueue{}
	handler := WorkloadMaterializationHandler{
		FactLoader:     loader,
		Materializer:   NewWorkloadMaterializer(&recordingCypherExecutor{}),
		PhasePublisher: publisher,
		RepairQueue:    repairQueue,
	}

	intent := Intent{
		IntentID:     "intent-wm-zero",
		ScopeID:      "scope-routes",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainWorkloadMaterialization,
		Cause:        "facts projected",
		EntityKeys:   []string{"repo:routes-only"},
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	}

	_, err := handler.Handle(context.Background(), intent)
	if err == nil {
		t.Fatal("Handle() error = nil, want repo readiness publish failure")
	}
	if got, want := len(repairQueue.calls), 1; got != want {
		t.Fatalf("repair queue calls = %d, want %d", got, want)
	}
	if got, want := len(repairQueue.calls[0]), 1; got != want {
		t.Fatalf("repair rows = %d, want %d", got, want)
	}
	wantKey := workloadMaterializationRepoReadinessKey("scope-routes", "repo-routes-only", "gen-1")
	if got := repairQueue.calls[0][0].Key; got != wantKey {
		t.Fatalf("repair key = %+v, want %+v", got, wantKey)
	}
}

type sequenceGraphProjectionPhasePublisher struct {
	calls  [][]GraphProjectionPhaseState
	errors []error
}

func (p *sequenceGraphProjectionPhasePublisher) PublishGraphProjectionPhases(
	_ context.Context,
	rows []GraphProjectionPhaseState,
) error {
	cloned := make([]GraphProjectionPhaseState, len(rows))
	copy(cloned, rows)
	p.calls = append(p.calls, cloned)
	idx := len(p.calls) - 1
	if idx < len(p.errors) {
		return p.errors[idx]
	}
	return nil
}

func graphProjectionPhaseRepairsForTest(calls [][]GraphProjectionPhaseRepair) []GraphProjectionPhaseRepair {
	var repairs []GraphProjectionPhaseRepair
	for _, call := range calls {
		repairs = append(repairs, call...)
	}
	return repairs
}

func graphProjectionPhaseRepairKeysContain(repairs []GraphProjectionPhaseRepair, key GraphProjectionPhaseKey) bool {
	for _, repair := range repairs {
		if repair.Key == key && repair.Phase == GraphProjectionPhaseWorkloadMaterialization {
			return true
		}
	}
	return false
}

func graphProjectionPhaseRepairKeysForTest(repairs []GraphProjectionPhaseRepair) []GraphProjectionPhaseKey {
	keys := make([]GraphProjectionPhaseKey, 0, len(repairs))
	for _, repair := range repairs {
		keys = append(keys, repair.Key)
	}
	return keys
}
