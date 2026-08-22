// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
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
// offline proof of the N-Workload fan-out the write TEMPLATE permits: the
// live Cypher's (Repository)-[:DEFINES]->(Workload) MATCH carries no LIMIT
// (go/internal/storage/cypher/canonical_runs_in_edges.go:26), so ONE RUNS_IN
// intent row -- which has no visibility into how many Workloads its
// repository DEFINES -- WOULD produce one graph edge PER Workload at write
// time if a repository ever had more than one. This test proves the guard's
// own fan-out reproduces that cross product deterministically: one row, two
// Workloads for its repo, must yield exactly two ExpectedEdges.
//
// It has to be synthetic (hand-built workloadIDs, not a live cassette):
// reducer.ExtractWorkloadCandidates aggregates workload signals per repo_id
// alone and reducer.BuildProjectionRows emits exactly one WorkloadRow per
// candidate (go/internal/reducer/candidate_loader.go,
// go/internal/reducer/projection.go:259-301), so today's reducer candidate
// path cannot hand one repository more than one Workload -- no live fixture
// can currently exhibit the shape this test defends against. That does not
// make the test unnecessary: the no-LIMIT MATCH is a real property of the
// write template regardless of what the candidate path can produce today, so
// this guard still needs to prove it derives fan-out from the real
// projection rather than assuming 1-to-1, and this is the only way to
// exercise that logic against N>1 until the candidate path can produce it.
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

// TestResolveRunsInMaterializedEdgesRejectsAnExtraEdge drives the REAL
// resolveRunsInMaterializedEdges entry point (not runsInRowsToExpectedEdges
// in isolation) over the cataloged symbol-runtime Odù. Shrinking the fixture
// below what the extractor/projection actually produces must surface the
// surplus real edge as EXTRA, mirroring TestShellExecFamilyExpectedSetRejectsAnExtraEdge.
func TestResolveRunsInMaterializedEdgesRejectsAnExtraEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	expected, err := LoadExpectedEdges(runsInExpectedEdgesPath(repoRoot), runsInFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	path := writeSymbolRuntimeExpectedEdgesFixture(t, expected[:len(expected)-1])

	ok, detail := resolveRunsInMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a short expected set; an edge nobody derived went unreported")
	}
	if !strings.Contains(detail, "EXTRA") {
		t.Errorf("detail = %q, want it to name the EXTRA edge", detail)
	}
}

// TestResolveRunsInMaterializedEdgesRejectsAMissingEdge pads the real
// fixture with a fabricated edge the extractor/projection does not produce,
// driving the real entry point. The fixture over-claiming an edge must
// surface it as MISSING, mirroring TestShellExecFamilyExpectedSetRejectsAMissingEdge.
func TestResolveRunsInMaterializedEdgesRejectsAMissingEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	expected, err := LoadExpectedEdges(runsInExpectedEdgesPath(repoRoot), runsInFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	padded := append(append([]ExpectedEdge{}, expected...), ExpectedEdge{
		RelationshipType: "RUNS_IN",
		SourceEntityID:   ifa.SymbolRuntimeFamilyHandlerFunctionUID,
		TargetEntityID:   "workload:nonexistent",
	})
	path := writeSymbolRuntimeExpectedEdgesFixture(t, padded)

	ok, detail := resolveRunsInMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against an expectation the extractor/projection does not satisfy")
	}
	if !strings.Contains(detail, "MISSING") {
		t.Errorf("detail = %q, want it to name the MISSING edge", detail)
	}
}

// TestResolveRunsInMaterializedEdgesRejectsWrongSourceEntityID proves the
// guard fails when a fixture's source_entity_id drifts from the real
// canonical Function uid by a single hex character -- exactly what a wrong
// content.CanonicalEntityID derivation looks like in production, otherwise
// silent live (the source-side MATCH finds nothing, the MERGE no-ops, no
// dead letter is raised).
func TestResolveRunsInMaterializedEdgesRejectsWrongSourceEntityID(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	expected, err := LoadExpectedEdges(runsInExpectedEdgesPath(repoRoot), runsInFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	corrupted := append([]ExpectedEdge{}, expected...)
	realSource := corrupted[0].SourceEntityID
	wrongSource := flipOneHexChar(t, realSource)
	corrupted[0].SourceEntityID = wrongSource
	path := writeSymbolRuntimeExpectedEdgesFixture(t, corrupted)

	ok, detail := resolveRunsInMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a fixture whose source_entity_id does not match the real canonical Function uid")
	}
	if !strings.Contains(detail, "MISSING") || !strings.Contains(detail, "EXTRA") {
		t.Errorf("detail = %q, want it to name both the MISSING (corrupted) edge and the EXTRA (real) edge", detail)
	}
	if !strings.Contains(detail, wrongSource) {
		t.Errorf("detail = %q, want it to name the corrupted source id %q as MISSING", detail, wrongSource)
	}
	if !strings.Contains(detail, realSource) {
		t.Errorf("detail = %q, want it to name the real source id %q as EXTRA", detail, realSource)
	}
}

// TestResolveRunsInMaterializedEdgesRejectsWrongTargetEntityID mirrors the
// source-id test for the Workload target id: a wrong workload id must fail
// the same way, naming both the bogus expectation and the real edge the
// projection actually produced. Unlike HANDLES_ROUTE's Endpoint hash, RUNS_IN's
// workload id ("workload:symbolruntime") is a non-hashed literal, so this
// uses corruptTargetEntityID rather than flipOneHexChar.
func TestResolveRunsInMaterializedEdgesRejectsWrongTargetEntityID(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	expected, err := LoadExpectedEdges(runsInExpectedEdgesPath(repoRoot), runsInFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	corrupted := append([]ExpectedEdge{}, expected...)
	realTarget := corrupted[0].TargetEntityID
	wrongTarget := corruptTargetEntityID(realTarget)
	corrupted[0].TargetEntityID = wrongTarget
	path := writeSymbolRuntimeExpectedEdgesFixture(t, corrupted)

	ok, detail := resolveRunsInMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a fixture whose target_entity_id does not match the real Workload MERGE-key id")
	}
	if !strings.Contains(detail, "MISSING") || !strings.Contains(detail, "EXTRA") {
		t.Errorf("detail = %q, want it to name both the MISSING (corrupted) edge and the EXTRA (real) edge", detail)
	}
	if !strings.Contains(detail, wrongTarget) {
		t.Errorf("detail = %q, want it to name the corrupted target id %q as MISSING", detail, wrongTarget)
	}
	if !strings.Contains(detail, realTarget) {
		t.Errorf("detail = %q, want it to name the real target id %q as EXTRA", detail, realTarget)
	}
}

// TestRunsInFamilyMissingRegistryTypeIsCaught proves
// missingSymbolRuntimeExpectedTypes fires when the fixture drops RUNS_IN, the
// family's only registry type, mirroring TestShellExecFamilyMissingRegistryTypeIsCaught.
func TestRunsInFamilyMissingRegistryTypeIsCaught(t *testing.T) {
	t.Parallel()
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	path := writeSymbolRuntimeExpectedEdgesFixture(t, []ExpectedEdge{
		{RelationshipType: "NOT_RUNS_IN", SourceEntityID: "a", TargetEntityID: "b"},
	})

	ok, detail := resolveRunsInMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a fixture naming no RUNS_IN edge")
	}
	if !strings.Contains(detail, "RUNS_IN") {
		t.Errorf("detail = %q, want it to name the missing RUNS_IN registry type", detail)
	}
}
