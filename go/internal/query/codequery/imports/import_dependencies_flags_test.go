// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imports_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// edgeFlags is the value of the three IMPORTS edge flag columns for one fixture
// row. A nil field models a graph property that was never written: the reader
// projects rel.<flag> raw, so an edge written before the flags existed comes
// back with the key present and a null value.
type edgeFlags struct{ typeOnly, deferred, inferred any }

var (
	runtimeFlags  = edgeFlags{false, false, false}
	typeOnlyFlags = edgeFlags{true, false, false}
	deferredFlags = edgeFlags{false, true, false}
	inferredFlags = edgeFlags{false, false, true}
	unknownFlags  = edgeFlags{nil, nil, nil}
)

// flaggedCycleEdge builds one file_import_cycles edge row carrying the flag
// columns the reader now projects.
func flaggedCycleEdge(file, targetModule string, flags edgeFlags) map[string]any {
	row := importDependencyCycleProofEdge(file+".py", file+".py", targetModule, 1)
	row["type_only"] = flags.typeOnly
	row["deferred"] = flags.deferred
	row["inferred"] = flags.inferred
	return row
}

// cycleFlagCoverage reads coverage.cycle_edge_flags out of a response.
func cycleFlagCoverage(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	coverage, ok := resp["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage = %#v, want a map", resp["coverage"])
	}
	flags, ok := coverage["cycle_edge_flags"].(map[string]any)
	if !ok {
		t.Fatalf("coverage.cycle_edge_flags = %#v, want a map: flag exclusions and unknown edges must be disclosed", coverage["cycle_edge_flags"])
	}
	return flags
}

// TestCycleFlagsExcludeTypeOnlyAndDeferredEdges is the #6851 deferred negative:
// a cycle closed only through an import that never runs at load time is not a
// load-time cycle, and the response says how many edges were left out.
func TestCycleFlagsExcludeTypeOnlyAndDeferredEdges(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name         string
		closing      edgeFlags
		wantTypeOnly int
		wantDeferred int
	}{
		{name: "closed only through a type-only import", closing: typeOnlyFlags, wantTypeOnly: 1},
		{name: "closed only through a deferred import", closing: deferredFlags, wantDeferred: 1},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			edges := []map[string]any{
				flaggedCycleEdge("a", "b", runtimeFlags),
				flaggedCycleEdge("b", "a", tt.closing),
			}
			req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 10}
			_, resp := buildCycleResponse(t, req, edges)

			if cycles, _ := resp["cycles"].([]map[string]any); len(cycles) != 0 {
				t.Fatalf("len(cycles) = %d, want 0: %+v", len(cycles), cycles)
			}
			flags := cycleFlagCoverage(t, resp)
			if got := flags["type_only_excluded"]; got != tt.wantTypeOnly {
				t.Errorf("type_only_excluded = %#v, want %d", got, tt.wantTypeOnly)
			}
			if got := flags["deferred_excluded"]; got != tt.wantDeferred {
				t.Errorf("deferred_excluded = %#v, want %d", got, tt.wantDeferred)
			}
			if got := flags["edges_considered"]; got != 2 {
				t.Errorf("edges_considered = %#v, want 2 (counted before exclusion)", got)
			}
		})
	}
}

// TestCycleFlagsLabelEachCycleByItsWeakestEdge pins the label rule: a cycle is
// ambiguous when any edge is inferred, else flags_unknown when any edge has no
// flag properties, else runtime. A null flag is never runtime.
func TestCycleFlagsLabelEachCycleByItsWeakestEdge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		first, back edgeFlags
		wantLabel   string
		wantStates  [2]string
	}{
		{name: "all explicit false is runtime", first: runtimeFlags, back: runtimeFlags, wantLabel: "runtime", wantStates: [2]string{"runtime", "runtime"}},
		{name: "one inferred edge is ambiguous", first: runtimeFlags, back: inferredFlags, wantLabel: "ambiguous", wantStates: [2]string{"runtime", "inferred"}},
		{name: "one edge with null flags is flags_unknown", first: runtimeFlags, back: unknownFlags, wantLabel: "flags_unknown", wantStates: [2]string{"runtime", "unknown"}},
		{name: "a partly written flag set is unknown, not runtime", first: runtimeFlags, back: edgeFlags{false, nil, false}, wantLabel: "flags_unknown", wantStates: [2]string{"runtime", "unknown"}},
		{name: "inferred beside unknown is ambiguous", first: inferredFlags, back: unknownFlags, wantLabel: "ambiguous", wantStates: [2]string{"inferred", "unknown"}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			edges := []map[string]any{
				flaggedCycleEdge("a", "b", tt.first),
				flaggedCycleEdge("b", "a", tt.back),
			}
			req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 10}
			_, resp := buildCycleResponse(t, req, edges)

			cycles, _ := resp["cycles"].([]map[string]any)
			if len(cycles) != 1 {
				t.Fatalf("len(cycles) = %d, want 1: %+v", len(cycles), cycles)
			}
			if got := cycles[0]["cycle_label"]; got != tt.wantLabel {
				t.Fatalf("cycle_label = %#v, want %q", got, tt.wantLabel)
			}
			edgesOut, _ := cycles[0]["cycle_edges"].([]map[string]any)
			if len(edgesOut) != 2 {
				t.Fatalf("len(cycle_edges) = %d, want 2", len(edgesOut))
			}
			// cycle_edges follows the normalized path: a->b then b->a.
			for i, want := range tt.wantStates {
				if got := edgesOut[i]["flag_state"]; got != want {
					t.Errorf("cycle_edges[%d].flag_state = %#v, want %q", i, got, want)
				}
			}
		})
	}
}

