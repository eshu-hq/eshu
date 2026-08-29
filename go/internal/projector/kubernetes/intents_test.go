// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetes

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestPodTemplateIntentBuildersPreserveContract(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "k8s://prod-us-east-1"
		generationID = "k8s-generation-1"
	)
	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: facts.KubernetesWarningFactKind, FactID: "warning-first"},
		{
			FactKind:      facts.KubernetesPodTemplateFactKind,
			FactID:        "pod-template-first",
			CollectorKind: " collector-fallback ",
			SourceRef:     facts.Ref{SourceSystem: " kubernetes-live-source "},
		},
		{
			FactKind:      facts.KubernetesPodTemplateFactKind,
			FactID:        "pod-template-second",
			CollectorKind: "ignored-collector",
			SourceRef:     facts.Ref{SourceSystem: "ignored-source"},
		},
	})

	tests := []struct {
		name  string
		build func(string, string, projectorintent.FactLookup) (projectorintent.ReducerIntent, bool)
		want  projectorintent.ReducerIntent
	}{
		{
			name:  "correlation",
			build: BuildCorrelationReducerIntent,
			want: projectorintent.ReducerIntent{
				ScopeID: scopeID, GenerationID: generationID,
				Domain:    reducer.DomainKubernetesCorrelation,
				EntityKey: "kubernetes_correlation:" + scopeID,
				Reason:    "kubernetes live workload evidence observed",
				FactID:    "pod-template-first", SourceSystem: "kubernetes-live-source",
			},
		},
		{
			name:  "workload materialization",
			build: BuildWorkloadMaterializationReducerIntent,
			want: projectorintent.ReducerIntent{
				ScopeID: scopeID, GenerationID: generationID,
				Domain:    reducer.DomainKubernetesWorkloadMaterialization,
				EntityKey: "kubernetes_workload_materialization:" + scopeID,
				Reason:    "kubernetes live workload pod-template facts observed",
				FactID:    "pod-template-first", SourceSystem: "kubernetes-live-source",
			},
		},
		{
			name:  "correlation materialization shares workload acceptance unit",
			build: BuildCorrelationMaterializationReducerIntent,
			want: projectorintent.ReducerIntent{
				ScopeID: scopeID, GenerationID: generationID,
				Domain:    reducer.DomainKubernetesCorrelationMaterialization,
				EntityKey: "kubernetes_workload_materialization:" + scopeID,
				Reason:    "kubernetes live workload pod-template facts observed",
				FactID:    "pod-template-first", SourceSystem: "kubernetes-live-source",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := test.build(scopeID, generationID, lookup)
			if !ok {
				t.Fatal("builder returned ok=false, want true")
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("intent = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestPodTemplateIntentBuildersRejectMissingTrigger(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind: facts.KubernetesWarningFactKind,
		FactID:   "warning-only",
	}})
	builders := []struct {
		name  string
		build func(string, string, projectorintent.FactLookup) (projectorintent.ReducerIntent, bool)
	}{
		{"correlation", BuildCorrelationReducerIntent},
		{"workload materialization", BuildWorkloadMaterializationReducerIntent},
		{"correlation materialization", BuildCorrelationMaterializationReducerIntent},
	}
	for _, builder := range builders {
		t.Run(builder.name, func(t *testing.T) {
			t.Parallel()
			got, ok := builder.build("scope", "generation", lookup)
			if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
				t.Fatalf("builder returned (%#v, %t), want zero intent and false", got, ok)
			}
		})
	}
}

func TestPodTemplateIntentSourceFallsBackToTrimmedCollector(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind:      facts.KubernetesPodTemplateFactKind,
		FactID:        "pod-template",
		CollectorKind: " kubernetes_live ",
		SourceRef:     facts.Ref{SourceSystem: "   "},
	}})
	got, ok := BuildCorrelationReducerIntent("scope", "generation", lookup)
	if !ok {
		t.Fatal("BuildCorrelationReducerIntent() ok = false, want true")
	}
	if got.SourceSystem != "kubernetes_live" {
		t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, "kubernetes_live")
	}
}

