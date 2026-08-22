// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"reflect"
	"testing"

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
		t.Fatalf("unresolved = %v, want none", unresolved)
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
		t.Fatalf("unresolved = %v, want none", unresolved)
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
		t.Fatalf("unresolved = %v, want none", unresolved)
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
		t.Fatalf("unresolved = %v, want exactly [\"repo-1\\x00/widgets\"]", unresolved)
	}
}
