// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBuildWorkItemEvidencePageDerivesTruncationFromFetchedFactsNotDecodedRows
// is the #4733 regression test. Root cause: the store fetched limit+1 facts,
// decoded the WHOLE window, and the caller computed truncated from
// len(decodedRows) > limit. A fact dropped by a failed typed decode INSIDE the
// visible window shrinks the decoded-row count, so a page that genuinely has
// more evidence beyond it could report truncated=false and omit next_cursor —
// evidence beyond the malformed fact becomes undiscoverable. It also let the
// lookahead ("+1") fact leak into the visible page, since the old code only
// trimmed to `limit` when it believed the page was truncated.
//
// This test fetches a window of 4 facts for a requested page size of 3
// (fetchLimit=4): fact-1 (valid), fact-2 (malformed: missing both required
// work_item.record identity fields), fact-3 (valid, the true boundary of the
// visible window), fact-4 (valid, the "+1" lookahead fact beyond the page).
// It asserts:
//   - Truncated is true (4 fetched > 3 requested), even though only 2 of the
//     3 visible-window facts decoded.
//   - NextCursorFactID is "fact-3" — the last FETCHED fact in the visible
//     window — not "fact-1" (the last successfully DECODED row) and not
//     "fact-4" (the lookahead fact, which must never appear as a cursor or a
//     row in this page).
//   - fact-4 never appears in Rows: the lookahead fact must not leak into the
//     visible page just because an earlier fact in the window was dropped.
func TestBuildWorkItemEvidencePageDerivesTruncationFromFetchedFactsNotDecodedRows(t *testing.T) {
	t.Parallel()

	facts := []workItemEvidenceFactRow{
		{
			FactID:        "fact-1",
			FactKind:      "work_item.record",
			SchemaVersion: "1.0.0",
			Payload: map[string]any{
				"provider":              "jira_cloud",
				"provider_work_item_id": "10001",
				"work_item_key":         "OPS-1",
			},
		},
		{
			FactID:        "fact-2",
			FactKind:      "work_item.record",
			SchemaVersion: "1.0.0",
			Payload: map[string]any{
				"provider": "jira_cloud",
				// work_item_key and provider_work_item_id both absent: this
				// fact must dead-letter (drop), never zero-value.
			},
		},
		{
			FactID:        "fact-3",
			FactKind:      "work_item.record",
			SchemaVersion: "1.0.0",
			Payload: map[string]any{
				"provider":              "jira_cloud",
				"provider_work_item_id": "10003",
				"work_item_key":         "OPS-3",
			},
		},
		{
			FactID:        "fact-4",
			FactKind:      "work_item.record",
			SchemaVersion: "1.0.0",
			Payload: map[string]any{
				"provider":              "jira_cloud",
				"provider_work_item_id": "10004",
				"work_item_key":         "OPS-4",
			},
		},
	}

	// fetchLimit=4 means the caller requested a page of 3 (fetchLimit-1) and
	// the store fetched one extra lookahead fact, matching
	// PostgresWorkItemEvidenceStore's "+1" convention.
	page := buildWorkItemEvidencePage(facts, 4)

	if !page.Truncated {
		t.Fatal("Truncated = false, want true (4 facts fetched for a 3-fact page)")
	}
	if got, want := page.NextCursorFactID, "fact-3"; got != want {
		t.Fatalf("NextCursorFactID = %q, want %q (last FETCHED fact in the visible window, not the last decoded row)", got, want)
	}
	if len(page.Rows) != 2 {
		t.Fatalf("len(Rows) = %d, want 2 (fact-1 and fact-3; fact-2 dropped, fact-4 beyond the page): %+v", len(page.Rows), page.Rows)
	}
	for _, row := range page.Rows {
		if row.FactID == "fact-2" {
			t.Fatalf("malformed fact-2 decoded to a row instead of being dropped: %+v", row)
		}
		if row.FactID == "fact-4" {
			t.Fatalf("lookahead fact-4 leaked into the visible page: %+v", row)
		}
	}
}

