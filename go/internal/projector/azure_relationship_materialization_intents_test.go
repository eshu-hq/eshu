// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildProjectionQueuesAzureRelationshipMaterialization(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "azure:tenant:subscription:sub-1:compute:eastus:resource_graph"}
	generation := scope.ScopeGeneration{GenerationID: "gen-azure-1"}
	intents := appendScopeGenerationReducerIntents(nil, scopeValue, generation, []facts.Envelope{
		{
			FactID:           "fact-rel-1",
			FactKind:         facts.AzureCloudRelationshipFactKind,
			SchemaVersion:    facts.AzureCloudRelationshipSchemaVersion,
			SourceRef:        facts.Ref{SourceSystem: "azure"},
			SourceConfidence: facts.SourceConfidenceReported,
			Payload: map[string]any{
				"source_arm_resource_id": "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
				"target_arm_resource_id": "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Network/networkInterfaces/nic",
				"relationship_type":      "managed_by",
				"support_state":          "supported",
			},
		},
	})

	intent := intentForDomain(t, intents, reducer.DomainAzureRelationshipMaterialization)
	if got, want := intent.ScopeID, scopeValue.ScopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := intent.GenerationID, "gen-azure-1"; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := intent.EntityKey, "azure_resource_materialization:"+scopeValue.ScopeID; got != want {
		t.Fatalf("EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-rel-1"; got != want {
		t.Fatalf("FactID = %q, want %q", got, want)
	}
}

func TestBuildProjectionSkipsAzureRelationshipMaterializationWithoutRelationshipFacts(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "azure:tenant:subscription:sub-1:compute:eastus:resource_graph"}
	generation := scope.ScopeGeneration{GenerationID: "gen-azure-1"}
	intents := appendScopeGenerationReducerIntents(nil, scopeValue, generation, []facts.Envelope{
		{
			FactID:        "fact-resource-1",
			FactKind:      facts.AzureCloudResourceFactKind,
			SchemaVersion: facts.AzureCloudResourceSchemaVersion,
			Payload: map[string]any{
				"arm_resource_id": "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
				"resource_type":   "microsoft.compute/virtualmachines",
			},
		},
	})

	for _, intent := range intents {
		if intent.Domain == reducer.DomainAzureRelationshipMaterialization {
			t.Fatalf("unexpected Azure relationship intent for resource-only facts: %#v", intent)
		}
	}
}
