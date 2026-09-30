// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// The docstring budget fixtures reproduce the #7234 shape: the source_cache
// clip (#7171) bounds each row's body, but the row shape echoes the docstring
// into metadata, semantic_summary, semantic_profile, javascript_semantics and
// story, so a repository with long docstrings still overflowed the default
// page. Only a read-time bound on the docstring (and therefore on every echo
// derived from it) keeps the default-args reply within budget.
const (
	docstringBudgetLen  = 16 * 1024
	docstringBudgetClip = 512
	// docstringBudgetMarker starts every fixture docstring so a test can tell a
	// clipped prefix from the full text.
	docstringBudgetMarker = "Handles the request lifecycle."
)

func newDocstringBudgetStore() *sourceCacheBudgetStore {
	body := docstringBudgetMarker + " " + strings.Repeat("d", docstringBudgetLen)
	entities := make([]querycontract.EntityContent, 0, sourceCacheBudgetRows)
	for index := 0; index < sourceCacheBudgetRows; index++ {
		entities = append(entities, querycontract.EntityContent{
			EntityID:     fmt.Sprintf("entity-%03d", index),
			RepoID:       sourceCacheBudgetRepoID,
			RelativePath: fmt.Sprintf("src/handler%03d.ts", index),
			EntityType:   "Function",
			EntityName:   "handleRequest",
			StartLine:    10,
			EndLine:      40,
			Language:     "typescript",
			SourceCache:  strings.Repeat("a", sourceCacheBudgetTypicalLen),
			Metadata:     map[string]any{"docstring": body},
		})
	}
	return newSourceCacheStore(entities)
}

// TestDocstringToolsDefaultResponseStaysWithinBudget dispatches the three
// row-returning tools with default arguments over rows that carry a docstring
// far larger than any page can repeat five times, and requires the reply to fit
// the response budget, carry the clip on the response and on every row, and
// never contain the full docstring.
func TestDocstringToolsDefaultResponseStaysWithinBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tool string
		args map[string]any
		rows int
	}{
		{tool: "find_symbol", args: map[string]any{"symbol": "handleRequest", "repo_id": sourceCacheBudgetRepoID}, rows: 20},
		{tool: "inspect_code_inventory", args: map[string]any{"repo_id": sourceCacheBudgetRepoID, "inventory_kind": "entity", "entity_kind": "function"}, rows: 20},
		{tool: "search_entity_content", args: map[string]any{"query": "handleRequest", "repo_id": sourceCacheBudgetRepoID}, rows: 10},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			t.Parallel()

			result := requireDefaultResponseWithinBudget(t, tt.tool, sourceCacheBudgetMux(newDocstringBudgetStore()), tt.args)
			data, ok := result.Envelope.Data.(map[string]any)
			if !ok {
				t.Fatalf("%s data type = %T, want map[string]any", tt.tool, result.Envelope.Data)
			}
			rows, _ := data["results"].([]any)
			if len(rows) != tt.rows {
				t.Fatalf("%s returned %d rows, want %d default rows", tt.tool, len(rows), tt.rows)
			}
			if got := numberValue(data["docstring_clip_bytes"]); got != docstringBudgetClip {
				t.Fatalf("%s docstring_clip_bytes = %v, want %d", tt.tool, data["docstring_clip_bytes"], docstringBudgetClip)
			}
			if got := numberValue(data["docstring_clipped_rows"]); got != tt.rows {
				t.Fatalf("%s docstring_clipped_rows = %v, want %d (every row is over the clip)", tt.tool, data["docstring_clipped_rows"], tt.rows)
			}
			first, _ := rows[0].(map[string]any)
			if first["docstring_clipped"] != true {
				t.Fatalf("%s first row docstring_clipped = %v, want true", tt.tool, first["docstring_clipped"])
			}
			if got, want := numberValue(first["docstring_total_bytes"]), len(docstringBudgetMarker)+1+docstringBudgetLen; got != want {
				t.Fatalf("%s first row docstring_total_bytes = %v, want %d", tt.tool, first["docstring_total_bytes"], want)
			}
			encoded, err := json.Marshal(first)
			if err != nil {
				t.Fatalf("%s marshal first row: %v", tt.tool, err)
			}
			if !strings.Contains(string(encoded), docstringBudgetMarker) {
				t.Fatalf("%s first row lost the docstring prefix %q", tt.tool, docstringBudgetMarker)
			}
			if strings.Contains(string(encoded), strings.Repeat("d", docstringBudgetClip+1)) {
				t.Fatalf("%s first row still carries more than %d docstring bytes in one echo", tt.tool, docstringBudgetClip)
			}
		})
	}
}