// TestCycleFlagsKeepAnAlternateRuntimePath proves excluding a type-only edge
// removes only the cycles that needed it: the runtime path through another file
// still closes a cycle.
func TestCycleFlagsKeepAnAlternateRuntimePath(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		flaggedCycleEdge("a", "b", runtimeFlags),
		flaggedCycleEdge("b", "a", typeOnlyFlags), // the a<->b cycle needs this and is gone
		flaggedCycleEdge("a", "c", runtimeFlags),
		flaggedCycleEdge("c", "a", runtimeFlags), // the a<->c cycle survives
	}
	req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 10}
	_, resp := buildCycleResponse(t, req, edges)

	cycles, _ := resp["cycles"].([]map[string]any)
	if len(cycles) != 1 {
		t.Fatalf("len(cycles) = %d, want only the runtime a<->c cycle: %+v", len(cycles), cycles)
	}
	if got := cycleStrings(t, cycles[0]["cycle_path"]); len(got) != 3 || got[0] != "a.py" || got[1] != "c.py" || got[2] != "a.py" {
		t.Fatalf("cycle_path = %v, want a.py -> c.py -> a.py", got)
	}
	if got := cycleFlagCoverage(t, resp)["type_only_excluded"]; got != 1 {
		t.Errorf("type_only_excluded = %#v, want 1", got)
	}
}

// TestCycleFlagsFoldDuplicateEdgeRows covers rows that share one edge key: the
// reader matches Module by name while the writer keys it on (name, lang), so two
// graph edges can project as one (file, module) row pair. The fold keeps the
// edge as strong as its strongest proof: any runtime row wins, then unknown, then
// inferred; only when every row is type-only or deferred is the edge excluded.
func TestCycleFlagsFoldDuplicateEdgeRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		duplicates []edgeFlags
		wantLabel  string // empty means the edge is excluded and no cycle closes
	}{
		{name: "runtime beside type-only stays runtime", duplicates: []edgeFlags{typeOnlyFlags, runtimeFlags}, wantLabel: "runtime"},
		{name: "runtime beside unknown stays runtime", duplicates: []edgeFlags{unknownFlags, runtimeFlags}, wantLabel: "runtime"},
		{name: "unknown beside inferred is unknown", duplicates: []edgeFlags{inferredFlags, unknownFlags}, wantLabel: "flags_unknown"},
		{name: "inferred beside type-only is inferred", duplicates: []edgeFlags{typeOnlyFlags, inferredFlags}, wantLabel: "ambiguous"},
		{name: "type-only beside deferred is excluded", duplicates: []edgeFlags{typeOnlyFlags, deferredFlags}, wantLabel: ""},
		{name: "every row type-only is excluded", duplicates: []edgeFlags{typeOnlyFlags, typeOnlyFlags}, wantLabel: ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			edges := []map[string]any{flaggedCycleEdge("b", "a", runtimeFlags)}
			for _, flags := range tt.duplicates {
				edges = append(edges, flaggedCycleEdge("a", "b", flags))
			}
			req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 10}
			_, resp := buildCycleResponse(t, req, edges)

			cycles, _ := resp["cycles"].([]map[string]any)
			if tt.wantLabel == "" {
				if len(cycles) != 0 {
					t.Fatalf("len(cycles) = %d, want 0 for an excluded edge: %+v", len(cycles), cycles)
				}
				return
			}
			if len(cycles) != 1 {
				t.Fatalf("len(cycles) = %d, want 1: %+v", len(cycles), cycles)
			}
			if got := cycles[0]["cycle_label"]; got != tt.wantLabel {
				t.Fatalf("cycle_label = %#v, want %q", got, tt.wantLabel)
			}
		})
	}
}

// TestCycleFlagCoverageCountsEveryEdgeClass proves the disclosure adds up: every
// deduplicated edge is counted once in edges_considered and once in the class it
// landed in, so a client can tell how much of a result rests on unknown or
// inferred edges.
func TestCycleFlagCoverageCountsEveryEdgeClass(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		flaggedCycleEdge("a", "b", runtimeFlags),
		flaggedCycleEdge("b", "a", inferredFlags),
		flaggedCycleEdge("c", "d", unknownFlags),
		flaggedCycleEdge("d", "c", unknownFlags),
		flaggedCycleEdge("e", "f", typeOnlyFlags),
		flaggedCycleEdge("g", "h", deferredFlags),
	}
	req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 10}
	_, resp := buildCycleResponse(t, req, edges)

	flags := cycleFlagCoverage(t, resp)
	want := map[string]int{
		"edges_considered":   6,
		"type_only_excluded": 1,
		"deferred_excluded":  1,
		"inferred":           1,
		"flags_unknown":      2,
	}
	for key, count := range want {
		if got := flags[key]; got != count {
			t.Errorf("cycle_edge_flags.%s = %#v, want %d", key, got, count)
		}
	}
}
