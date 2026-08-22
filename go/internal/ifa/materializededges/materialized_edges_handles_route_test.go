// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// handlesRouteTestRow builds a minimal HANDLES_ROUTE upsert row carrying only
// the payload fields handlesRouteRowsToExpectedEdges reads.
func handlesRouteTestRow(functionID, repoID, path, httpMethod string) reducer.SharedProjectionIntentRow {
	return reducer.SharedProjectionIntentRow{
		ProjectionDomain: reducer.DomainHandlesRoute,
		Payload: map[string]any{
			"function_entity_id": functionID,
			"repo_id":            repoID,
			"path":               path,
			"http_method":        httpMethod,
		},
	}
}

// TestHandlesRouteRowsToExpectedEdgesCollapsesMethodDedupe is the required
// offline proof of the GET+POST same-path collapse: production's own
// intent-level dedupe key includes http_method
// (go/internal/reducer/handles_route_intents.go:80), so two intent rows on
// the same (function, repo, path) but different methods both exist as
// distinct SharedProjectionIntentRows -- but the graph-write MERGE identity
// is only the (Function, HANDLES_ROUTE, Endpoint) node pair, so both rows
// MERGE the SAME relationship instance at write time. This test proves the
// GUARD's own conversion collapses them to exactly one ExpectedEdge,
// deterministically, without depending on which row's write happens to land
// last against a live backend.
func TestHandlesRouteRowsToExpectedEdgesCollapsesMethodDedupe(t *testing.T) {
	t.Parallel()

	rows := []reducer.SharedProjectionIntentRow{
		handlesRouteTestRow("content-entity:fn", "repo-1", "/widgets", "GET"),
		handlesRouteTestRow("content-entity:fn", "repo-1", "/widgets", "POST"),
	}
	endpointIDs := map[string]string{"repo-1\x00/widgets": "endpoint:abc"}

	edges, unresolved := handlesRouteRowsToExpectedEdges(rows, endpointIDs, "HANDLES_ROUTE")
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %q, want none", unresolved)
	}
	if len(edges) != 1 {
		t.Fatalf("expected exactly 1 collapsed edge for a GET+POST same-path pair, got %d: %+v", len(edges), edges)
	}
	want := ExpectedEdge{RelationshipType: "HANDLES_ROUTE", SourceEntityID: "content-entity:fn", TargetEntityID: "endpoint:abc"}
	if !reflect.DeepEqual(edges[0], want) {
		t.Fatalf("edge = %+v, want %+v", edges[0], want)
	}
}

// TestHandlesRouteRowsToExpectedEdgesDistinctPathsStayDistinct proves the
// dedupe is scoped to (function, repo, path), not just function: two
// different route paths for the same handler function must remain two
// distinct edges.
func TestHandlesRouteRowsToExpectedEdgesDistinctPathsStayDistinct(t *testing.T) {
	t.Parallel()

	rows := []reducer.SharedProjectionIntentRow{
		handlesRouteTestRow("content-entity:fn", "repo-1", "/widgets", "GET"),
		handlesRouteTestRow("content-entity:fn", "repo-1", "/gadgets", "GET"),
	}
	endpointIDs := map[string]string{
		"repo-1\x00/widgets": "endpoint:widgets",
		"repo-1\x00/gadgets": "endpoint:gadgets",
	}

	edges, unresolved := handlesRouteRowsToExpectedEdges(rows, endpointIDs, "HANDLES_ROUTE")
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %q, want none", unresolved)
	}
	if len(edges) != 2 {
		t.Fatalf("expected 2 distinct edges for 2 distinct paths, got %d: %+v", len(edges), edges)
	}
}

