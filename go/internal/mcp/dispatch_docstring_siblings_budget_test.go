// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// The sibling budget fixtures extend the #7234 docstring fixture to the graph-
// backed tools that attach the same docstring-derived echoes: the dead-code and
// complexity lists. Measured before the clip, a 16 KiB docstring on every row
// made find_dead_code, investigate_dead_code, and find_most_complex_functions
// return mcp_response_over_budget at their default limits, because each row
// carries the docstring in metadata, semantic_summary, semantic_profile, the
// language block, and story. calculate_cyclomatic_complexity returns one row
// and stays under the budget, so it is asserted for the budget only.

// siblingDocstringRows is the row count the graph fake serves per candidate
// scan: more than any default page, so the page trim is what bounds the reply.
const siblingDocstringRows = 40

func siblingDocstringRow(index int, doc string) map[string]any {
	return map[string]any{
		"entity_id": fmt.Sprintf("fn-%03d", index), "id": fmt.Sprintf("fn-%03d", index),
		"name": fmt.Sprintf("handleRequest%03d", index), "labels": []any{"Function"},
		"file_path": fmt.Sprintf("src/handler%03d.js", index), "repo_id": "repo-1", "repo_name": "repo-1",
		"language": "javascript", "start_line": int64(10), "end_line": int64(40),
		"docstring": doc, "method_kind": "getter", "complexity": int64(90 - index),
		"outgoing_count": int64(1), "incoming_count": int64(0), "total_relationships": int64(1),
	}
}

// siblingDocstringMux serves docstring-bearing graph rows through the real
// code handler for the dead-code candidate scan, the complexity list, and the
// single-entity complexity lookup. Every other graph read returns no rows.
func siblingDocstringMux() http.Handler {
	return siblingDocstringMuxFor(siblingDocstringRows, nil)
}

// siblingDocstringMuxFor is siblingDocstringMux with a candidate-scan row count
// and an optional per-row edit, used to route rows into the suppressed bucket.
func siblingDocstringMuxFor(candidates int, edit func(index int, row map[string]any)) http.Handler {
	doc := docstringBudgetMarker + " " + strings.Repeat("d", docstringBudgetLen)
	mux := http.NewServeMux()
	(&codequery.CodeHandler{
		Neo4j: graph.FakeGraphReader{
			RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				skip, _ := params["skip"].(int)
				switch {
				case strings.Contains(cypher, "cyclomatic_complexity") && strings.Contains(cypher, "ORDER BY complexity DESC"):
					return siblingDocstringRowsFor(siblingDocstringRows, doc), nil
				case strings.Contains(cypher, "cyclomatic_complexity") && strings.Contains(cypher, "outgoing_count"):
					return []map[string]any{siblingDocstringRow(0, doc)}, nil
				case strings.Contains(cypher, "(e:Function)") && strings.Contains(cypher, "SKIP $skip") && skip == 0:
					rows := siblingDocstringRowsFor(candidates, doc)
					if edit != nil {
						for index, row := range rows {
							edit(index, row)
						}
					}
					return rows, nil
				}
				return nil, nil
			},
		},
	}).Mount(mux)
	return mux
}

func siblingDocstringRowsFor(count int, doc string) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for index := 0; index < count; index++ {
		rows = append(rows, siblingDocstringRow(index, doc))
	}
	return rows
}

// siblingRows returns the result rows of a sibling tool reply: the flat
// results list, or the three investigation buckets.
func siblingRows(t *testing.T, data map[string]any) []map[string]any {
	t.Helper()

	var rows []map[string]any
	appendRows := func(raw any) {
		list, _ := raw.([]any)
		for _, item := range list {
			if row, ok := item.(map[string]any); ok {
				rows = append(rows, row)
			}
		}
	}
	appendRows(data["results"])
	if buckets, ok := data["candidate_buckets"].(map[string]any); ok {
		for _, bucket := range []string{"cleanup_ready", "ambiguous", "suppressed"} {
			appendRows(buckets[bucket])
		}
	}
	return rows
}

