// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func azureResourceEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:   "azure-resource-fact",
		FactKind: facts.AzureCloudResourceFactKind,
		Payload:  payload,
	}
}

func azureResourceIntent() Intent {
	return Intent{
		IntentID:     "intent-azure-resources-1",
		ScopeID:      "scope-azure-1",
		GenerationID: "gen-azure-1",
		Domain:       DomainAzureResourceMaterialization,
		EntityKeys:   []string{"azure_resource_materialization:scope-azure-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func TestExtractAzureCloudResourceNodeRowsBuildsStableUID(t *testing.T) {
	t.Parallel()

	const armID = "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm"
	rows, quarantined, err := ExtractAzureCloudResourceNodeRows([]facts.Envelope{
		azureResourceEnvelope(map[string]any{
			"arm_resource_id":        armID,
			"normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm",
			"subscription_id":        "sub-1",
			"resource_type":          "microsoft.compute/virtualmachines",
			"resource_name":          "vm",
			"location":               "eastus",
			"kind":                   "linux",
		}),
	})
	if err != nil {
		t.Fatalf("ExtractAzureCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %v, want none", quarantined)
	}

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	wantUID := cloudResourceUID("sub-1", "eastus", "microsoft.compute/virtualmachines", "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm")
	if got := anyToString(rows[0]["uid"]); got != wantUID {
		t.Fatalf("uid = %q, want %q", got, wantUID)
	}
	if got := anyToString(rows[0]["resource_id"]); got != "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm" {
		t.Fatalf("resource_id = %q, want normalized ARM id", got)
	}
	if got := anyToString(rows[0]["arn"]); got != "" {
		t.Fatalf("arn = %q, want empty for Azure", got)
	}
}

func TestAzureResourceMaterializationPublishesReadinessPhase(t *testing.T) {
	t.Parallel()

	publisher := &recordingGraphProjectionPhasePublisher{}
	writer := &recordingCloudResourceNodeWriter{}
	handler := AzureResourceMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			azureResourceEnvelope(map[string]any{
				"arm_resource_id":        "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
				"normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm",
				"subscription_id":        "sub-1",
				"resource_type":          "microsoft.compute/virtualmachines",
				"location":               "eastus",
			}),
		}},
		NodeWriter:     writer,
		PhasePublisher: publisher,
	}

	result, err := handler.Handle(context.Background(), azureResourceIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if writer.evidenceSource != azureResourceEvidenceSource {
		t.Fatalf("evidenceSource = %q, want %q", writer.evidenceSource, azureResourceEvidenceSource)
	}
	if len(publisher.calls) != 1 || len(publisher.calls[0]) != 1 {
		t.Fatalf("phase publisher calls = %#v, want one readiness row", publisher.calls)
	}
	if got, want := publisher.calls[0][0].Key.AcceptanceUnitID, "azure_resource_materialization:scope-azure-1"; got != want {
		t.Fatalf("acceptance unit = %q, want %q", got, want)
	}
}
