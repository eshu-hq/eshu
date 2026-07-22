// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestKubernetesNamespaceMaterializationCompleteEmptySnapshotRetractsStaleNodes(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesNamespaceNodeWriter{}
	handler := KubernetesNamespaceMaterializationHandler{
		FactLoader: &stubFactLoader{},
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-empty-complete",
		ScopeID:      "kubernetes_live:scope-prod",
		GenerationID: "generation-empty-complete",
		Domain:       DomainKubernetesNamespaceMaterialization,
		Payload: map[string]any{
			"cluster_id":         "prod-eks",
			"reconcile_complete": true,
		},
		EnqueuedAt:  time.Now(),
		AvailableAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.calls != 0 {
		t.Fatalf("WriteKubernetesNamespaceNodes calls = %d, want 0 for empty snapshot", writer.calls)
	}
	if got, want := writer.retractCalls, 1; got != want {
		t.Fatalf("RetractStaleKubernetesNamespaceNodes calls = %d, want %d", got, want)
	}
	if got, want := writer.retractClusterID, "prod-eks"; got != want {
		t.Fatalf("retract cluster ID = %q, want %q", got, want)
	}
	if got, want := writer.retractGenerationID, "generation-empty-complete"; got != want {
		t.Fatalf("retract generation ID = %q, want %q", got, want)
	}
	if got, want := result.CanonicalWrites, 0; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if !strings.Contains(result.EvidenceSummary, "reconciliation=applied") {
		t.Fatalf("EvidenceSummary = %q, want applied reconciliation status", result.EvidenceSummary)
	}
}

func TestKubernetesNamespaceMaterializationCompleteSnapshotUpsertsBeforeRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesNamespaceNodeWriter{}
	handler := KubernetesNamespaceMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			kubernetesNamespaceEnvelope(sampleNamespacePayload("object-current", "payments", nil)),
		}},
		NodeWriter: writer,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-complete",
		ScopeID:      "kubernetes_live:scope-prod",
		GenerationID: "generation-current",
		Domain:       DomainKubernetesNamespaceMaterialization,
		Payload: map[string]any{
			"cluster_id":         "prod-eks",
			"reconcile_complete": true,
		},
		EnqueuedAt:  time.Now(),
		AvailableAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := writer.calls, 1; got != want {
		t.Fatalf("write calls = %d, want %d", got, want)
	}
	if got, want := writer.retractCalls, 1; got != want {
		t.Fatalf("retract calls = %d, want %d", got, want)
	}
	if got, want := writer.events, []string{"write", "retract"}; !slices.Equal(got, want) {
		t.Fatalf("writer events = %v, want %v", got, want)
	}
	if got, want := writer.rows[0]["generation_id"], "generation-current"; got != want {
		t.Fatalf("row generation_id = %#v, want %#v", got, want)
	}
}

func TestKubernetesNamespaceMaterializationRetractFailureFailsIntent(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesNamespaceNodeWriter{retractErr: errors.New("retract failed")}
	handler := KubernetesNamespaceMaterializationHandler{
		FactLoader: &stubFactLoader{},
		NodeWriter: writer,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-complete",
		ScopeID:      "kubernetes_live:scope-prod",
		GenerationID: "generation-current",
		Domain:       DomainKubernetesNamespaceMaterialization,
		Payload: map[string]any{
			"cluster_id":         "prod-eks",
			"reconcile_complete": true,
		},
		EnqueuedAt:  time.Now(),
		AvailableAt: time.Now(),
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want retract failure")
	}
}

func TestKubernetesNamespaceMaterializationCompleteSnapshotWithInvalidFactDoesNotRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesNamespaceNodeWriter{}
	handler := KubernetesNamespaceMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			kubernetesNamespaceEnvelope(map[string]any{
				"cluster_id": "prod-eks",
				"namespace":  "still-present",
			}),
		}},
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-complete-invalid",
		ScopeID:      "kubernetes_live:scope-prod",
		GenerationID: "generation-current",
		Domain:       DomainKubernetesNamespaceMaterialization,
		Payload: map[string]any{
			"cluster_id":         "prod-eks",
			"reconcile_complete": true,
		},
		EnqueuedAt:  time.Now(),
		AvailableAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil quarantine result", err)
	}
	if got := writer.retractCalls; got != 0 {
		t.Fatalf("retract calls = %d, want 0 when any namespace fact is invalid", got)
	}
	if len(result.SubSignals) == 0 {
		t.Fatal("SubSignals empty, want input_invalid quarantine signal")
	}
	if !strings.Contains(result.EvidenceSummary, "reconciliation=suppressed_input_invalid") {
		t.Fatalf("EvidenceSummary = %q, want suppressed reconciliation status", result.EvidenceSummary)
	}
}

func TestKubernetesNamespaceMaterializationRejectsCompleteSnapshotClusterMismatchBeforeWrite(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesNamespaceNodeWriter{}
	handler := KubernetesNamespaceMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			kubernetesNamespaceEnvelope(sampleNamespacePayload("object-other", "payments", nil)),
		}},
		NodeWriter: writer,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-complete-mismatch",
		ScopeID:      "kubernetes_live:scope-prod",
		GenerationID: "generation-current",
		Domain:       DomainKubernetesNamespaceMaterialization,
		Payload: map[string]any{
			"cluster_id":         "other-eks",
			"reconcile_complete": true,
		},
		EnqueuedAt:  time.Now(),
		AvailableAt: time.Now(),
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want complete-snapshot cluster mismatch error")
	}
	if writer.calls != 0 || writer.retractCalls != 0 {
		t.Fatalf("writer calls = %d, retract calls = %d, want no graph mutation before mismatch rejection", writer.calls, writer.retractCalls)
	}
}
