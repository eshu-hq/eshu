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

// readinessLookupFromStates builds a GraphProjectionReadinessLookup over the
// concrete phase states a publisher recorded, so a test can gate the edge handler
// on the SAME GraphProjectionPhaseKey the workload handler actually published —
// not a stubbed readyLookup(true,true) that ignores the key. The lookup keys on
// the comparable GraphProjectionPhaseKey struct, so a drift in the
// graphPhaseAcceptanceUnitID derivation on either side changes the key and the
// lookup misses.
func readinessLookupFromStates(states []GraphProjectionPhaseState) GraphProjectionReadinessLookup {
	return func(key GraphProjectionPhaseKey, phase GraphProjectionPhase) (bool, bool) {
		for _, state := range states {
			if state.Key == key && state.Phase == phase {
				return true, true
			}
		}
		return false, false
	}
}

// runKubernetesCorrelationSeam drives the REAL workload-materialization handler to
// publish its canonical-nodes-committed phase under workloadEntityKey, then drives
// the REAL edge handler whose readiness gate looks the phase up by the key it
// derives from the edge intent. Both sides run the production
// graphProjectionPhaseStateForIntent / graphPhaseAcceptanceUnitID derivation; only
// the published key set is shared, so the seam is exercised end to end with no
// stubbed readiness lookup. It returns the edge handler's write count and error so
// the positive and negative cases assert against one code path.
func runKubernetesCorrelationSeam(t *testing.T, workloadEntityKey string) (int, error) {
	t.Helper()

	publisher := &recordingGraphProjectionPhasePublisher{}
	workload := KubernetesWorkloadMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			kubernetesPodTemplateEnvelope(samplePodTemplatePayload("object-a", "checkout")),
		}},
		NodeWriter:     &recordingKubernetesWorkloadNodeWriter{},
		PhasePublisher: publisher,
	}
	if _, err := workload.Handle(context.Background(), Intent{
		IntentID:     "wl-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainKubernetesWorkloadMaterialization,
		EntityKeys:   []string{workloadEntityKey},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}); err != nil {
		t.Fatalf("workload Handle returned error: %v", err)
	}

	var states []GraphProjectionPhaseState
	for _, call := range publisher.calls {
		states = append(states, call...)
	}

	writer := &recordingKubernetesCorrelationEdgeWriter{}
	edge := KubernetesCorrelationMaterializationHandler{
		FactLoader:           &stubKubernetesCorrelationFactLoader{scopeFacts: exactDigestEdgeFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      readinessLookupFromStates(states),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	_, err := edge.Handle(context.Background(), kubernetesCorrelationMaterializationIntent())
	return writer.writeCalls, err
}

// TestKubernetesCorrelationMaterializationSeamRealReadinessKeyOpensGate proves the
// load-bearing invariant from issue #4142 item 2: when the workload intent
// publishes its phase under the SAME acceptance unit the projector emits for both
// intents ("kubernetes_workload_materialization:<scope>"), the edge handler's real
// readiness lookup matches and the RUNS_IMAGE edge materializes. The phase key is
// not stubbed — it is whatever graphPhaseAcceptanceUnitID derives from the
// workload intent.
func TestKubernetesCorrelationMaterializationSeamRealReadinessKeyOpensGate(t *testing.T) {
	t.Parallel()

	writeCalls, err := runKubernetesCorrelationSeam(t, "kubernetes_workload_materialization:scope-1")
	if err != nil {
		t.Fatalf("edge Handle returned error with matching readiness key: %v", err)
	}
	if writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1 (the readiness key matched, so the edge must materialize)", writeCalls)
	}
}

// TestKubernetesCorrelationMaterializationSeamMismatchedKeyClosesGate is the
// negative half of the seam: when the workload intent publishes under a DIFFERENT
// acceptance unit than the edge intent derives, the keys differ and the edge gate
// stays closed (retryable, no writes). This is what makes the seam test
// load-bearing — if graphPhaseAcceptanceUnitID stopped honoring EntityKeys (e.g.
// always returned ScopeID), both intents would derive the same key, the gate would
// wrongly open, and THIS test would fail. So a regression in the derivation breaks
// a unit test rather than only the compose B-7 gate.
func TestKubernetesCorrelationMaterializationSeamMismatchedKeyClosesGate(t *testing.T) {
	t.Parallel()

	writeCalls, err := runKubernetesCorrelationSeam(t, "some_other_acceptance_unit:scope-1")
	if err == nil {
		t.Fatal("expected a retryable error when the published readiness key does not match the edge derivation")
	}
	if !IsRetryable(err) {
		t.Fatalf("error must be retryable so the intent re-enters the queue, got %v", err)
	}
	if writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 (the readiness key did not match, so no edge may write)", writeCalls)
	}
}

// TestKubernetesCorrelationNodesNotReadyExposesFailureClass pins the durable
// failure_class the queue uses to defer a readiness-gate miss without counting it
// against maxAttempts (issue #4142 item 3). The class string is the contract the
// claim/batch attempt-count CASE and the Go retry classifier both key on, so it is
// an exported constant rather than a buried literal.
func TestKubernetesCorrelationNodesNotReadyExposesFailureClass(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesCorrelationEdgeWriter{}
	handler := KubernetesCorrelationMaterializationHandler{
		FactLoader:      &stubKubernetesCorrelationFactLoader{scopeFacts: exactDigestEdgeFixture()},
		EdgeWriter:      writer,
		ReadinessLookup: readyLookup(false, false),
	}
	_, err := handler.Handle(context.Background(), kubernetesCorrelationMaterializationIntent())
	if err == nil {
		t.Fatal("expected a not-ready error while the workload phase is unpublished")
	}

	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("readiness-miss error does not expose FailureClass(): %v", err)
	}
	if got := classified.FailureClass(); got != KubernetesCorrelationNodesNotReadyFailureClass {
		t.Fatalf("FailureClass() = %q, want %q", got, KubernetesCorrelationNodesNotReadyFailureClass)
	}
}