// TestHandlesRouteRowsToExpectedEdgesAssertsMethodOnlyWhenUnambiguous proves
// the asymmetry a live regression needs to be catchable: a route served by
// exactly one method gets its http_method asserted as a Property (safe,
// deterministic), while a route served by two-or-more methods on the same
// path does NOT (the write-order-dependent case), even when both shapes are
// compared in the same call.
func TestHandlesRouteRowsToExpectedEdgesAssertsMethodOnlyWhenUnambiguous(t *testing.T) {
	t.Parallel()

	rows := []reducer.SharedProjectionIntentRow{
		handlesRouteTestRow("content-entity:widgets", "repo-1", "/widgets", "GET"),
		handlesRouteTestRow("content-entity:widgets", "repo-1", "/widgets", "POST"),
		handlesRouteTestRow("content-entity:healthz", "repo-1", "/healthz", "GET"),
	}
	endpointIDs := map[string]string{
		"repo-1\x00/widgets": "endpoint:widgets",
		"repo-1\x00/healthz": "endpoint:healthz",
	}

	edges, unresolved := handlesRouteRowsToExpectedEdges(rows, endpointIDs, "HANDLES_ROUTE")
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %q, want none", unresolved)
	}
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges, got %d: %+v", len(edges), edges)
	}

	byTarget := make(map[string]ExpectedEdge, len(edges))
	for _, e := range edges {
		byTarget[e.TargetEntityID] = e
	}

	multiMethod := byTarget["endpoint:widgets"]
	if len(multiMethod.Properties) != 0 {
		t.Errorf("multi-method /widgets edge asserted Properties %v, want none (write-order dependent, unsafe to assert)", multiMethod.Properties)
	}

	singleMethod := byTarget["endpoint:healthz"]
	wantProps := map[string]string{"http_method": "GET"}
	if !reflect.DeepEqual(singleMethod.Properties, wantProps) {
		t.Errorf("single-method /healthz edge Properties = %v, want %v (deterministic, safe to assert)", singleMethod.Properties, wantProps)
	}
}

// TestHandlesRouteRowsToExpectedEdgesReportsUnresolvedEndpoint proves a row
// whose (repo_id, path) has no committed Endpoint is reported as unresolved
// rather than silently dropped or given a zero-value target.
func TestHandlesRouteRowsToExpectedEdgesReportsUnresolvedEndpoint(t *testing.T) {
	t.Parallel()

	rows := []reducer.SharedProjectionIntentRow{
		handlesRouteTestRow("content-entity:fn", "repo-1", "/widgets", "GET"),
	}
	edges, unresolved := handlesRouteRowsToExpectedEdges(rows, map[string]string{}, "HANDLES_ROUTE")
	if len(edges) != 0 {
		t.Fatalf("expected 0 edges when no Endpoint resolves, got %d: %+v", len(edges), edges)
	}
	if len(unresolved) != 1 || unresolved[0] != "repo-1\x00/widgets" {
		t.Fatalf("unresolved = %q, want exactly [\"repo-1\\x00/widgets\"]", unresolved)
	}
}

// TestResolveHandlesRouteMaterializedEdgesRejectsAnExtraEdge drives the REAL
// resolveHandlesRouteMaterializedEdges entry point (not handlesRouteRowsToExpectedEdges
// in isolation) over the cataloged symbol-runtime Odù, so a future refactor
// that bypassed compareSymbolRuntimeExpectedEdges would still be caught here.
// Shrinking the fixture below what the extractor actually produces must
// surface the surplus real edge as EXTRA -- the direction a "contains all
// expected" check would miss -- mirroring
// TestShellExecFamilyExpectedSetRejectsAnExtraEdge.
func TestResolveHandlesRouteMaterializedEdgesRejectsAnExtraEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	expected, err := LoadExpectedEdges(handlesRouteExpectedEdgesPath(repoRoot), handlesRouteFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	path := writeSymbolRuntimeExpectedEdgesFixture(t, expected[:len(expected)-1])

	ok, detail := resolveHandlesRouteMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a short expected set; an edge nobody derived went unreported")
	}
	if !strings.Contains(detail, "EXTRA") {
		t.Errorf("detail = %q, want it to name the EXTRA edge", detail)
	}
}

// TestResolveHandlesRouteMaterializedEdgesRejectsAMissingEdge pads the real
// fixture with a fabricated edge the extractor does not produce, driving the
// real entry point. The fixture over-claiming an edge must surface it as
// MISSING, mirroring TestShellExecFamilyExpectedSetRejectsAMissingEdge.
func TestResolveHandlesRouteMaterializedEdgesRejectsAMissingEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	expected, err := LoadExpectedEdges(handlesRouteExpectedEdgesPath(repoRoot), handlesRouteFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	padded := append(append([]ExpectedEdge{}, expected...), ExpectedEdge{
		RelationshipType: "HANDLES_ROUTE",
		SourceEntityID:   ifa.SymbolRuntimeFamilyHandlerFunctionUID,
		TargetEntityID:   "endpoint:deadbeefdeadbeefdeadbeef",
	})
	path := writeSymbolRuntimeExpectedEdgesFixture(t, padded)

	ok, detail := resolveHandlesRouteMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against an expectation the extractor does not satisfy")
	}
	if !strings.Contains(detail, "MISSING") {
		t.Errorf("detail = %q, want it to name the MISSING edge", detail)
	}
}

