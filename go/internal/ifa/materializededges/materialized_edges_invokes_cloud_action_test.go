// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestInvokesCloudActionRowsToExpectedEdgesIsOneToOne pins the family's
// simplest-of-the-three shape: unlike HANDLES_ROUTE (dedupe collapse) and
// RUNS_IN (fan-out), each upsert row maps directly to exactly one edge, with
// no family-specific dedupe or workload-projection step.
func TestInvokesCloudActionRowsToExpectedEdgesIsOneToOne(t *testing.T) {
	t.Parallel()

	rows := []reducer.SharedProjectionIntentRow{
		{
			ProjectionDomain: reducer.DomainInvokesCloudAction,
			Payload: map[string]any{
				"function_id": "content-entity:fn1",
				"action_id":   "cloud-action:s3:putobject",
			},
		},
		{
			ProjectionDomain: reducer.DomainInvokesCloudAction,
			Payload: map[string]any{
				"function_id": "content-entity:fn2",
				"action_id":   "cloud-action:dynamodb:getitem",
			},
		},
	}

	edges := invokesCloudActionRowsToExpectedEdges(rows, "INVOKES_CLOUD_ACTION")
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges (one per row), got %d: %+v", len(edges), edges)
	}
	want := []ExpectedEdge{
		{RelationshipType: "INVOKES_CLOUD_ACTION", SourceEntityID: "content-entity:fn1", TargetEntityID: "cloud-action:s3:putobject"},
		{RelationshipType: "INVOKES_CLOUD_ACTION", SourceEntityID: "content-entity:fn2", TargetEntityID: "cloud-action:dynamodb:getitem"},
	}
	for i, e := range want {
		if !reflect.DeepEqual(edges[i], e) {
			t.Errorf("edge %d = %+v, want %+v", i, edges[i], e)
		}
	}
}

// TestSingleRegistryEdgeTypeRejectsNonSingleton proves singleRegistryEdgeType
// fails closed rather than silently picking an arbitrary type when a
// registry carries anything other than exactly one relationship type -- the
// invariant every symbol-runtime guard depends on to avoid hardcoding a
// relationship-type literal that could drift from the writer registry.
func TestSingleRegistryEdgeTypeRejectsNonSingleton(t *testing.T) {
	t.Parallel()

	if _, err := singleRegistryEdgeType("some_family", map[string]struct{}{}); err == nil {
		t.Fatal("expected an error for an empty registry, got nil")
	}
	if _, err := singleRegistryEdgeType("some_family", map[string]struct{}{"A": {}, "B": {}}); err == nil {
		t.Fatal("expected an error for a multi-type registry, got nil")
	}
	got, err := singleRegistryEdgeType("some_family", map[string]struct{}{"HANDLES_ROUTE": {}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "HANDLES_ROUTE" {
		t.Fatalf("got %q, want %q", got, "HANDLES_ROUTE")
	}
}

// TestResolveInvokesCloudActionMaterializedEdgesRejectsAMissingEdge pads the
// real fixture with a fabricated edge the extractor does not produce,
// driving the real resolveInvokesCloudActionMaterializedEdges entry point.
// The fixture over-claiming an edge must surface it as MISSING, mirroring
// TestShellExecFamilyExpectedSetRejectsAMissingEdge.
func TestResolveInvokesCloudActionMaterializedEdgesRejectsAMissingEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	expected, err := LoadExpectedEdges(invokesCloudActionExpectedEdgesPath(repoRoot), invokesCloudActionFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	padded := append(append([]ExpectedEdge{}, expected...), ExpectedEdge{
		RelationshipType: "INVOKES_CLOUD_ACTION",
		SourceEntityID:   ifa.SymbolRuntimeFamilyCallerFunctionUID,
		TargetEntityID:   "cloud-action:dynamodb:nonexistent",
	})
	path := writeSymbolRuntimeExpectedEdgesFixture(t, padded)

	ok, detail := resolveInvokesCloudActionMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against an expectation the extractor does not satisfy")
	}
	if !strings.Contains(detail, "MISSING") {
		t.Errorf("detail = %q, want it to name the MISSING edge", detail)
	}
}

// TestResolveInvokesCloudActionMaterializedEdgesRejectsEmptyExpectedSet
// covers this family's "drop the only edge" direction: unlike HANDLES_ROUTE
// and RUNS_IN (2 edges each), INVOKES_CLOUD_ACTION's fixture carries exactly
// 1 edge, so dropping it produces an EMPTY expected set rather than a short
// one. loadSQLRelationshipExpectedEdges rejects a zero-edge file at load
// time (materialized_edges_sql.go), so this proves that rejection actually
// reaches resolveInvokesCloudActionMaterializedEdges's caller for THIS
// family specifically, mirroring TestShellExecFamilyExpectedSetIsVacuityGuarded.
func TestResolveInvokesCloudActionMaterializedEdgesRejectsEmptyExpectedSet(t *testing.T) {
	t.Parallel()
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	path := writeSymbolRuntimeExpectedEdgesFixture(t, nil)

	ok, detail := resolveInvokesCloudActionMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against an empty expected-edge fixture (its only edge dropped); the gate would be vacuous")
	}
	if detail == "" {
		t.Error("guard returned no detail for the empty-fixture rejection")
	}
	t.Logf("%s", detail)
}

