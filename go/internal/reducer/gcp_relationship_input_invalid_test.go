// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestGCPRelationshipMaterializationQuarantinesMissingRelationshipType is the
// gcp_cloud_relationship counterpart to
// TestGCPResourceMaterializationQuarantinesMissingFullResourceName. It proves a
// gcp_cloud_relationship fact missing its required relationship_type key is
// QUARANTINED as a visible input_invalid dead-letter rather than silently
// skipped with no operator signal, while a valid, independent relationship in
// the same batch still materializes its edge.
func TestGCPRelationshipMaterializationQuarantinesMissingRelationshipType(t *testing.T) {
	t.Parallel()

	// A relationship fact whose required relationship_type key is ABSENT (not
	// merely empty).
	malformed := gcpRelationshipEnvelope(map[string]any{
		"source_full_resource_name": gcpInstanceFullName,
		"target_full_resource_name": gcpDiskFullName,
		// "relationship_type" intentionally absent.
		"target_asset_type": "compute.googleapis.com/Disk",
		"support_state":     "supported",
	})

	writer := &recordingCloudResourceEdgeWriter{}
	handler := GCPRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			gcpInstanceResource(), gcpDiskResource(), malformed, gcpInstanceToDisk("supported"),
		}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), gcpRelationshipIntent())
	// Per-fact isolation: the malformed fact does NOT fail the whole intent.
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed gcp_cloud_relationship fact must be quarantined per-fact, not fail the whole intent", err)
	}

	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-relationship_type fact must be recorded as one input_invalid quarantine", got)
	}

	// The batch's VALID relationship (instance -> disk, "supported") must still
	// materialize its edge: isolation means a poisoned sibling never suppresses
	// valid graph truth.
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1; the valid relationship must still project despite the quarantined fact", writer.writeCalls)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written edge rows = %d, want 1", len(writer.writtenRows))
	}
	if got := anyToString(writer.writtenRows[0]["relationship_type"]); got != "INSTANCE_TO_DISK" {
		t.Fatalf("relationship_type = %q, want INSTANCE_TO_DISK", got)
	}
}
