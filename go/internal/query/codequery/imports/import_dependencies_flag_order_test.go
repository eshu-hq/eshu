// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imports_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// flaggedCycleEdgeAt is flaggedCycleEdge with an explicit line number, so a
// test can make the row that wins on line differ from the row that wins on
// proof.
func flaggedCycleEdgeAt(file, targetModule string, flags edgeFlags, line int) map[string]any {
	row := flaggedCycleEdge(file, targetModule, flags)
	row["line_number"] = line
	return row
}

// cycleEdgeFrom returns the cycle_edges item whose source is sourceFile from the
// first cycle of a response.
func cycleEdgeFrom(t *testing.T, resp map[string]any, sourceFile string) map[string]any {
	t.Helper()
	cycles, _ := resp["cycles"].([]map[string]any)
	if len(cycles) != 1 {
		t.Fatalf("len(cycles) = %d, want 1: %+v", len(cycles), cycles)
	}
	items, _ := cycles[0]["cycle_edges"].([]map[string]any)
	for _, item := range items {
		if item["source_file"] == sourceFile {
			return item
		}
	}
	t.Fatalf("no cycle_edges item from %q in %+v", sourceFile, items)
	return nil
}

// TestCycleFlagsFoldDoesNotDependOnRowOrder pins the fold as a maximum over a
// total order. Each pair is fed in both orders and the rows sit on different
// lines, so a "last row wins" fold, a "first row wins" fold, and a fold that
// takes the state from the earliest-line row all fail one order.
func TestCycleFlagsFoldDoesNotDependOnRowOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		strong    edgeFlags // the row the fold must keep, on line 9
		weak      edgeFlags // the weaker row, on line 1
		wantState string
		wantLabel string
	}{
		{name: "runtime over type-only", strong: runtimeFlags, weak: typeOnlyFlags, wantState: "runtime", wantLabel: "runtime"},
		{name: "unknown over inferred", strong: unknownFlags, weak: inferredFlags, wantState: "unknown", wantLabel: "flags_unknown"},
		{name: "inferred over deferred", strong: inferredFlags, weak: deferredFlags, wantState: "inferred", wantLabel: "ambiguous"},
	}
	for _, tt := range tests {
		tt := tt
		for _, strongFirst := range []bool{true, false} {
			strongFirst := strongFirst
			name := tt.name + " strong row last"
			if strongFirst {
				name = tt.name + " strong row first"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				strongRow := flaggedCycleEdgeAt("a", "b", tt.strong, 9)
				weakRow := flaggedCycleEdgeAt("a", "b", tt.weak, 1)
				edges := []map[string]any{flaggedCycleEdge("b", "a", runtimeFlags)}
				if strongFirst {
					edges = append(edges, strongRow, weakRow)
				} else {
					edges = append(edges, weakRow, strongRow)
				}
				req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 10}
				_, resp := buildCycleResponse(t, req, edges)

				edge := cycleEdgeFrom(t, resp, "a.py")
				if got := edge["flag_state"]; got != tt.wantState {
					t.Errorf("flag_state = %#v, want %q", got, tt.wantState)
				}
				// The reported line must belong to a row of the surviving
				// state, not to a weaker duplicate that happened to sit earlier.
				if got := edge["line_number"]; got != 9 {
					t.Errorf("line_number = %#v, want 9 (the surviving row's line)", got)
				}
				cycles, _ := resp["cycles"].([]map[string]any)
				if got := cycles[0]["cycle_label"]; got != tt.wantLabel {
					t.Errorf("cycle_label = %#v, want %q", got, tt.wantLabel)
				}
			})
		}
	}
}

// TestCycleFlagsHopCollapsePrefersTheStrongerProof covers several edges feeding
// one collapsed file-to-file hop. The rows differ only in source_path, so the
// edge dedupe keeps both and they collapse onto one hop. The hop is as certain
// as its best proof (runtime, then inferred, then unknown) and that decision
// is made before the line-number preference, so the weaker row sits on the
// earlier line to catch a hop that prefers lines first.
func TestCycleFlagsHopCollapsePrefersTheStrongerProof(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		strong    edgeFlags
		weak      edgeFlags
		wantState string
		wantLabel string
	}{
		{name: "runtime over unknown", strong: runtimeFlags, weak: unknownFlags, wantState: "runtime", wantLabel: "runtime"},
		{name: "inferred over unknown", strong: inferredFlags, weak: unknownFlags, wantState: "inferred", wantLabel: "ambiguous"},
		{name: "runtime over inferred", strong: runtimeFlags, weak: inferredFlags, wantState: "runtime", wantLabel: "runtime"},
	}
	for _, tt := range tests {
		tt := tt
		for _, strongFirst := range []bool{true, false} {
			strongFirst := strongFirst
			name := tt.name + " strong row last"
			if strongFirst {
				name = tt.name + " strong row first"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				strongRow := flaggedCycleEdgeAt("a", "b", tt.strong, 9)
				weakRow := flaggedCycleEdgeAt("a", "b", tt.weak, 1)
				weakRow["source_path"] = "/other/a.py"
				edges := []map[string]any{flaggedCycleEdge("b", "a", runtimeFlags)}
				if strongFirst {
					edges = append(edges, strongRow, weakRow)
				} else {
					edges = append(edges, weakRow, strongRow)
				}
				req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 10}
				_, resp := buildCycleResponse(t, req, edges)

				edge := cycleEdgeFrom(t, resp, "a.py")
				if got := edge["flag_state"]; got != tt.wantState {
					t.Errorf("flag_state = %#v, want %q", got, tt.wantState)
				}
				if got := edge["line_number"]; got != 9 {
					t.Errorf("line_number = %#v, want 9 (the stronger edge's line)", got)
				}
				cycles, _ := resp["cycles"].([]map[string]any)
				if got := cycles[0]["cycle_label"]; got != tt.wantLabel {
					t.Errorf("cycle_label = %#v, want %q", got, tt.wantLabel)
				}
			})
		}
	}
}
