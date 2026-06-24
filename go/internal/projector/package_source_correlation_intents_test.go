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

func TestBuildProjectionQueuesSinglePackageSourceCorrelationIntent(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "package-registry:npm:team-api",
		ScopeKind:    "package_registry",
		SourceSystem: "package_registry",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-1",
		ObservedAt:   time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, 5, 14, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	envelopes := []facts.Envelope{
		packageSourceHintEnvelope("fact-source-1", scopeValue.ScopeID, generation.GenerationID),
		packageSourceHintEnvelope("fact-source-2", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := requirePackageSourceCorrelationIntent(t, projection.reducerIntents)
	if got, want := intent.Domain, reducer.DomainPackageSourceCorrelation; got != want {
		t.Fatalf("intent.Domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKey, "package_source_correlation:package-registry:npm:team-api"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-source-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first source hint fact", got)
	}
}

func TestBuildProjectionQueuesPackageSourceCorrelationForPackageIdentity(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "package-registry:npm:vite",
		ScopeKind:    "package_registry",
		SourceSystem: "package_registry",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-1",
		ObservedAt:   time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, 5, 23, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	envelopes := []facts.Envelope{
		packageIdentityEnvelope("fact-package-1", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := requirePackageSourceCorrelationIntent(t, projection.reducerIntents)
	if got, want := intent.Domain, reducer.DomainPackageSourceCorrelation; got != want {
		t.Fatalf("intent.Domain = %q, want %q", got, want)
	}
	if got, want := intent.Reason, "package registry identity observed"; got != want {
		t.Fatalf("intent.Reason = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-package-1"; got != want {
		t.Fatalf("intent.FactID = %q, want package identity fact", got)
	}
}

func requirePackageSourceCorrelationIntent(t *testing.T, intents []ReducerIntent) ReducerIntent {
	t.Helper()
	for _, intent := range intents {
		if intent.Domain == reducer.DomainPackageSourceCorrelation {
			return intent
		}
	}
	t.Fatalf("package_source_correlation intent missing from %#v", intents)
	return ReducerIntent{}
}

func packageSourceHintEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.PackageRegistrySourceHintFactKind,
		SchemaVersion:    facts.PackageRegistrySourceHintSchemaVersion,
		CollectorKind:    "package_registry",
		SourceConfidence: "reported",
		ObservedAt:       time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "package_registry",
		},
		Payload: map[string]any{
			"package_id":     "pkg:npm://registry.example/team-api",
			"hint_kind":      "repository",
			"normalized_url": "https://github.com/acme/team-api",
		},
	}
}

func packageIdentityEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.PackageRegistryPackageFactKind,
		SchemaVersion:    facts.PackageRegistryPackageSchemaVersion,
		CollectorKind:    "package_registry",
		SourceConfidence: "reported",
		ObservedAt:       time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "package_registry",
		},
		Payload: map[string]any{
			"package_id":      "npm://registry.npmjs.org/vite",
			"ecosystem":       "npm",
			"raw_name":        "vite",
			"normalized_name": "vite",
		},
	}
}