// TestBuildWorkItemEvidencePageNotTruncatedKeepsAllDecodedRows proves the
// unexceptional case is unaffected: when the fetch does not exceed the
// visible window, every fact is in scope and nothing is trimmed.
func TestBuildWorkItemEvidencePageNotTruncatedKeepsAllDecodedRows(t *testing.T) {
	t.Parallel()

	facts := []workItemEvidenceFactRow{
		{
			FactID:        "fact-1",
			FactKind:      "work_item.record",
			SchemaVersion: "1.0.0",
			Payload: map[string]any{
				"provider":              "jira_cloud",
				"provider_work_item_id": "10001",
				"work_item_key":         "OPS-1",
			},
		},
	}

	page := buildWorkItemEvidencePage(facts, 4)

	if page.Truncated {
		t.Fatal("Truncated = true, want false (only 1 of 3 requested facts exists)")
	}
	if page.NextCursorFactID != "" {
		t.Fatalf("NextCursorFactID = %q, want empty when not truncated", page.NextCursorFactID)
	}
	if len(page.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(page.Rows))
	}
}

// rawFactWorkItemEvidenceStore is a WorkItemEvidenceStore fake that stores raw
// (undecoded) facts and calls the real buildWorkItemEvidencePage, exercising
// the actual production pagination logic end-to-end through the HTTP handler
// rather than a hand-built stand-in.
type rawFactWorkItemEvidenceStore struct {
	facts []workItemEvidenceFactRow
}

func (s *rawFactWorkItemEvidenceStore) ListWorkItemEvidence(
	_ context.Context,
	filter WorkItemEvidenceFilter,
) (WorkItemEvidencePage, error) {
	return buildWorkItemEvidencePage(s.facts, filter.Limit), nil
}

// TestWorkItemListEvidenceHandlerAdvancesPastMalformedFactInsideWindow is the
// #4733 regression test at the HTTP handler level: a malformed fact inside
// the fetch window must not make a genuinely-truncated page report itself
// complete. It exercises the real handler, the real store contract
// (buildWorkItemEvidencePage), and the real typed-decode drop path together.
func TestWorkItemListEvidenceHandlerAdvancesPastMalformedFactInsideWindow(t *testing.T) {
	t.Parallel()

	store := &rawFactWorkItemEvidenceStore{
		facts: []workItemEvidenceFactRow{
			{
				FactID:        "fact-1",
				FactKind:      "work_item.record",
				SchemaVersion: "1.0.0",
				Payload: map[string]any{
					"provider":              "jira_cloud",
					"provider_work_item_id": "10001",
					"work_item_key":         "OPS-1",
				},
			},
			{
				FactID:        "fact-2",
				FactKind:      "work_item.record",
				SchemaVersion: "1.0.0",
				Payload: map[string]any{
					"provider": "jira_cloud",
				},
			},
			{
				FactID:        "fact-3",
				FactKind:      "work_item.record",
				SchemaVersion: "1.0.0",
				Payload: map[string]any{
					"provider":              "jira_cloud",
					"provider_work_item_id": "10003",
					"work_item_key":         "OPS-3",
				},
			},
			{
				FactID:        "fact-4",
				FactKind:      "work_item.record",
				SchemaVersion: "1.0.0",
				Payload: map[string]any{
					"provider":              "jira_cloud",
					"provider_work_item_id": "10004",
					"work_item_key":         "OPS-4",
				},
			},
		},
	}
	handler := &WorkItemHandler{Evidence: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/work-items/evidence?work_item_key=OPS-1&limit=3", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Count      int               `json:"count"`
		Truncated  bool              `json:"truncated"`
		NextCursor map[string]string `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true: a malformed fact inside the window must not hide remaining evidence")
	}
	if got, want := resp.NextCursor["after_fact_id"], "fact-3"; got != want {
		t.Fatalf("next_cursor.after_fact_id = %q, want %q", got, want)
	}
	if got, want := resp.Count, 2; got != want {
		t.Fatalf("count = %d, want %d (fact-1 and fact-3; fact-2 dropped, fact-4 beyond the page)", got, want)
	}
}