// TestResolveInvokesCloudActionMaterializedEdgesRejectsWrongSourceEntityID
// proves the guard fails when a fixture's source_entity_id drifts from the
// real canonical Function uid by a single hex character -- exactly what a
// wrong content.CanonicalEntityID derivation looks like in production,
// otherwise silent live (the source-side MATCH finds nothing, the MERGE
// no-ops, no dead letter is raised).
func TestResolveInvokesCloudActionMaterializedEdgesRejectsWrongSourceEntityID(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	expected, err := LoadExpectedEdges(invokesCloudActionExpectedEdgesPath(repoRoot), invokesCloudActionFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	corrupted := append([]ExpectedEdge{}, expected...)
	realSource := corrupted[0].SourceEntityID
	wrongSource := flipOneHexChar(t, realSource)
	corrupted[0].SourceEntityID = wrongSource
	path := writeSymbolRuntimeExpectedEdgesFixture(t, corrupted)

	ok, detail := resolveInvokesCloudActionMaterializedEdges(odu, path)
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

// TestResolveInvokesCloudActionMaterializedEdgesRejectsWrongTargetEntityID
// mirrors the source-id test for the CloudAction target id: a wrong action
// id must fail the same way, naming both the bogus expectation and the real
// edge the extractor actually produced.
func TestResolveInvokesCloudActionMaterializedEdgesRejectsWrongTargetEntityID(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	expected, err := LoadExpectedEdges(invokesCloudActionExpectedEdgesPath(repoRoot), invokesCloudActionFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	corrupted := append([]ExpectedEdge{}, expected...)
	realTarget := corrupted[0].TargetEntityID
	wrongTarget := corruptTargetEntityID(realTarget)
	corrupted[0].TargetEntityID = wrongTarget
	path := writeSymbolRuntimeExpectedEdgesFixture(t, corrupted)

	ok, detail := resolveInvokesCloudActionMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a fixture whose target_entity_id does not match the real CloudAction MERGE-key id")
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

// TestInvokesCloudActionFamilyMissingRegistryTypeIsCaught proves
// missingSymbolRuntimeExpectedTypes fires when the fixture drops
// INVOKES_CLOUD_ACTION, the family's only registry type, mirroring
// TestShellExecFamilyMissingRegistryTypeIsCaught.
func TestInvokesCloudActionFamilyMissingRegistryTypeIsCaught(t *testing.T) {
	t.Parallel()
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	path := writeSymbolRuntimeExpectedEdgesFixture(t, []ExpectedEdge{
		{RelationshipType: "NOT_INVOKES_CLOUD_ACTION", SourceEntityID: "a", TargetEntityID: "b"},
	})

	ok, detail := resolveInvokesCloudActionMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a fixture naming no INVOKES_CLOUD_ACTION edge")
	}
	if !strings.Contains(detail, "INVOKES_CLOUD_ACTION") {
		t.Errorf("detail = %q, want it to name the missing INVOKES_CLOUD_ACTION registry type", detail)
	}
}
