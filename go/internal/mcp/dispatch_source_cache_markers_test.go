// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// sourceCacheMarkerEntities returns three rows in one page: a small body that
// must pass through untouched, a body one byte over the clip, and a multi-byte
// body whose clip boundary falls inside a code point.
func sourceCacheMarkerEntities() []querycontract.EntityContent {
	row := func(id, body string) querycontract.EntityContent {
		return querycontract.EntityContent{
			EntityID:     id,
			RepoID:       sourceCacheBudgetRepoID,
			RelativePath: "src/" + id + ".ts",
			EntityType:   "Function",
			EntityName:   "handleRequest",
			StartLine:    3,
			EndLine:      9,
			Language:     "typescript",
			SourceCache:  body,
		}
	}
	return []querycontract.EntityContent{
		row("small", "function small() {}"),
		row("just-over", strings.Repeat("a", querycontract.SourceCacheClipBytes+1)),
		// "é" is two bytes, so 3000 of them (6000 B) cannot be cut at byte 4096
		// without landing mid-rune unless the cut steps back to 4096 exactly;
		// the leading "x" shifts every rune boundary to an odd offset.
		row("multibyte", "x"+strings.Repeat("é", 3000)),
	}
}

// TestSourceCacheToolsClipRowsWithSparseMarkers dispatches each row-returning
// source tool over small, just-over, and multi-byte bodies and checks the clip:
// the small row is untouched and carries no markers, clipped rows carry the row
// markers, every clipped body is valid UTF-8 within the ceiling, the stored
// length is reported, the response counts the clipped rows, and the
// source_handle drill-down is present on every row.
func TestSourceCacheToolsClipRowsWithSparseMarkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tool string
		args map[string]any
	}{
		{tool: "find_symbol", args: map[string]any{"symbol": "handleRequest", "repo_id": sourceCacheBudgetRepoID}},
		{tool: "inspect_code_inventory", args: map[string]any{"repo_id": sourceCacheBudgetRepoID, "inventory_kind": "entity", "entity_kind": "function"}},
		{tool: "search_entity_content", args: map[string]any{"query": "handleRequest", "repo_id": sourceCacheBudgetRepoID}},
		{tool: "find_code", args: map[string]any{"query": "handleRequest", "repo_id": sourceCacheBudgetRepoID}},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			t.Parallel()

			result := requireDefaultResponseWithinBudget(t, tt.tool, sourceCacheBudgetMux(newSourceCacheStore(sourceCacheMarkerEntities())), tt.args)
			data, _ := result.Envelope.Data.(map[string]any)
			if got := numberValue(data["source_cache_clip_bytes"]); got != 4096 {
				t.Fatalf("source_cache_clip_bytes = %v, want 4096", data["source_cache_clip_bytes"])
			}
			if got := numberValue(data["source_cache_clipped_rows"]); got != 2 {
				t.Fatalf("source_cache_clipped_rows = %v, want 2", data["source_cache_clipped_rows"])
			}
			rows, _ := data["results"].([]any)
			byID := map[string]map[string]any{}
			for _, raw := range rows {
				row, _ := raw.(map[string]any)
				byID[row["entity_id"].(string)] = row
			}
			if len(byID) != 3 {
				t.Fatalf("rows by entity_id = %d, want 3", len(byID))
			}

			small := byID["small"]
			if small["source_cache"] != "function small() {}" {
				t.Fatalf("small row source_cache = %q, want the stored body", small["source_cache"])
			}
			for _, key := range []string{"source_cache_clipped", "source_cache_clip_bytes", "source_cache_total_bytes"} {
				if _, present := small[key]; present {
					t.Fatalf("small row carries %s, want row markers only on clipped rows", key)
				}
			}

			justOver := byID["just-over"]
			if got := len(justOver["source_cache"].(string)); got != 4096 {
				t.Fatalf("just-over source_cache = %d bytes, want 4096", got)
			}
			if got := numberValue(justOver["source_cache_total_bytes"]); got != 4097 {
				t.Fatalf("just-over source_cache_total_bytes = %v, want 4097", justOver["source_cache_total_bytes"])
			}

			multi := byID["multibyte"]
			body := multi["source_cache"].(string)
			if !utf8.ValidString(body) || len(body) > 4096 || strings.ContainsRune(body, utf8.RuneError) {
				t.Fatalf("multibyte source_cache invalid or over ceiling: valid=%v len=%d", utf8.ValidString(body), len(body))
			}
			// "x" + 2047 two-byte runes = 4095 bytes; the 2048th rune would end at
			// 4097, so the cut must step back rather than split it.
			if got, want := len(body), 4095; got != want {
				t.Fatalf("multibyte source_cache = %d bytes, want %d", got, want)
			}
			if got := numberValue(multi["source_cache_total_bytes"]); got != 6001 {
				t.Fatalf("multibyte source_cache_total_bytes = %v, want 6001", multi["source_cache_total_bytes"])
			}

			for id, row := range byID {
				if row["source_cache_clipped"] == true && row["source_cache_clip_bytes"] == nil {
					t.Fatalf("clipped row %s lacks source_cache_clip_bytes", id)
				}
				handle, ok := row["source_handle"].(map[string]any)
				if tt.tool == "find_code" {
					continue // find_code rows predate source_handle; the contract is markers only.
				}
				if !ok || handle["repo_id"] != sourceCacheBudgetRepoID || handle["file_path"] == nil ||
					numberValue(handle["start_line"]) != 3 || numberValue(handle["end_line"]) != 9 {
					t.Fatalf("row %s source_handle = %#v, want {repo_id,file_path,start_line,end_line}", id, row["source_handle"])
				}
				if row["entity_id"] != id {
					t.Fatalf("row %s entity_id = %v", id, row["entity_id"])
				}
			}
		})
	}
}
