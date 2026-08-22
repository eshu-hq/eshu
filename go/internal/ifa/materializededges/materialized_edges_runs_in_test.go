// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// runsInTestRow builds a minimal RUNS_IN upsert row carrying only the
// payload fields runsInRowsToExpectedEdges reads.
func runsInTestRow(functionID, repoID string) reducer.SharedProjectionIntentRow {
	return reducer.SharedProjectionIntentRow{
		ProjectionDomain: reducer.DomainRunsIn,
		Payload: map[string]any{
			"function_id": functionID,
			"repo_id":     repoID,
		},
	}
}

// TestRunsInRowsToExpectedEdgesFansOutOverMultipleWorkloads is the required
// offline proof of the N-Workload fan-out: the live Cypher's
// (Repository)-[:DEFINES]->(Workload) MATCH carries no LIMIT
// (canonical_runs_in_edges.go), so ONE RUNS_IN intent row -- which has no
// visibility into how many Workloads its repository DEFINES -- produces one
// graph edge PER Workload at write time. This test proves the guard's own
// fan-out reproduces that cross product deterministically: one row, two
// Workloads for its repo, must yield exactly two ExpectedEdges.
func TestRunsInRowsToExpectedEdgesFansOutOverMultipleWorkloads(t *testing.T) {
	t.Parallel()

	rows := []reducer.SharedProjectionIntentRow{
		runsInTestRow("content-entity:fn", "repo-1"),
	}
	workloadIDs := map[string][]string{
		"repo-1": {"workload:api", "workload:worker"},
	}

	edges, unresolved := runsInRowsToExpectedEdges(rows, workloadIDs, "RUNS_IN")
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %v, want none", unresolved)
	}
	if len(edges) != 2 {
		t.Fatalf("expected exactly 2 fanned-out edges for 2 Workloads, got %d: %+v", len(edges), edges)
	}
	want := map[string]bool{
		(ExpectedEdge{RelationshipType: "RUNS_IN", SourceEntityID: "content-entity:fn", TargetEntityID: "workload:api"}).Key():    true,
		(ExpectedEdge{RelationshipType: "RUNS_IN", SourceEntityID: "content-entity:fn", TargetEntityID: "workload:worker"}).Key(): true,
	}
	for _, e := range edges {
		if !want[e.Key()] {
			t.Errorf("unexpected edge %+v", e)
		}
	}
}

// TestRunsInRowsToExpectedEdgesSingleWorkloadYieldsOneEdge pins the
// non-fan-out baseline: a repository that DEFINES exactly one Workload still
// yields exactly one edge (canonical_runs_in_edges.go's own doc comment).
func TestRunsInRowsToExpectedEdgesSingleWorkloadYieldsOneEdge(t *testing.T) {
	t.Parallel()

	rows := []reducer.SharedProjectionIntentRow{
		runsInTestRow("content-entity:fn", "repo-1"),
	}
	workloadIDs := map[string][]string{"repo-1": {"workload:api"}}

	edges, unresolved := runsInRowsToExpectedEdges(rows, workloadIDs, "RUNS_IN")
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %v, want none", unresolved)
	}
	if len(edges) != 1 {
		t.Fatalf("expected exactly 1 edge, got %d: %+v", len(edges), edges)
	}
}

// TestRunsInRowsToExpectedEdgesReportsUnresolvedRepo proves a repository with
// no Workload in the projection is reported as unresolved rather than
// silently dropped.
func TestRunsInRowsToExpectedEdgesReportsUnresolvedRepo(t *testing.T) {
	t.Parallel()

	rows := []reducer.SharedProjectionIntentRow{
		runsInTestRow("content-entity:fn", "repo-1"),
	}
	edges, unresolved := runsInRowsToExpectedEdges(rows, map[string][]string{}, "RUNS_IN")
	if len(edges) != 0 {
		t.Fatalf("expected 0 edges when no Workload resolves, got %d: %+v", len(edges), edges)
	}
	if len(unresolved) != 1 || unresolved[0] != "repo-1" {
		t.Fatalf("unresolved = %v, want exactly [\"repo-1\"]", unresolved)
	}
}
