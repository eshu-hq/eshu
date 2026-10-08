// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"testing"
)

// TestDecodeIncidentWorkItemRecordMapsDecodedFields pins the review read's
// use of the shared work-item decode leaf: a valid record row maps its
// decoded identity through, and a row missing its required anchor is
// dropped. Added for #6623; it fails if the shared DecodeRecord behavior
// mutates.
func TestDecodeIncidentWorkItemRecordMapsDecodedFields(t *testing.T) {
	t.Parallel()

	record, ok := decodeIncidentWorkItemRecord(incidentContextFactRow{
		FactID:        "fact-1",
		SchemaVersion: "1.0.0",
		Payload: map[string]any{
			"provider":              "jira_cloud",
			"provider_work_item_id": "10001",
			"work_item_key":         "OPS-123",
		},
	})
	if !ok {
		t.Fatal("decode valid record row: got ok=false, want decoded row")
	}
	if record.WorkItemKey != "OPS-123" || record.WorkItemID != "10001" {
		t.Fatalf("record identity = %q/%q, want OPS-123/10001", record.WorkItemKey, record.WorkItemID)
	}

	_, ok = decodeIncidentWorkItemRecord(incidentContextFactRow{
		FactID:        "fact-2",
		SchemaVersion: "1.0.0",
		Payload:       map[string]any{"provider": "jira_cloud"},
	})
	if ok {
		t.Fatal("decode record row missing anchors: got ok=true, want drop")
	}
}

// TestDecodeIncidentWorkItemProjectMetadataMapsDecodedFields pins the
// review read's project-metadata path through the shared leaf.
func TestDecodeIncidentWorkItemProjectMetadataMapsDecodedFields(t *testing.T) {
	t.Parallel()

	metadata, ok := decodeIncidentWorkItemProjectMetadata(incidentContextFactRow{
		FactID:        "fact-1",
		SchemaVersion: "1.0.0",
		Payload: map[string]any{
			"provider":   "jira_cloud",
			"project_id": "p1",
		},
	})
	if !ok {
		t.Fatal("decode valid project row: got ok=false, want decoded row")
	}
	if metadata.ProjectID != "p1" {
		t.Fatalf("project id = %q, want p1", metadata.ProjectID)
	}

	_, ok = decodeIncidentWorkItemProjectMetadata(incidentContextFactRow{
		FactID:        "fact-2",
		SchemaVersion: "1.0.0",
		Payload:       map[string]any{},
	})
	if ok {
		t.Fatal("decode project row missing provider: got ok=true, want drop")
	}
}

// TestDecodeIncidentWorkItemStatusMetadataMapsDecodedFields pins the review
// read's status-metadata path through the shared leaf.
func TestDecodeIncidentWorkItemStatusMetadataMapsDecodedFields(t *testing.T) {
	t.Parallel()

	metadata, ok := decodeIncidentWorkItemStatusMetadata(incidentContextFactRow{
		FactID:        "fact-1",
		SchemaVersion: "1.0.0",
		Payload: map[string]any{
			"provider":  "jira_cloud",
			"status_id": "s1",
		},
	})
	if !ok {
		t.Fatal("decode valid status row: got ok=false, want decoded row")
	}
	if metadata.StatusID != "s1" {
		t.Fatalf("status id = %q, want s1", metadata.StatusID)
	}

	_, ok = decodeIncidentWorkItemStatusMetadata(incidentContextFactRow{
		FactID:        "fact-2",
		SchemaVersion: "1.0.0",
		Payload:       map[string]any{"provider": "jira_cloud"},
	})
	if ok {
		t.Fatal("decode status row missing status_id: got ok=true, want drop")
	}
}