// TestResolveHandlesRouteMaterializedEdgesRejectsWrongSourceEntityID proves
// the guard fails when a fixture's source_entity_id drifts from the real
// canonical Function uid by a single hex character -- exactly what a wrong
// content.CanonicalEntityID derivation looks like in production. That
// failure mode is otherwise silent live: the write template's source-side
// MATCH finds nothing, the MERGE no-ops, and no dead letter is raised.
func TestResolveHandlesRouteMaterializedEdgesRejectsWrongSourceEntityID(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	expected, err := LoadExpectedEdges(handlesRouteExpectedEdgesPath(repoRoot), handlesRouteFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	corrupted := append([]ExpectedEdge{}, expected...)
	realSource := corrupted[0].SourceEntityID
	wrongSource := flipOneHexChar(t, realSource)
	corrupted[0].SourceEntityID = wrongSource
	path := writeSymbolRuntimeExpectedEdgesFixture(t, corrupted)

	ok, detail := resolveHandlesRouteMaterializedEdges(odu, path)
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

// TestResolveHandlesRouteMaterializedEdgesRejectsWrongTargetEntityID mirrors
// the source-id test for the Endpoint target id: a wrong Endpoint hash must
// fail the same way, naming both the bogus expectation and the real edge the
// projection actually produced.
func TestResolveHandlesRouteMaterializedEdgesRejectsWrongTargetEntityID(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	expected, err := LoadExpectedEdges(handlesRouteExpectedEdgesPath(repoRoot), handlesRouteFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	corrupted := append([]ExpectedEdge{}, expected...)
	realTarget := corrupted[0].TargetEntityID
	wrongTarget := corruptTargetEntityID(realTarget)
	corrupted[0].TargetEntityID = wrongTarget
	path := writeSymbolRuntimeExpectedEdgesFixture(t, corrupted)

	ok, detail := resolveHandlesRouteMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a fixture whose target_entity_id does not match the real Endpoint MERGE-key id")
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

// TestHandlesRouteFamilyMissingRegistryTypeIsCaught proves
// missingSymbolRuntimeExpectedTypes fires when the fixture drops
// HANDLES_ROUTE, the family's only registry type, mirroring
// TestShellExecFamilyMissingRegistryTypeIsCaught.
func TestHandlesRouteFamilyMissingRegistryTypeIsCaught(t *testing.T) {
	t.Parallel()
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	path := writeSymbolRuntimeExpectedEdgesFixture(t, []ExpectedEdge{
		{RelationshipType: "NOT_HANDLES_ROUTE", SourceEntityID: "a", TargetEntityID: "b"},
	})

	ok, detail := resolveHandlesRouteMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a fixture naming no HANDLES_ROUTE edge")
	}
	if !strings.Contains(detail, "HANDLES_ROUTE") {
		t.Errorf("detail = %q, want it to name the missing HANDLES_ROUTE registry type", detail)
	}
}

// TestHandlesRouteUnresolvedDiagnosticEscapesNUL pins the gate's own failure
// message, not a test helper's. handlesRouteRowsToExpectedEdges keys unresolved
// pairs as repoID + "\x00" + path, and the guard in
// resolveHandlesRouteMaterializedEdges reports that slice to whoever is reading
// gate output. Rendered with %v the NUL reaches the terminal and the CI log raw,
// where it truncates or corrupts the surrounding line; %q escapes it. The
// message only ever prints on failure, which is exactly when it has to be
// legible, so this asserts the rendering rather than trusting it.
func TestHandlesRouteUnresolvedDiagnosticEscapesNUL(t *testing.T) {
	t.Parallel()

	unresolved := []string{"repo-1\x00/widgets"}
	rendered := fmt.Sprintf("odù %q: no workload-materialization Endpoint resolved for %d (repo_id, path) pair(s) HANDLES_ROUTE needs: %q; HANDLES_ROUTE cannot bind without a workload-committed Endpoint at that key",
		"ifa-symbol-runtime-family", len(unresolved), unresolved)

	if strings.ContainsRune(rendered, 0) {
		t.Fatalf("diagnostic carries a raw NUL byte; it must be escaped so gate output stays readable: %q", rendered)
	}
	if !strings.Contains(rendered, `repo-1\x00/widgets`) {
		t.Fatalf("diagnostic does not show the escaped key; got %s", rendered)
	}
}
