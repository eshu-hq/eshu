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

// crossplaneCandidateEnvelope builds a content_entity fact envelope with the
// given entity_type. entityType must be the canonical Neo4j label the real
// pipeline stamps (internal/content/shape/materialize.go materializeEntities
// sets EntityType to the PascalCase label, e.g. "K8sResource",
// "CrossplaneXRD", "Function" -- never the lowercase entityTypeLabelMap key),
// so a caller passing the lowercase form would silently test a shape the
// production collector never emits.
func crossplaneCandidateEnvelope(factID, scopeID, generationID, entityType string) facts.Envelope {
	return facts.Envelope{
		FactID:       factID,
		ScopeID:      scopeID,
		GenerationID: generationID,
		FactKind:     "content_entity",
		ObservedAt:   time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "git",
		},
		Payload: map[string]any{
			"entity_id":     "content-entity:" + factID,
			"entity_type":   entityType,
			"entity_name":   "some-name",
			"relative_path": "some/path.yaml",
			"repo_id":       "repo-1",
		},
	}
}

func TestBuildProjectionQueuesCrossplaneSatisfiedByIntentForK8sResourceCandidate(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "repo://github.com/example/app",
		ScopeKind:    "git_repository",
		SourceSystem: "git",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "crossplane-generation-1",
		ObservedAt:   time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.July, 1, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}

	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		crossplaneCandidateEnvelope("fact-k8s-1", scopeValue.ScopeID, generation.GenerationID, "K8sResource"),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireCrossplaneSatisfiedByIntent(t, projection.reducerIntents)
	if got, want := intent.EntityKey, "crossplane_satisfied_by_materialization:repo://github.com/example/app"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-k8s-1"; got != want {
		t.Fatalf("intent.FactID = %q, want fact-k8s-1", got)
	}
	if got, want := intent.SourceSystem, "git"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionQueuesCrossplaneSatisfiedByIntentForXRDCandidate(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "repo://github.com/example/platform",
		ScopeKind:    "git_repository",
		SourceSystem: "git",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "crossplane-generation-2",
		ObservedAt:   time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.July, 1, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}

	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		crossplaneCandidateEnvelope("fact-xrd-1", scopeValue.ScopeID, generation.GenerationID, "CrossplaneXRD"),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	requireCrossplaneSatisfiedByIntent(t, projection.reducerIntents)
}

// TestBuildProjectionDoesNotQueueCrossplaneSatisfiedByForUnrelatedEntity proves
// the trigger is precise: a generation with content_entity facts but no
// k8s_resource/crossplane_xrd candidate must not enqueue the domain, keeping
// the intent narrow rather than firing on any content_entity presence.
func TestBuildProjectionDoesNotQueueCrossplaneSatisfiedByForUnrelatedEntity(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "repo://github.com/example/lib",
		ScopeKind:    "git_repository",
		SourceSystem: "git",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "crossplane-generation-3",
		ObservedAt:   time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.July, 1, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}

	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		crossplaneCandidateEnvelope("fact-func-1", scopeValue.ScopeID, generation.GenerationID, "Function"),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainCrossplaneSatisfiedByMaterialization {
			t.Fatalf("unexpected crossplane_satisfied_by_materialization intent from a non-candidate entity_type")
		}
	}
}

func requireCrossplaneSatisfiedByIntent(t *testing.T, intents []ReducerIntent) ReducerIntent {
	t.Helper()
	for _, intent := range intents {
		if intent.Domain == reducer.DomainCrossplaneSatisfiedByMaterialization {
			return intent
		}
	}
	t.Fatalf("crossplane_satisfied_by_materialization intent missing from %#v", intents)
	return ReducerIntent{}
}
