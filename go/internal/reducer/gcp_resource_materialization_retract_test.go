// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestGCPResourceMaterializationRetractsDeletedUIDs is the #6887 GCP
// copy-paste guard: the predecessor-only uid reaches the retracter with the
// GCP evidence source (not a sibling family's kinds, extractor, or source).
func TestGCPResourceMaterializationRetractsDeletedUIDs(t *testing.T) {
	t.Parallel()

	kept := gcpResourceEnvelope(map[string]any{
		"full_resource_name": "//compute.googleapis.com/projects/demo-proj/zones/us-central1-a/instances/kept",
		"asset_type":         "compute.googleapis.com/Instance",
		"project_id":         "demo-proj",
		"location":           "us-central1-a",
		"asset_type_family":  "compute",
		"display_name":       "kept",
		"state":              "RUNNING",
	})
	deleted := gcpResourceEnvelope(map[string]any{
		"full_resource_name": "//compute.googleapis.com/projects/demo-proj/zones/us-central1-a/instances/deleted",
		"asset_type":         "compute.googleapis.com/Instance",
		"project_id":         "demo-proj",
		"location":           "us-central1-a",
		"asset_type_family":  "compute",
		"display_name":       "deleted",
		"state":              "RUNNING",
	})
	loader := &generationScopedFactLoader{byGeneration: map[string][]facts.Envelope{
		"gen-2": {kept},
		"gen-1": {kept, deleted},
	}}
	retracter := &recordingCloudResourceNodeRetracter{retracted: 1}

	handler := GCPResourceMaterializationHandler{
		FactLoader:      loader,
		NodeWriter:      &recordingCloudResourceNodeWriter{},
		NodeRetracter:   retracter,
		PriorGeneration: func(context.Context, string, string) (string, bool, error) { return "gen-1", true, nil },
	}
	intent := Intent{
		IntentID:     "intent-gcp-retract",
		ScopeID:      "scope-gcp-1",
		GenerationID: "gen-2",
		Domain:       DomainGCPResourceMaterialization,
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
	wantDeleted := cloudResourceUID("demo-proj", "us-central1-a", "compute.googleapis.com/Instance",
		"//compute.googleapis.com/projects/demo-proj/zones/us-central1-a/instances/deleted")
	if len(retracter.uids) != 1 || retracter.uids[0] != wantDeleted {
		t.Fatalf("retract uids = %v, want exactly [%q]", retracter.uids, wantDeleted)
	}
	if retracter.evidenceSource != gcpResourceEvidenceSource {
		t.Fatalf("evidenceSource = %q, want %q", retracter.evidenceSource, gcpResourceEvidenceSource)
	}
}
