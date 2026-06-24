// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testRelationshipResourceObservation() ResourceObservation {
	return ResourceObservation{
		Name:       "//compute.googleapis.com/projects/my-project/zones/us-central1-a/instances/vm-rel",
		AssetType:  "compute.googleapis.com/Instance",
		State:      "RUNNING",
		Location:   "us-central1-a",
		Ancestors:  []string{"projects/123456789", "organizations/9988776655"},
		UpdateTime: time.Date(2026, 6, 9, 12, 3, 0, 0, time.UTC),
		Relationships: []RelationshipObservation{
			{
				SourceFullResourceName: "//compute.googleapis.com/projects/my-project/zones/us-central1-a/instances/vm-rel",
				SourceAssetType:        "compute.googleapis.com/Instance",
				RelationshipType:       "INSTANCE_TO_DISK",
				TargetFullResourceName: "//compute.googleapis.com/projects/my-project/zones/us-central1-a/disks/disk-rel",
				TargetAssetType:        "compute.googleapis.com/Disk",
				SupportState:           RelationshipSupportSupported,
			},
		},
	}
}

func TestGenerationBuildEmitsRelationshipObservationsForRelatedAssets(t *testing.T) {
	gen := NewGeneration(testGenerationBoundary(), testRedactionKey(t))
	gen.ObserveReadTime(time.Date(2026, 6, 9, 12, 15, 0, 0, time.UTC))

	if err := gen.AddPage([]ResourceObservation{testRelationshipResourceObservation()}); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := countKind(envelopes, facts.GCPCloudResourceFactKind); got != 1 {
		t.Fatalf("resource fact count = %d, want 1", got)
	}
	if got := countKind(envelopes, facts.GCPCloudRelationshipFactKind); got != 1 {
		t.Fatalf("relationship fact count = %d, want 1", got)
	}

	rel := firstFactKind(t, envelopes, facts.GCPCloudRelationshipFactKind)
	if rel.Payload["relationship_type"] != "INSTANCE_TO_DISK" {
		t.Fatalf("relationship_type = %#v, want INSTANCE_TO_DISK", rel.Payload["relationship_type"])
	}
	if rel.Payload["support_state"] != RelationshipSupportSupported {
		t.Fatalf("support_state = %#v, want supported", rel.Payload["support_state"])
	}
	if rel.Payload["read_time"] == nil {
		t.Fatal("read_time missing from relationship observation")
	}
}
