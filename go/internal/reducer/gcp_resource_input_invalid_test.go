// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestGCPResourceMaterializationQuarantinesMissingFullResourceName is the
// flagship regression test for the gcp family's typed-decode migration
// (Contract System v1 §3.2, mirroring
// TestAWSRelationshipMaterializationQuarantinesMissingAccountID). It proves the
// accuracy guarantee the migration exists to protect AND the per-fact isolation
// contract: a gcp_cloud_resource fact missing its required full_resource_name
// key is QUARANTINED as a visible input_invalid dead-letter — never silently
// producing an empty-string CloudResource uid — while every VALID fact in the
// same batch still projects and the handler succeeds so one malformed fact
// never stalls the scope generation.
//
// Before the migration this behavior was impossible: gcpCloudResourceNodeRow
// read full_resource_name with payloadString, which returns "" for the absent
// key, and cloudResourceUID(projectID, location, assetType, "") yielded a
// wrong-but-plausible identity that would silently materialize a corrupt node.
//
// After the migration ExtractGCPCloudResourceNodeRows decodes each
// gcp_cloud_resource fact through factschema.DecodeGCPCloudResource; the
// malformed fact yields a classified *factschema.DecodeError that
// partitionDecodeFailures routes to a per-fact quarantine. The handler records
// it (metric + structured log + the input_invalid_facts SubSignal) and
// continues, so the batch's valid resource still materializes its node.
func TestGCPResourceMaterializationQuarantinesMissingFullResourceName(t *testing.T) {
	t.Parallel()

	// A resource fact whose required full_resource_name key is ABSENT (not
	// merely empty): the exact malformed input the accuracy guarantee names.
	// Everything else is present so the ONLY reason to quarantine the fact is
	// the missing required field.
	malformed := gcpResourceEnvelope(map[string]any{
		// "full_resource_name" intentionally absent.
		"asset_type": "compute.googleapis.com/Instance",
		"project_id": "demo-proj",
		"location":   "us-central1-a",
	})
	// A fully valid, independent resource that must still project despite the
	// malformed fact sharing the batch. This is the isolation half of the
	// contract: valid facts are unaffected by a poisoned sibling.
	valid := gcpResourceEnvelope(map[string]any{
		"full_resource_name": "//compute.googleapis.com/projects/demo-proj/zones/us-central1-a/instances/good",
		"asset_type":         "compute.googleapis.com/Instance",
		"project_id":         "demo-proj",
		"location":           "us-central1-a",
	})

	writer := &recordingCloudResourceNodeWriter{}
	handler := GCPResourceMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{malformed, valid}},
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainGCPResourceMaterialization,
	})
	// Per-fact isolation: the malformed fact does NOT fail the whole intent.
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed gcp_cloud_resource fact must be quarantined per-fact, not fail the whole intent", err)
	}

	// The malformed fact must be counted as an input_invalid quarantine in the
	// Result SubSignals so the operator sees it on the per-intent signal (each
	// quarantined fact is also on the eshu_dp_reducer_input_invalid_facts_total
	// counter and a structured error log).
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-full_resource_name fact must be recorded as one input_invalid quarantine", got)
	}

	// The batch's VALID resource must still materialize its node: isolation
	// means a poisoned sibling never suppresses valid graph truth.
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1; the valid resource must still project despite the quarantined fact", writer.calls)
	}
	if len(writer.rows) != 1 {
		t.Fatalf("len(writer.rows) = %d, want 1; exactly the one valid node must be written", len(writer.rows))
	}

	// No node may be written under a uid computed with an empty-string
	// full_resource_name segment — the accuracy guarantee.
	emptyIdentityUID := cloudResourceUID("demo-proj", "us-central1-a", "compute.googleapis.com/Instance", "")
	for _, row := range writer.rows {
		if row["uid"] == emptyIdentityUID {
			t.Fatalf("written node references the empty-identity uid %q; a quarantined fact must never produce graph identity", emptyIdentityUID)
		}
	}
}