func TestNamespaceMaterializationIntentPreservesFactAndReconciliationContract(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "k8s://prod-us-east-1"
		generationID = "k8s-generation-namespace"
	)
	namespaceFact := facts.Envelope{
		FactKind:      facts.KubernetesNamespaceFactKind,
		FactID:        "namespace-first",
		CollectorKind: "ignored-collector",
		SourceRef:     facts.Ref{SourceSystem: " kubernetes-live-source "},
	}
	tests := []struct {
		name       string
		scopeValue scope.IngestionScope
		generation scope.ScopeGeneration
		facts      []facts.Envelope
		want       projectorintent.ReducerIntent
		wantOK     bool
	}{
		{
			name:       "namespace fact",
			scopeValue: scope.IngestionScope{ScopeID: scopeID, ScopeKind: scope.KindCluster},
			generation: scope.ScopeGeneration{GenerationID: generationID},
			facts:      []facts.Envelope{namespaceFact},
			wantOK:     true,
			want: projectorintent.ReducerIntent{
				ScopeID: scopeID, GenerationID: generationID,
				Domain:    reducer.DomainKubernetesNamespaceMaterialization,
				EntityKey: "kubernetes_namespace_materialization:" + scopeID,
				Reason:    "kubernetes live namespace facts observed",
				FactID:    "namespace-first", SourceSystem: "kubernetes-live-source",
			},
		},
		{
			name: "complete empty snapshot",
			scopeValue: scope.IngestionScope{
				ScopeID: scopeID, ScopeKind: scope.KindCluster,
				SourceSystem:  " kubernetes-live-scope ",
				CollectorKind: scope.CollectorKubernetesLive,
				Metadata:      map[string]string{"cluster_id": " prod-us-east-1 "},
			},
			generation: scope.ScopeGeneration{GenerationID: generationID, FreshnessHint: " complete "},
			wantOK:     true,
			want: projectorintent.ReducerIntent{
				ScopeID: scopeID, GenerationID: generationID,
				Domain:       reducer.DomainKubernetesNamespaceMaterialization,
				EntityKey:    "kubernetes_namespace_materialization:" + scopeID,
				Reason:       "complete kubernetes live namespace snapshot observed",
				SourceSystem: "kubernetes-live-scope",
				Payload:      map[string]any{"cluster_id": "prod-us-east-1", "reconcile_complete": true},
			},
		},
		{
			name:       "partial empty snapshot",
			scopeValue: completeKubernetesScope(scopeID, "prod-us-east-1"),
			generation: scope.ScopeGeneration{GenerationID: generationID, FreshnessHint: "partial"},
		},
		{
			name:       "complete snapshot with blank cluster id",
			scopeValue: completeKubernetesScope(scopeID, "   "),
			generation: scope.ScopeGeneration{GenerationID: generationID, FreshnessHint: "complete"},
		},
		{
			name: "complete snapshot with wrong collector",
			scopeValue: scope.IngestionScope{
				ScopeID: scopeID, ScopeKind: scope.KindCluster,
				CollectorKind: scope.CollectorGit,
				Metadata:      map[string]string{"cluster_id": "prod-us-east-1"},
			},
			generation: scope.ScopeGeneration{GenerationID: generationID, FreshnessHint: "complete"},
		},
		{
			name: "complete snapshot with wrong scope kind",
			scopeValue: scope.IngestionScope{
				ScopeID: scopeID, ScopeKind: scope.KindRepository,
				CollectorKind: scope.CollectorKubernetesLive,
				Metadata:      map[string]string{"cluster_id": "prod-us-east-1"},
			},
			generation: scope.ScopeGeneration{GenerationID: generationID, FreshnessHint: "complete"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := BuildNamespaceMaterializationReducerIntent(
				test.scopeValue,
				test.generation,
				projectorintent.NewFactLookup(test.facts),
			)
			if ok != test.wantOK || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("builder returned (%#v, %t), want (%#v, %t)", got, ok, test.want, test.wantOK)
			}
		})
	}
}

func completeKubernetesScope(scopeID, clusterID string) scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       scopeID,
		ScopeKind:     scope.KindCluster,
		SourceSystem:  "kubernetes_live",
		CollectorKind: scope.CollectorKubernetesLive,
		Metadata:      map[string]string{"cluster_id": clusterID},
	}
}
