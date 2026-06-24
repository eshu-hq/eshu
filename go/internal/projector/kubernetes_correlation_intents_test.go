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

// TestBuildProjectionQueuesSingleKubernetesCorrelationIntentForPodTemplate
// proves one kubernetes_correlation intent is enqueued per scope generation that
// observed a live workload, triggered by the pod-template fact.
func TestBuildProjectionQueuesSingleKubernetesCorrelationIntentForPodTemplate(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "k8s://prod-us-east-1",
		ScopeKind:    scope.KindCluster,
		SourceSystem: "kubernetes_live",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "k8s-generation-1",
		ObservedAt:   time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.May, 15, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	envelopes := []facts.Envelope{
		kubernetesPodTemplateEnvelope("fact-pod-1", scopeValue.ScopeID, generation.GenerationID),
		kubernetesWarningEnvelope("fact-warn-1", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	var found *ReducerIntent
	for i := range projection.reducerIntents {
		if projection.reducerIntents[i].Domain == reducer.DomainKubernetesCorrelation {
			found = &projection.reducerIntents[i]
		}
	}
	if found == nil {
		t.Fatalf("no kubernetes_correlation intent enqueued; intents=%+v", projection.reducerIntents)
	}
	if got, want := found.EntityKey, "kubernetes_correlation:k8s://prod-us-east-1"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := found.FactID, "fact-pod-1"; got != want {
		t.Fatalf("intent.FactID = %q, want the pod-template fact", got)
	}
	if got, want := found.SourceSystem, "kubernetes_live"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

// TestBuildProjectionQueuesNoKubernetesCorrelationIntentWithoutPodTemplate
// proves a warning-only generation does not enqueue a correlation intent (no
// workload to correlate).
func TestBuildProjectionQueuesNoKubernetesCorrelationIntentWithoutPodTemplate(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "k8s://prod-us-east-1",
		ScopeKind:    scope.KindCluster,
		SourceSystem: "kubernetes_live",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "k8s-generation-2",
		ObservedAt:   time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.May, 15, 10, 0, 1, 0, time.UTC),
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
		if intent.Domain == reducer.DomainKubernetesCorrelation {
			t.Fatalf("unexpected kubernetes_correlation intent for warning-only generation: %+v", intent)
		}
	}
}

func kubernetesPodTemplateEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.KubernetesPodTemplateFactKind,
		SchemaVersion:    facts.KubernetesPodTemplateSchemaVersion,
		CollectorKind:    "kubernetes_live",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "kubernetes_live",
		},
		Payload: map[string]any{
			"cluster_id": "prod-us-east-1",
			"object_id":  "k8s://prod-us-east-1/apps/v1/deployments/checkout/checkout",
			"namespace":  "checkout",
			"name":       "checkout",
			"uid":        "uid-1",
			"image_refs": []string{"registry.example.com/team/checkout@sha256:aaaa"},
		},
	}
}

func kubernetesWarningEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.KubernetesWarningFactKind,
		SchemaVersion:    facts.KubernetesWarningSchemaVersion,
		CollectorKind:    "kubernetes_live",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "kubernetes_live",
		},
		Payload: map[string]any{
			"cluster_id":     "prod-us-east-1",
			"reason":         "ambiguous_selector",
			"resource_scope": "apps/v1/deployments",
		},
	}
}
