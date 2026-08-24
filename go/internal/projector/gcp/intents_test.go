// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildMaterializationReducerIntents(t *testing.T) {
	t.Parallel()

	scopeID := "gcp:project:project-1"
	generationID := "generation-1"
	tests := []struct {
		name          string
		fact          facts.Envelope
		build         func(string, string, projectorintent.FactLookup) (projectorintent.ReducerIntent, bool)
		wantDomain    reducer.Domain
		wantReason    string
		wantSource    string
		wantEntityKey string
	}{
		{
			name: "resource prefers source ref",
			fact: facts.Envelope{
				FactID:        "gcp-resource-1",
				FactKind:      facts.GCPCloudResourceFactKind,
				CollectorKind: "gcp-collector",
				SourceRef:     facts.Ref{SourceSystem: " cloud-asset-inventory "},
			},
			build:         BuildResourceMaterializationReducerIntent,
			wantDomain:    reducer.DomainGCPResourceMaterialization,
			wantReason:    "gcp cloud resource facts observed",
			wantSource:    "cloud-asset-inventory",
			wantEntityKey: "gcp_resource_materialization:" + scopeID,
		},
		{
			name: "relationship falls back to collector",
			fact: facts.Envelope{
				FactID:        "gcp-relationship-1",
				FactKind:      facts.GCPCloudRelationshipFactKind,
				CollectorKind: " gcp-collector ",
			},
			build:         BuildRelationshipMaterializationReducerIntent,
			wantDomain:    reducer.DomainGCPRelationshipMaterialization,
			wantReason:    "gcp runtime relationship facts observed",
			wantSource:    "gcp-collector",
			wantEntityKey: "gcp_resource_materialization:" + scopeID,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := test.build(scopeID, generationID, projectorintent.NewFactLookup([]facts.Envelope{test.fact}))
			if !ok {
				t.Fatal("build intent ok = false, want true")
			}
			if got.ScopeID != scopeID || got.GenerationID != generationID {
				t.Fatalf("scope generation = %q/%q, want %q/%q", got.ScopeID, got.GenerationID, scopeID, generationID)
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

func TestBuildMaterializationReducerIntentsPreserveEarliestMatch(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactID: "resource-first", FactKind: facts.GCPCloudResourceFactKind},
		{FactID: "relationship-first", FactKind: facts.GCPCloudRelationshipFactKind},
		{FactID: "resource-later", FactKind: facts.GCPCloudResourceFactKind},
		{FactID: "relationship-later", FactKind: facts.GCPCloudRelationshipFactKind},
	})

	resource, ok := BuildResourceMaterializationReducerIntent("scope-1", "generation-1", lookup)
	if !ok || resource.FactID != "resource-first" {
		t.Fatalf("resource intent = %#v, %v; want earliest matching fact", resource, ok)
	}
	relationship, ok := BuildRelationshipMaterializationReducerIntent("scope-1", "generation-1", lookup)
	if !ok || relationship.FactID != "relationship-first" {
		t.Fatalf("relationship intent = %#v, %v; want earliest matching fact", relationship, ok)
	}
}

func TestBuildMaterializationReducerIntentsSkipMissingKinds(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{FactKind: facts.AWSResourceFactKind}})

	if got, ok := BuildResourceMaterializationReducerIntent("scope-1", "generation-1", lookup); ok {
		t.Fatalf("resource intent = %#v, want no intent", got)
	}
	if got, ok := BuildRelationshipMaterializationReducerIntent("scope-1", "generation-1", lookup); ok {
		t.Fatalf("relationship intent = %#v, want no intent", got)
	}
}
