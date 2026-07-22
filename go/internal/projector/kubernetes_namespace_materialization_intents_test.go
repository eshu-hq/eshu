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

func TestBuildProjectionQueuesKubernetesNamespaceMaterializationIntent(t *testing.T) {
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

	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		kubernetesNamespaceEnvelope("fact-namespace-1", scopeValue.ScopeID, generation.GenerationID),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	for _, intent := range projection.reducerIntents {
		if intent.Domain != reducer.DomainKubernetesNamespaceMaterialization {
			continue
		}
		if got, want := intent.EntityKey, "kubernetes_namespace_materialization:k8s://prod-us-east-1"; got != want {
			t.Fatalf("intent.EntityKey = %q, want %q", got, want)
		}
		if got, want := intent.FactID, "fact-namespace-1"; got != want {
			t.Fatalf("intent.FactID = %q, want %q", got, want)
		}
		if got, want := intent.SourceSystem, "kubernetes_live"; got != want {
			t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
		}
		return
	}
	t.Fatalf("no kubernetes_namespace_materialization intent enqueued; intents=%+v", projection.reducerIntents)
}

func TestBuildProjectionQueuesNoKubernetesNamespaceMaterializationIntentWithoutNamespaceFact(t *testing.T) {
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

	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		kubernetesWarningEnvelope("fact-warn-1", scopeValue.ScopeID, generation.GenerationID),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainKubernetesNamespaceMaterialization {
			t.Fatalf("unexpected kubernetes_namespace_materialization intent without a namespace fact: %+v", intent)
		}
	}
}

func kubernetesNamespaceEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.KubernetesNamespaceFactKind,
		SchemaVersion:    facts.KubernetesNamespaceSchemaVersion,
		CollectorKind:    "kubernetes_live",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "kubernetes_live",
		},
		Payload: map[string]any{
			"cluster_id": "prod-us-east-1",
			"object_id":  "k8s://prod-us-east-1/core/v1/namespaces/payments-prod",
			"namespace":  "payments-prod",
			"labels":     map[string]any{"environment": "prod"},
		},
	}
}
