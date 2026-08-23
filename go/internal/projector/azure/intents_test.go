// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azure

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildMaterializationReducerIntents(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "azure:subscription:sub-1"}
	generation := scope.ScopeGeneration{GenerationID: "generation-1"}
	tests := []struct {
		name          string
		fact          facts.Envelope
		build         func(scope.IngestionScope, scope.ScopeGeneration, projectorintent.FactLookup) (projectorintent.ReducerIntent, bool)
		wantDomain    reducer.Domain
		wantReason    string
		wantSource    string
		wantEntityKey string
	}{
		{
			name: "resource prefers source ref",
			fact: facts.Envelope{
				FactID:        "azure-resource-1",
				FactKind:      facts.AzureCloudResourceFactKind,
				CollectorKind: "azure-collector",
				SourceRef:     facts.Ref{SourceSystem: " azure-resource-graph "},
			},
			build:         BuildResourceMaterializationReducerIntent,
			wantDomain:    reducer.DomainAzureResourceMaterialization,
			wantReason:    "azure runtime resource facts observed",
			wantSource:    "azure-resource-graph",
			wantEntityKey: "azure_resource_materialization:" + scopeValue.ScopeID,
		},
		{
			name: "relationship falls back to collector",
			fact: facts.Envelope{
				FactID:        "azure-relationship-1",
				FactKind:      facts.AzureCloudRelationshipFactKind,
				CollectorKind: " azure-collector ",
			},
			build:         BuildRelationshipMaterializationReducerIntent,
			wantDomain:    reducer.DomainAzureRelationshipMaterialization,
			wantReason:    "azure runtime relationship facts observed",
			wantSource:    "azure-collector",
			wantEntityKey: "azure_resource_materialization:" + scopeValue.ScopeID,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := test.build(scopeValue, generation, projectorintent.NewFactLookup([]facts.Envelope{test.fact}))
			if !ok {
				t.Fatal("build intent ok = false, want true")
			}
			if got.ScopeID != scopeValue.ScopeID || got.GenerationID != generation.GenerationID {
				t.Fatalf("scope generation = %q/%q, want %q/%q", got.ScopeID, got.GenerationID, scopeValue.ScopeID, generation.GenerationID)
			}
			if got.Domain != test.wantDomain {
				t.Fatalf("Domain = %q, want %q", got.Domain, test.wantDomain)
			}
			if got.EntityKey != test.wantEntityKey {
				t.Fatalf("EntityKey = %q, want %q", got.EntityKey, test.wantEntityKey)
			}
			if got.Reason != test.wantReason {
				t.Fatalf("Reason = %q, want %q", got.Reason, test.wantReason)
			}
			if got.FactID != test.fact.FactID {
				t.Fatalf("FactID = %q, want %q", got.FactID, test.fact.FactID)
			}
			if got.SourceSystem != test.wantSource {
				t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, test.wantSource)
			}
		})
	}
}

func TestBuildMaterializationReducerIntentsSkipMissingKinds(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "azure:subscription:sub-1"}
	generation := scope.ScopeGeneration{GenerationID: "generation-1"}
	lookup := projectorintent.NewFactLookup([]facts.Envelope{{FactKind: facts.AWSResourceFactKind}})

	if got, ok := BuildResourceMaterializationReducerIntent(scopeValue, generation, lookup); ok {
		t.Fatalf("resource intent = %#v, want no intent", got)
	}
	if got, ok := BuildRelationshipMaterializationReducerIntent(scopeValue, generation, lookup); ok {
		t.Fatalf("relationship intent = %#v, want no intent", got)
	}
}
