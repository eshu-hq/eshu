// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
)

// countToleranceSnapshot defines one node-count and one edge-count tolerance for
// the full-corpus assertions exercised by #3866 criterion 3.
func countToleranceSnapshot() Snapshot {
	return Snapshot{
		Graph: GraphSnapshot{
			NodeCounts: map[string]CountRange{"Repository": {Min: 15, Max: 30}},
			EdgeCounts: map[string]CountRange{"REPO_CONTAINS": {Min: 100, Max: 20000}},
		},
	}
}

func countFinding(r Report, check string) (Finding, bool) {
	for _, f := range r.Findings {
		if f.Check == check {
			return f, true
		}
	}
	return Finding{}, false
}

// TestCheckGraphCountTolerancesRequiredInFullMode is #3866 criterion 3: in
// full-corpus mode (requiredOnly=false) the node/edge count tolerances must be
// promoted to REQUIRED findings, not advisory. In-range counts still pass, but a
// regression that drops a count out of range now fails the gate (a required
// File-count floor would have caught the #4019 nested-file drop).
func TestCheckGraphCountTolerancesRequiredInFullMode(t *testing.T) {
	t.Parallel()

	c := fakeCounter{
		nodes: map[string]int64{"Repository": 20},
		edges: map[string]int64{"REPO_CONTAINS": 500},
	}
	var r Report
	if err := checkGraph(context.Background(), c, countToleranceSnapshot(), false, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if r.Failed() {
		t.Fatal("in-range count tolerances should pass the gate")
	}
	for _, check := range []string{"node_count_Repository", "edge_count_REPO_CONTAINS"} {
		f, ok := countFinding(r, check)
		if !ok {
			t.Fatalf("missing count finding %q in full mode", check)
		}
		if !f.Required {
			t.Errorf("count finding %q is advisory in full mode, want required (#3866)", check)
		}
	}
}

// TestCheckGraphCountToleranceBelowFloorFailsInFullMode proves the required
// tolerance actually blocks: a count below the snapshot floor fails the gate.
func TestCheckGraphCountToleranceBelowFloorFailsInFullMode(t *testing.T) {
	t.Parallel()

	c := fakeCounter{
		nodes: map[string]int64{"Repository": 2}, // below floor 15
		edges: map[string]int64{"REPO_CONTAINS": 500},
	}
	var r Report
	if err := checkGraph(context.Background(), c, countToleranceSnapshot(), false, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("Repository count below the snapshot floor must fail the gate in full mode (#3866)")
	}
}

// TestCheckGraphCountTolerancesSkippedInRequiredOnlyMode keeps the minimal mode
// behavior: requiredOnly=true skips count tolerances entirely, so an
// out-of-range count does not fail that mode.
func TestCheckGraphCountTolerancesSkippedInRequiredOnlyMode(t *testing.T) {
	t.Parallel()

	c := fakeCounter{
		nodes: map[string]int64{"Repository": 2}, // below floor, but skipped
		edges: map[string]int64{"REPO_CONTAINS": 500},
	}
	var r Report
	if err := checkGraph(context.Background(), c, countToleranceSnapshot(), true, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if _, ok := countFinding(r, "node_count_Repository"); ok {
		t.Fatal("requiredOnly mode must not emit count-tolerance findings")
	}
	if _, ok := countFinding(r, "edge_count_REPO_CONTAINS"); ok {
		t.Fatal("requiredOnly mode must not emit edge count-tolerance findings")
	}
}
