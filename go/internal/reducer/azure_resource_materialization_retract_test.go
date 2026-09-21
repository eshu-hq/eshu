// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestAzureResourceMaterializationRetractsDeletedUIDs is the #6887 Azure
// copy-paste guard: the predecessor-only uid reaches the retracter with the
// Azure evidence source (not a sibling family's kinds, extractor, or
// source).
func TestAzureResourceMaterializationRetractsDeletedUIDs(t *testing.T) {
	t.Parallel()

	kept := azureResourceEnvelope(map[string]any{
		"arm_resource_id":        "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/kept",
		"normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/kept",
		"subscription_id":        "sub-1",
		"resource_type":          "microsoft.compute/virtualmachines",
		"resource_name":          "kept",
		"location":               "eastus",
		"kind":                   "linux",
	})
	deleted := azureResourceEnvelope(map[string]any{
		"arm_resource_id":        "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/deleted",
		"normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/deleted",
		"subscription_id":        "sub-1",
		"resource_type":          "microsoft.compute/virtualmachines",
		"resource_name":          "deleted",
		"location":               "eastus",
		"kind":                   "linux",
	})
	loader := &generationScopedFactLoader{byGeneration: map[string][]facts.Envelope{
		"gen-2": {kept},
		"gen-1": {kept, deleted},
	}}
	retracter := &recordingCloudResourceNodeRetracter{retracted: 1}

	handler := AzureResourceMaterializationHandler{
		FactLoader:      loader,
		NodeWriter:      &recordingCloudResourceNodeWriter{},
		NodeRetracter:   retracter,
		PriorGeneration: func(context.Context, string, string) (string, bool, error) { return "gen-1", true, nil },
	}
	intent := Intent{
		IntentID:     "intent-azure-retract",
		ScopeID:      "scope-azure-1",
		GenerationID: "gen-2",
		Domain:       DomainAzureResourceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	wantDeleted := cloudResourceUID("sub-1", "eastus", "microsoft.compute/virtualmachines",
		"/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/deleted")
	if len(retracter.uids) != 1 || retracter.uids[0] != wantDeleted {
		t.Fatalf("retract uids = %v, want exactly [%q]", retracter.uids, wantDeleted)
	}
	if retracter.evidenceSource != azureResourceEvidenceSource {
		t.Fatalf("evidenceSource = %q, want %q", retracter.evidenceSource, azureResourceEvidenceSource)
	}
}
