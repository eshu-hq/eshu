// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"
)

// TestBuildWorkItemEvidenceRowsDropsRecordMissingRequiredAnchor proves the
// accuracy guarantee Contract System v1 exists to enforce for this family's
// query-layer decode site: a work_item.record fact whose payload is missing
// its required identity anchor (provider_work_item_id or work_item_key) is
// classified input_invalid and DROPPED from the evidence list rather than
// producing a row with an empty-string identity. A valid sibling fact in the
// same batch still lists normally, proving the drop is per-fact, not
// whole-batch.
func TestBuildWorkItemEvidenceRowsDropsRecordMissingRequiredAnchor(t *testing.T) {
	t.Parallel()

	facts := []workItemEvidenceFactRow{
		{
			FactID:           "malformed-record",
			FactKind:         "work_item.record",
			ScopeID:          "jira:site:example",
			GenerationID:     "generation-1",
			SourceConfidence: "reported",
			ObservedAt:       "2026-06-01T12:00:00Z",
			SchemaVersion:    "1.0.0",
			Payload: map[string]any{
				"provider": "jira_cloud",
				// work_item_key and provider_work_item_id are both absent: a
				// malformed fact that must dead-letter, never decode to an
				// empty-identity row.
			},
		},
		{
			FactID:           "valid-record",
			FactKind:         "work_item.record",
			ScopeID:          "jira:site:example",
			GenerationID:     "generation-1",
			SourceConfidence: "reported",
			ObservedAt:       "2026-06-01T12:05:00Z",
			SchemaVersion:    "1.0.0",
			Payload: map[string]any{
				"provider":              "jira_cloud",
				"provider_work_item_id": "10001",
				"work_item_key":         "OPS-123",
				"status_name":           "In Progress",
			},
		},
	}

	rows := buildWorkItemEvidenceRows(facts)

	if len(rows) != 1 {
		t.Fatalf("buildWorkItemEvidenceRows() returned %d rows, want 1 (malformed record dropped): %+v", len(rows), rows)
	}
	if got := rows[0].FactID; got != "valid-record" {
		t.Fatalf("surviving row FactID = %q, want %q", got, "valid-record")
	}
	if got := rows[0].WorkItemKey; got != "OPS-123" {
		t.Fatalf("surviving row WorkItemKey = %q, want %q", got, "OPS-123")
	}
	if got := rows[0].ProviderWorkItemID; got != "10001" {
		t.Fatalf("surviving row ProviderWorkItemID = %q, want %q", got, "10001")
	}

	// Prove the malformed fact never appears with an empty-string identity —
	// the exact accuracy hole this test guards against.
	for _, row := range rows {
		if row.FactID == "malformed-record" {
			t.Fatalf("malformed-record decoded to a row instead of being dropped: %+v", row)
		}
	}
}

// TestBuildWorkItemEvidenceRowsKeepsValidSiblingsWhenOneKindMalforms proves
// the drop is scoped to the one malformed fact kind's row, not the whole
// batch: an external_link fact (which requires only "provider") survives
// alongside a dropped malformed record.
func TestBuildWorkItemEvidenceRowsKeepsValidSiblingsWhenOneKindMalforms(t *testing.T) {
	t.Parallel()

	facts := []workItemEvidenceFactRow{
		{
			FactID:        "malformed-record",
			FactKind:      "work_item.record",
			ScopeID:       "jira:site:example",
			GenerationID:  "generation-1",
			SchemaVersion: "1.0.0",
			Payload: map[string]any{
				"provider": "jira_cloud",
			},
		},
		{
			FactID:        "valid-link",
			FactKind:      "work_item.external_link",
			ScopeID:       "jira:site:example",
			GenerationID:  "generation-1",
			SchemaVersion: "1.0.0",
			Payload: map[string]any{
				"provider":               "jira_cloud",
				"work_item_key":          "OPS-123",
				"provider_support_state": "supported_provider",
			},
		},
	}

	rows := buildWorkItemEvidenceRows(facts)
	if len(rows) != 1 {
		t.Fatalf("buildWorkItemEvidenceRows() returned %d rows, want 1: %+v", len(rows), rows)
	}
	if got := rows[0].FactID; got != "valid-link" {
		t.Fatalf("surviving row FactID = %q, want %q", got, "valid-link")
	}
}
