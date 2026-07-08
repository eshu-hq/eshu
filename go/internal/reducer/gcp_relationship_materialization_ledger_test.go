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
	gcpInstance2FullName = "//compute.googleapis.com/projects/demo-proj/zones/us-central1-a/instances/app2"
	gcpDisk2FullName     = "//compute.googleapis.com/projects/demo-proj/zones/us-central1-a/disks/data2"
)

func gcpInstance2Resource() facts.Envelope {
	return gcpResourceEnvelope(map[string]any{
		"full_resource_name": gcpInstance2FullName,
		"asset_type":         "compute.googleapis.com/Instance",
		"project_id":         "demo-proj",
		"location":           "us-central1-a",
	})
}

func gcpDisk2Resource() facts.Envelope {
	return gcpResourceEnvelope(map[string]any{
		"full_resource_name": gcpDisk2FullName,
		"asset_type":         "compute.googleapis.com/Disk",
		"project_id":         "demo-proj",
		"location":           "us-central1-a",
	})
}

func gcpInstance2ToDisk2(supportState string) facts.Envelope {
	return gcpRelationshipEnvelope(map[string]any{
		"source_full_resource_name": gcpInstance2FullName,
		"target_full_resource_name": gcpDisk2FullName,
		"relationship_type":         "INSTANCE_TO_DISK",
		"target_asset_type":         "compute.googleapis.com/Disk",
		"support_state":             supportState,
	})
}

// TestGCPRelationshipMaterializationLedgerRecordsBeforeWrite proves the
// handler records the ledger BEFORE writing graph edges when a Ledger is
// wired.
func TestGCPRelationshipMaterializationLedgerRecordsBeforeWrite(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	ledger := &fakeProjectedSourceLedger{}
	handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(), gcpDiskResource(), gcpInstanceToDisk("supported"),
		}},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), gcpRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if ledger.recordCalls != 1 {
		t.Fatalf("record calls = %d, want 1", ledger.recordCalls)
	}
	wantUID := cloudResourceUID("demo-proj", "us-central1-a", "compute.googleapis.com/Instance", gcpInstanceFullName)
	if len(ledger.recordedUIDs) != 1 || ledger.recordedUIDs[0] != wantUID {
		t.Fatalf("recorded uids = %v, want [%s]", ledger.recordedUIDs, wantUID)
	}
	if ledger.recordedSource != gcpRelationshipEvidenceSource {
		t.Fatalf("recorded evidence source = %q, want %q", ledger.recordedSource, gcpRelationshipEvidenceSource)
	}
	if len(ledger.callOrder) < 3 || ledger.callOrder[0] != "list" || ledger.callOrder[1] != "prune" || ledger.callOrder[2] != "record" {
		t.Fatalf("call order = %v, want [list prune record]", ledger.callOrder)
	}
}

// TestGCPRelationshipMaterializationLedgerRetractUsesLedgerUIDs proves retract
// enumerates uids from the ledger and calls the anchored-delete method.
func TestGCPRelationshipMaterializationLedgerRetractUsesLedgerUIDs(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	ledger := &fakeProjectedSourceLedger{listUIDs: []string{"uid-1", "uid-2"}}
	handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(), gcpDiskResource(), gcpInstanceToDisk("supported"),
		}},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), gcpRelationshipIntent()); err != nil {
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

// TestGCPRelationshipMaterializationNilLedgerPreservesOldRetractPath proves
// the pre-ledger whole-scope retract still runs when Ledger is nil.
func TestGCPRelationshipMaterializationNilLedgerPreservesOldRetractPath(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(), gcpDiskResource(), gcpInstanceToDisk("supported"),
		}},
		EdgeWriter:           writer,
		Ledger:               nil,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), gcpRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("old retract calls = %d, want 1 when Ledger is nil", writer.retractCalls)
	}
	if writer.retractByUIDsCalls != 0 {
		t.Fatalf("retractByUIDs calls = %d, want 0 when Ledger is nil", writer.retractByUIDsCalls)
	}
}

// TestGCPRelationshipMaterializationLedgerLeakSafetyAcrossGenerations proves
// generation N+1's retract anchors on the ledger's full prior source set
// (instance, instance2), not just the current generation's resolved set
// (instance), when instance2's relationship disappears between generations.
func TestGCPRelationshipMaterializationLedgerLeakSafetyAcrossGenerations(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	ledger := newStatefulProjectedSourceLedger()

	gen1Handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(), gcpDiskResource(), gcpInstanceToDisk("supported"),
			gcpInstance2Resource(), gcpDisk2Resource(), gcpInstance2ToDisk2("supported"),
		}},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	gen1Intent := gcpRelationshipIntent()
	gen1Intent.GenerationID = "gen-1"
	if _, err := gen1Handler.Handle(context.Background(), gen1Intent); err != nil {
		t.Fatalf("gen1 Handle returned error: %v", err)
	}
	if writer.writeCalls != 1 || len(writer.writtenRows) != 2 {
		t.Fatalf("gen1 write calls = %d rows = %d, want 1 call / 2 rows", writer.writeCalls, len(writer.writtenRows))
	}

	// Generation 2: instance2's relationship is gone.
	gen2Handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(), gcpDiskResource(), gcpInstanceToDisk("supported"),
		}},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	gen2Intent := gcpRelationshipIntent()
	gen2Intent.GenerationID = "gen-2"
	if _, err := gen2Handler.Handle(context.Background(), gen2Intent); err != nil {
		t.Fatalf("gen2 Handle returned error: %v", err)
	}

	uidInstance1 := cloudResourceUID("demo-proj", "us-central1-a", "compute.googleapis.com/Instance", gcpInstanceFullName)
	uidInstance2 := cloudResourceUID("demo-proj", "us-central1-a", "compute.googleapis.com/Instance", gcpInstance2FullName)

	if writer.retractByUIDsCalls != 1 {
		t.Fatalf("retractByUIDs calls = %d, want 1", writer.retractByUIDsCalls)
	}
	gotUIDs := append([]string(nil), writer.retractByUIDsUids...)
	sort.Strings(gotUIDs)
	wantUIDs := []string{uidInstance1, uidInstance2}
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
