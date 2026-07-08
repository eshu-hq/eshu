// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	azureVM2ID  = "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm2"
	azureNIC2ID = "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Network/networkInterfaces/nic2"
)

func azureVM2Resource() facts.Envelope {
	return azureResourceEnvelope(map[string]any{
		"arm_resource_id":        azureVM2ID,
		"normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm2",
		"subscription_id":        "sub-1",
		"resource_type":          "microsoft.compute/virtualmachines",
		"resource_name":          "vm2",
		"location":               "eastus",
	})
}

func azureNIC2Resource() facts.Envelope {
	return azureResourceEnvelope(map[string]any{
		"arm_resource_id":        azureNIC2ID,
		"normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.network/networkinterfaces/nic2",
		"subscription_id":        "sub-1",
		"resource_type":          "microsoft.network/networkinterfaces",
		"resource_name":          "nic2",
		"location":               "eastus",
	})
}

func azureManagedBy2(supportState string) facts.Envelope {
	return azureRelationshipEnvelope(map[string]any{
		"source_arm_resource_id":        azureVM2ID,
		"source_normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm2",
		"target_arm_resource_id":        azureNIC2ID,
		"target_normalized_resource_id": "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.network/networkinterfaces/nic2",
		"relationship_type":             "managed_by",
		"target_resource_type":          "microsoft.network/networkinterfaces",
		"support_state":                 supportState,
	})
}

// TestAzureRelationshipMaterializationLedgerRecordsBeforeWrite proves the
// handler records the ledger BEFORE writing graph edges when a Ledger is
// wired.
func TestAzureRelationshipMaterializationLedgerRecordsBeforeWrite(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	ledger := &fakeProjectedSourceLedger{}
	handler := AzureRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			azureVMResource(), azureNICResource(), azureManagedBy("supported"),
		}},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), azureRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if ledger.recordCalls != 1 {
		t.Fatalf("record calls = %d, want 1", ledger.recordCalls)
	}
	wantUID := cloudResourceUID("sub-1", "eastus", "microsoft.compute/virtualmachines",
		"/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm")
	if len(ledger.recordedUIDs) != 1 || ledger.recordedUIDs[0] != wantUID {
		t.Fatalf("recorded uids = %v, want [%s]", ledger.recordedUIDs, wantUID)
	}
	if ledger.recordedSource != azureRelationshipEvidenceSource {
		t.Fatalf("recorded evidence source = %q, want %q", ledger.recordedSource, azureRelationshipEvidenceSource)
	}
	if len(ledger.callOrder) < 3 || ledger.callOrder[0] != "list" || ledger.callOrder[1] != "prune" || ledger.callOrder[2] != "record" {
		t.Fatalf("call order = %v, want [list prune record]", ledger.callOrder)
	}
}

// TestAzureRelationshipMaterializationLedgerRetractUsesLedgerUIDs proves
// retract enumerates uids from the ledger and calls the anchored-delete method.
func TestAzureRelationshipMaterializationLedgerRetractUsesLedgerUIDs(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	ledger := &fakeProjectedSourceLedger{listUIDs: []string{"uid-1", "uid-2"}}
	handler := AzureRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			azureVMResource(), azureNICResource(), azureManagedBy("supported"),
		}},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), azureRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractByUIDsCalls != 1 {
		t.Fatalf("retractByUIDs calls = %d, want 1", writer.retractByUIDsCalls)
	}
	if len(writer.retractByUIDsUids) != 2 {
		t.Fatalf("retractByUIDs uids = %v, want 2", writer.retractByUIDsUids)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("old whole-scope retract calls = %d, want 0 when a ledger is wired", writer.retractCalls)
	}
}

// TestAzureRelationshipMaterializationNilLedgerPreservesOldRetractPath proves
// the pre-ledger whole-scope retract still runs when Ledger is nil.
func TestAzureRelationshipMaterializationNilLedgerPreservesOldRetractPath(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AzureRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			azureVMResource(), azureNICResource(), azureManagedBy("supported"),
		}},
		EdgeWriter:           writer,
		Ledger:               nil,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), azureRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("old retract calls = %d, want 1 when Ledger is nil", writer.retractCalls)
	}
	if writer.retractByUIDsCalls != 0 {
		t.Fatalf("retractByUIDs calls = %d, want 0 when Ledger is nil", writer.retractByUIDsCalls)
	}
}

// TestAzureRelationshipMaterializationLedgerLeakSafetyAcrossGenerations proves
// generation N+1's retract anchors on the ledger's full prior source set
// (VM1, VM2), not just the current generation's resolved set (VM1), when VM2's
// managed_by relationship disappears between generations.
func TestAzureRelationshipMaterializationLedgerLeakSafetyAcrossGenerations(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	ledger := newStatefulProjectedSourceLedger()

	gen1Handler := AzureRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			azureVMResource(), azureNICResource(), azureManagedBy("supported"),
			azureVM2Resource(), azureNIC2Resource(), azureManagedBy2("supported"),
		}},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	gen1Intent := azureRelationshipIntent()
	gen1Intent.GenerationID = "gen-1"
	if _, err := gen1Handler.Handle(context.Background(), gen1Intent); err != nil {
		t.Fatalf("gen1 Handle returned error: %v", err)
	}
	if writer.writeCalls != 1 || len(writer.writtenRows) != 2 {
		t.Fatalf("gen1 write calls = %d rows = %d, want 1 call / 2 rows", writer.writeCalls, len(writer.writtenRows))
	}

	// Generation 2: VM2's managed_by relationship is gone.
	gen2Handler := AzureRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			azureVMResource(), azureNICResource(), azureManagedBy("supported"),
		}},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	gen2Intent := azureRelationshipIntent()
	gen2Intent.GenerationID = "gen-2"
	if _, err := gen2Handler.Handle(context.Background(), gen2Intent); err != nil {
		t.Fatalf("gen2 Handle returned error: %v", err)
	}

	uidVM1 := cloudResourceUID("sub-1", "eastus", "microsoft.compute/virtualmachines",
		"/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm")
	uidVM2 := cloudResourceUID("sub-1", "eastus", "microsoft.compute/virtualmachines",
		"/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm2")

	if writer.retractByUIDsCalls != 1 {
		t.Fatalf("retractByUIDs calls = %d, want 1", writer.retractByUIDsCalls)
	}
	gotUIDs := append([]string(nil), writer.retractByUIDsUids...)
	sort.Strings(gotUIDs)
	wantUIDs := []string{uidVM1, uidVM2}
	sort.Strings(wantUIDs)
	if len(gotUIDs) != len(wantUIDs) {
		t.Fatalf("retract anchored on %v, want the full PRIOR ledger set %v", gotUIDs, wantUIDs)
	}
	for i := range gotUIDs {
		if gotUIDs[i] != wantUIDs[i] {
			t.Fatalf("retract anchored on %v, want the full PRIOR ledger set %v", gotUIDs, wantUIDs)
		}
	}
}