// TestDocstringSiblingToolsDefaultResponseStaysWithinBudget dispatches the
// dead-code and complexity list tools with default arguments over rows that
// carry a docstring far larger than a page can repeat five times, and requires
// the reply to fit the response budget, carry the clip on the response and on
// every row, and never contain the full docstring.
func TestDocstringSiblingToolsDefaultResponseStaysWithinBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tool string
		args map[string]any
		// minRows is the fewest rows the default limit must return.
		minRows int
	}{
		{tool: "find_dead_code", args: map[string]any{"repo_id": "repo-1"}, minRows: 25},
		{tool: "investigate_dead_code", args: map[string]any{"repo_id": "repo-1"}, minRows: 1},
		{tool: "find_most_complex_functions", args: map[string]any{"repo_id": "repo-1"}, minRows: 10},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			t.Parallel()

			result := requireDefaultResponseWithinBudget(t, tt.tool, siblingDocstringMux(), tt.args)
			data, ok := result.Envelope.Data.(map[string]any)
			if !ok {
				t.Fatalf("%s data type = %T, want map[string]any", tt.tool, result.Envelope.Data)
			}
			rows := siblingRows(t, data)
			t.Logf("%s returned %d rows, docstring_clipped_rows=%v", tt.tool, len(rows), data["docstring_clipped_rows"])
			if len(rows) < tt.minRows {
				t.Fatalf("%s returned %d rows, want at least %d", tt.tool, len(rows), tt.minRows)
			}
			if got := numberValue(data["docstring_clip_bytes"]); got != docstringBudgetClip {
				t.Fatalf("%s docstring_clip_bytes = %v, want %d", tt.tool, data["docstring_clip_bytes"], docstringBudgetClip)
			}
			if got := numberValue(data["docstring_clipped_rows"]); got != len(rows) {
				t.Fatalf("%s docstring_clipped_rows = %v, want %d (every row is over the clip)", tt.tool, data["docstring_clipped_rows"], len(rows))
			}
			for _, row := range rows {
				if row["docstring_clipped"] != true {
					t.Fatalf("%s row %v docstring_clipped = %v, want true", tt.tool, row["entity_id"], row["docstring_clipped"])
				}
				if got, want := numberValue(row["docstring_total_bytes"]), len(docstringBudgetMarker)+1+docstringBudgetLen; got != want {
					t.Fatalf("%s row %v docstring_total_bytes = %v, want %d", tt.tool, row["entity_id"], row["docstring_total_bytes"], want)
				}
			}
			encoded, err := json.Marshal(rows[0])
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

// TestCyclomaticComplexityStaysWithinBudgetOverALongDocstring pins the
// measurement that left calculate_cyclomatic_complexity unclipped: one row with
// a 16 KiB docstring fits the budget by the dispatcher's own accounting.
func TestCyclomaticComplexityStaysWithinBudgetOverALongDocstring(t *testing.T) {
	t.Parallel()

	requireDefaultResponseWithinBudget(t, "calculate_cyclomatic_complexity", siblingDocstringMux(),
		map[string]any{"repo_id": "repo-1", "entity_id": "fn-000"})
}

// TestInvestigateDeadCodeFullSuppressedBucketStaysWithinBudget covers the
// investigation reply at its widest: a full page of active rows plus a full
// suppressed bucket (min(limit, 50) rows, #7168), each with a 16 KiB docstring.
// Measured at 50 rows the reply is 222,078 bytes by the dispatcher's own
// accounting, 84.7% of the budget, and it fits only through the resource-only
// fallback; without the clip it is far over. The clip must reach the suppressed
// bucket as well as the active ones.
func TestInvestigateDeadCodeFullSuppressedBucketStaysWithinBudget(t *testing.T) {
	t.Parallel()

	mux := siblingDocstringMuxFor(60, func(index int, row map[string]any) {
		if index%2 == 0 {
			row["decorators"] = []any{"@cached"}
		}
	})
	result := requireDefaultResponseWithinBudget(t, "investigate_dead_code", mux,
		map[string]any{"repo_id": "repo-1", "exclude_decorated_with": []any{"@cached"}})
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", result.Envelope.Data)
	}
	buckets, _ := data["candidate_buckets"].(map[string]any)
	count := func(name string) int {
		list, _ := buckets[name].([]any)
		return len(list)
	}
	active, suppressed := count("cleanup_ready")+count("ambiguous"), count("suppressed")
	if active != 25 || suppressed != 25 {
		t.Fatalf("buckets active=%d suppressed=%d, want a full page of each (25 and 25)", active, suppressed)
	}
	if got := numberValue(data["docstring_clipped_rows"]); got != active+suppressed {
		t.Fatalf("docstring_clipped_rows = %v, want %d (every row, suppressed bucket included)", data["docstring_clipped_rows"], active+suppressed)
	}
}
