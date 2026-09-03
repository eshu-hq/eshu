// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplanesatisfiedby

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildCrossplaneSatisfiedByMaterializationReducerIntentNoFactNoIntent(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind: "content_entity",
		Payload: map[string]any{
			"entity_type": "Function",
		},
	}})
	if _, ok := BuildCrossplaneSatisfiedByMaterializationReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued a crossplane_satisfied_by_materialization intent for a non-candidate entity_type")
	}
}

func TestBuildCrossplaneSatisfiedByMaterializationReducerIntentEmptyGeneration(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup(nil)
	if _, ok := BuildCrossplaneSatisfiedByMaterializationReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued a crossplane_satisfied_by_materialization intent for a generation with no facts at all")
	}
}

func TestBuildCrossplaneSatisfiedByMaterializationReducerIntentFromK8sResourceCandidate(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind: "content_entity",
		FactID:   "fact-k8s-1",
		Payload: map[string]any{
			"entity_type": "K8sResource",
		},
		SourceRef: facts.Ref{SourceSystem: "git"},
	}})
	intent, ok := BuildCrossplaneSatisfiedByMaterializationReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a K8sResource content_entity fact")
	}
	if intent.Domain != reducer.DomainCrossplaneSatisfiedByMaterialization {
		t.Fatalf("intent.Domain = %q, want crossplane_satisfied_by_materialization", intent.Domain)
	}
	if intent.EntityKey != "crossplane_satisfied_by_materialization:scope-1" {
		t.Fatalf("intent.EntityKey = %q", intent.EntityKey)
	}
	if intent.Reason != "k8s_resource/crossplane_xrd content-entity facts observed" {
		t.Fatalf("intent.Reason = %q", intent.Reason)
	}
	if intent.FactID != "fact-k8s-1" {
		t.Fatalf("intent.FactID = %q, want fact-k8s-1", intent.FactID)
	}
}

func TestBuildCrossplaneSatisfiedByMaterializationReducerIntentFromCrossplaneXRDCandidate(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind: "content_entity",
		FactID:   "fact-xrd-1",
		Payload: map[string]any{
			"entity_type": "CrossplaneXRD",
		},
	}})
	if _, ok := BuildCrossplaneSatisfiedByMaterializationReducerIntent("scope-1", "gen-1", lookup); !ok {
		t.Fatal("no intent queued for a CrossplaneXRD content_entity fact")
	}
}

func TestBuildCrossplaneSatisfiedByMaterializationReducerIntentEntityKindFallback(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind: "content_entity",
		FactID:   "fact-kind-fallback-1",
		Payload: map[string]any{
			"entity_kind": "K8sResource",
		},
	}})
	if _, ok := BuildCrossplaneSatisfiedByMaterializationReducerIntent("scope-1", "gen-1", lookup); !ok {
		t.Fatal("no intent queued when entity_kind (not entity_type) carries the candidate label")
	}
}

// TestBuildCrossplaneSatisfiedByMaterializationReducerIntentSourceSystemFallsBackToCollectorKind
// pins the shared two-tier projectorintent.SourceSystem label this family
// uses verbatim: SourceRef.SourceSystem wins when set, else the trimmed
// CollectorKind.
func TestBuildCrossplaneSatisfiedByMaterializationReducerIntentSourceSystemFallsBackToCollectorKind(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind: "content_entity",
		FactID:   "fact-k8s-2",
		Payload: map[string]any{
			"entity_type": "K8sResource",
		},
		CollectorKind: "  kubernetes  ",
	}})
	intent, ok := BuildCrossplaneSatisfiedByMaterializationReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a K8sResource content_entity fact")
	}
	if intent.SourceSystem != "kubernetes" {
		t.Fatalf("intent.SourceSystem = %q, want the trimmed CollectorKind fallback", intent.SourceSystem)
	}
}

// TestBuildCrossplaneSatisfiedByMaterializationReducerIntentSourceSystemPrefersSourceRef
// pins the tier ORDER, which the fallback test above cannot: it sets
// SourceRef.SourceSystem and CollectorKind to DIFFERENT values, so a
// regression that swapped the two tiers would change the result. A test
// where both tiers carry the same value passes either way and proves only
// that a label was produced.
func TestBuildCrossplaneSatisfiedByMaterializationReducerIntentSourceSystemPrefersSourceRef(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind: "content_entity",
		FactID:   "fact-k8s-3",
		Payload: map[string]any{
			"entity_type": "K8sResource",
		},
		CollectorKind: "kubelet_scanner",
		SourceRef:     facts.Ref{SourceSystem: "  kubernetes_live  "},
	}})
	intent, ok := BuildCrossplaneSatisfiedByMaterializationReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a K8sResource content_entity fact")
	}
	if intent.SourceSystem != "kubernetes_live" {
		t.Fatalf("intent.SourceSystem = %q, want the trimmed SourceRef.SourceSystem to win over CollectorKind %q",
			intent.SourceSystem, "kubelet_scanner")
	}
}
