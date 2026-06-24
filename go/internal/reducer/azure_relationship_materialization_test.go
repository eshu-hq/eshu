// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	azureVMID  = "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm"
	azureNICID = "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Network/networkInterfaces/nic"
)

func azureRelationshipEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:   "azure-relationship-fact",
		FactKind: facts.AzureCloudRelationshipFactKind,
		Payload:  payload,
	}
}

func azureVMResource() facts.Envelope {
	return azureResourceEnvelope(map[string]any{
		"arm_resource_id":        azureVMID,
		"normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm",
		"subscription_id":        "sub-1",
		"resource_type":          "microsoft.compute/virtualmachines",
		"resource_name":          "vm",
		"location":               "eastus",
	})
}

func azureNICResource() facts.Envelope {
	return azureResourceEnvelope(map[string]any{
		"arm_resource_id":        azureNICID,
		"normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.network/networkinterfaces/nic",
		"subscription_id":        "sub-1",
		"resource_type":          "microsoft.network/networkinterfaces",
		"resource_name":          "nic",
		"location":               "eastus",
	})
}

func azureManagedBy(supportState string) facts.Envelope {
	return azureRelationshipEnvelope(map[string]any{
		"source_arm_resource_id":        azureVMID,
		"source_normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm",
		"target_arm_resource_id":        azureNICID,
		"target_normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.network/networkinterfaces/nic",
		"relationship_type":             "managed_by",
		"target_resource_type":          "microsoft.network/networkinterfaces",
		"support_state":                 supportState,
	})
}

func azureRelationshipIntent() Intent {
	return Intent{
		IntentID:     "intent-azure-edges-1",
		ScopeID:      "scope-azure-1",
		GenerationID: "gen-azure-1",
		Domain:       DomainAzureRelationshipMaterialization,
		EntityKeys:   []string{"azure_resource_materialization:scope-azure-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func TestAzureRelationshipMaterializationProjectsResolvedManagedByEdge(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AzureRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			azureVMResource(), azureNICResource(), azureManagedBy("supported"),
		}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), azureRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if writer.writeCalls != 1 || len(writer.writtenRows) != 1 {
		t.Fatalf("writeCalls/rows = %d/%d, want 1/1", writer.writeCalls, len(writer.writtenRows))
	}
	if got := anyToString(writer.writtenRows[0]["relationship_type"]); got != "managed_by" {
		t.Fatalf("relationship_type = %q, want managed_by", got)
	}
	if got := anyToString(writer.writtenRows[0]["resolution_mode"]); got != azureJoinModeARMResourceID {
		t.Fatalf("resolution_mode = %q, want %q", got, azureJoinModeARMResourceID)
	}
	if writer.writeEvidence != azureRelationshipEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, azureRelationshipEvidenceSource)
	}
}

func TestAzureRelationshipMaterializationSkipMatrixDoesNotFabricateEdges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		envelopes   []facts.Envelope
		wantSkipped int
	}{
		{
			name:        "missing_target",
			envelopes:   []facts.Envelope{azureVMResource(), azureManagedBy("supported")},
			wantSkipped: 1,
		},
		{
			name: "cross_subscription_target",
			envelopes: []facts.Envelope{azureVMResource(), azureRelationshipEnvelope(map[string]any{
				"source_normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm",
				"target_normalized_resource_id": "/subscriptions/sub-2/resourcegroups/rg/providers/microsoft.network/networkinterfaces/nic",
				"relationship_type":             "managed_by",
				"target_resource_type":          "microsoft.network/networkinterfaces",
				"support_state":                 "supported",
			})},
			wantSkipped: 1,
		},
		{
			name:        "partial",
			envelopes:   []facts.Envelope{azureVMResource(), azureNICResource(), azureManagedBy("partial")},
			wantSkipped: 1,
		},
		{
			name:        "unsupported",
			envelopes:   []facts.Envelope{azureVMResource(), azureNICResource(), azureManagedBy("unsupported")},
			wantSkipped: 1,
		},
		{
			name: "invalid_type",
			envelopes: []facts.Envelope{azureVMResource(), azureNICResource(), azureRelationshipEnvelope(map[string]any{
				"source_normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm",
				"target_normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.network/networkinterfaces/nic",
				"relationship_type":             "bad type`)//",
				"support_state":                 "supported",
			})},
			wantSkipped: 1,
		},
		{
			name: "unsupported_type",
			envelopes: []facts.Envelope{azureVMResource(), azureNICResource(), azureRelationshipEnvelope(map[string]any{
				"source_normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm",
				"target_normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.network/networkinterfaces/nic",
				"relationship_type":             "depends_on",
				"support_state":                 "supported",
			})},
			wantSkipped: 1,
		},
		{
			name: "self_loop",
			envelopes: []facts.Envelope{azureVMResource(), azureRelationshipEnvelope(map[string]any{
				"source_normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm",
				"target_normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm",
				"relationship_type":             "managed_by",
				"support_state":                 "supported",
			})},
			wantSkipped: 1,
		},
		{
			name: "tombstoned_relationship",
			envelopes: []facts.Envelope{azureVMResource(), azureNICResource(), func() facts.Envelope {
				env := azureManagedBy("supported")
				env.IsTombstone = true
				return env
			}()},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			writer := &recordingCloudResourceEdgeWriter{}
			handler := AzureRelationshipMaterializationHandler{
				FactLoader:           &stubFactLoader{envelopes: tt.envelopes},
				EdgeWriter:           writer,
				ReadinessLookup:      readyLookup(true, true),
				PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
			}
			result, err := handler.Handle(context.Background(), azureRelationshipIntent())
			if err != nil {
				t.Fatalf("Handle returned error: %v", err)
			}
			if writer.writeCalls != 0 {
				t.Fatalf("writeCalls = %d, want 0", writer.writeCalls)
			}
			if tt.wantSkipped > 0 && !strings.Contains(result.EvidenceSummary, "1 skipped") {
				t.Fatalf("EvidenceSummary = %q, want skip accounting", result.EvidenceSummary)
			}
		})
	}
}

func TestAzureRelationshipMaterializationRetractsPriorGenerationEdges(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AzureRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			azureVMResource(), azureNICResource(), azureManagedBy("supported"),
		}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), azureRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1", writer.retractCalls)
	}
	if writer.retractEvidence != azureRelationshipEvidenceSource {
		t.Fatalf("retract evidence = %q, want %q", writer.retractEvidence, azureRelationshipEvidenceSource)
	}
	if len(writer.retractScopeIDs) != 1 || writer.retractScopeIDs[0] != "scope-azure-1" {
		t.Fatalf("retract scope ids = %v, want [scope-azure-1]", writer.retractScopeIDs)
	}
}

func TestAzureRelationshipMaterializationEmptyGenerationIsNoOp(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AzureRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), azureRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0", writer.writeCalls)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
}

func TestAzureRelationshipMaterializationGatesOnCanonicalNodesPhase(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AzureRelationshipMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		EdgeWriter:      writer,
		ReadinessLookup: readyLookup(false, false),
	}

	_, err := handler.Handle(context.Background(), azureRelationshipIntent())
	if err == nil {
		t.Fatal("expected a retryable readiness error")
	}
	if !IsRetryable(err) {
		t.Fatalf("readiness error must be retryable, got %v", err)
	}
	if writer.writeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("writes before readiness: write=%d retract=%d", writer.writeCalls, writer.retractCalls)
	}
}
