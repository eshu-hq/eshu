// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// TestCrossRepoDeadCodeClipsDocstringsWithinBudget is the #7234 sibling proof
// for find_cross_repo_dead_code. Its rows come from the same builder as
// investigate_dead_code, so each carries the docstring in metadata,
// semantic_summary, semantic_profile, the language block, and story. With a
// 16 KiB docstring on every row the default reply must fit the response budget,
// carry the clip markers on the response and on every row, and never contain
// the full docstring.
func TestCrossRepoDeadCodeClipsDocstringsWithinBudget(t *testing.T) {
	t.Parallel()

	store := newDeadCodeBudgetStore(2)
	long := docstringBudgetMarker + " " + strings.Repeat("d", docstringBudgetLen)
	for id, entity := range store.Entities {
		entity.Metadata["docstring"] = long
		store.Entities[id] = entity
	}

	result, err := dispatchTool(context.Background(), deadCodeBudgetMux(store), "find_cross_repo_dead_code",
		map[string]any{"repo_id": deadCodeBudgetProducerRepoID}, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || result == nil || result.Envelope == nil {
		t.Fatalf("dispatchTool() = %#v, %v; want a canonical result", result, err)
	}
	if result.IsError {
		t.Fatalf("default-args reply is an error %#v, want a success within budget", result.Envelope.Error)
	}
	size := estimateResponseBytes(result)
	t.Logf("find_cross_repo_dead_code 16 KiB docstring: response_bytes=%d budget=%d resource_only=%v", size, defaultToolResponseByteBudget, result.ResourceOnly)
	if size > defaultToolResponseByteBudget {
		t.Fatalf("response = %d bytes, want <= %d", size, defaultToolResponseByteBudget)
	}

	data, _ := result.Envelope.Data.(map[string]any)
	if got := numberValue(data["docstring_clip_bytes"]); got != docstringBudgetClip {
		t.Fatalf("docstring_clip_bytes = %v, want %d", data["docstring_clip_bytes"], docstringBudgetClip)
	}
	buckets, _ := data["candidate_buckets"].(map[string]any)
	rows := 0
	for _, name := range []string{"dead", "live_by_consumer", "unknown", "suppressed"} {
		list, _ := buckets[name].([]any)
		for _, raw := range list {
			row, _ := raw.(map[string]any)
			rows++
			if row["docstring_clipped"] != true {
				t.Fatalf("%s row %v docstring_clipped = %v, want true", name, row["entity_id"], row["docstring_clipped"])
			}
			encoded, _ := json.Marshal(row)
			if strings.Contains(string(encoded), strings.Repeat("d", docstringBudgetClip+1)) {
				t.Fatalf("%s row %v still carries more than %d docstring bytes in one echo", name, row["entity_id"], docstringBudgetClip)
			}
		}
	}
	if rows == 0 {
		t.Fatal("reply carried no rows, so the test proves nothing")
	}
	if suppressed, _ := buckets["suppressed"].([]any); len(suppressed) == 0 {
		t.Fatal("suppressed bucket is empty, so the test no longer proves the clip reaches it")
	}
	if got := numberValue(data["docstring_clipped_rows"]); got != rows {
		t.Fatalf("docstring_clipped_rows = %v, want %d (every row, suppressed included)", data["docstring_clipped_rows"], rows)
	}
}
