// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"slices"
	"testing"
)

// repoContextSourceToolShapeKey is the second get_repo_context shape that
// asserts source_tool_breakdown on a repository whose outgoing edge really
// carries source_tool (#6782).
const repoContextSourceToolShapeKey = "get_repo_context?assert=source_tool_breakdown"

// TestGoldenSnapshotAssertsSourceToolBreakdownOnTruth pins the #6782 retarget.
//
// The orders-api get_repo_context shape used to require source_tool_breakdown.
// That requirement was calibrated against the NornicDB v1.3.3 missing-row-key
// defect, which stamped orders-api's unstamped package-consumption DEPENDS_ON
// edge with the literal token "row.source_tool". With the writer fixed, the
// documented response omits the field for orders-api on every backend. The
// field stays asserted on helm-umbrella-chart, whose only outgoing dependency is
// the helm-stamped DEPLOYS_FROM edge rc-34 pins.
func TestGoldenSnapshotAssertsSourceToolBreakdownOnTruth(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	ordersAPI, ok := snapshot.QueryShapes.MCP["get_repo_context"]
	if !ok {
		t.Fatal("query_shapes.mcp missing get_repo_context")
	}
	if slices.Contains(ordersAPI.RequiredResponseFields, "source_tool_breakdown") {
		t.Error("orders-api get_repo_context must not require source_tool_breakdown: it has no source_tool-stamped outgoing edge")
	}

	stamped, ok := snapshot.QueryShapes.MCP[repoContextSourceToolShapeKey]
	if !ok {
		t.Fatalf("query_shapes.mcp missing %s", repoContextSourceToolShapeKey)
	}
	if got := mcpToolName(repoContextSourceToolShapeKey); got != "get_repo_context" {
		t.Fatalf("mcpToolName(%q) = %q, want get_repo_context", repoContextSourceToolShapeKey, got)
	}
	if !slices.Contains(stamped.RequiredResponseFields, "source_tool_breakdown") {
		t.Error("the stamped-repo shape must require source_tool_breakdown")
	}
	if got := stamped.Arguments["repo_id"]; got != "helm-umbrella-chart" {
		t.Errorf("stamped-repo shape repo_id = %v, want helm-umbrella-chart", got)
	}
	if got, ok := stamped.RequiredJSONValues["source_tool_breakdown.helm"].(float64); !ok || got != 1 {
		t.Errorf("source_tool_breakdown.helm pin = %#v, want 1", stamped.RequiredJSONValues["source_tool_breakdown.helm"])
	}

	requireCorrelation(t, snapshot, "rc-34", "helm")

	const base = `"repository":{"id":"repository:r_x","name":"helm-umbrella-chart"},` +
		`"consumers":[],"dependency_count":1,"entry_points":[],"file_count":2,"infrastructure":[],` +
		`"languages":[],"partial_reasons":[],"platform_count":0,"workload_count":0,` +
		`"relationship_overview":{"outgoing":1},"relationships":[{"type":"DEPLOYS_FROM"}]`
	cases := []struct {
		name   string
		body   string
		wantOK bool
	}{
		{"helm-stamped breakdown passes", `{` + base + `,"source_tool_breakdown":{"helm":1}}`, true},
		{"omitted breakdown fails", `{` + base + `}`, false},
		{"junk token breakdown fails", `{` + base + `,"source_tool_breakdown":{"row.source_tool":1}}`, false},
		{"wrong helm count fails", `{` + base + `,"source_tool_breakdown":{"helm":2}}`, false},
	}
	for _, tc := range cases {
		finding := EvaluateQueryShape(repoContextSourceToolShapeKey, stamped, []byte(tc.body))
		if finding.OK != tc.wantOK {
			t.Errorf("%s: OK = %v, want %v (%s)", tc.name, finding.OK, tc.wantOK, finding.Detail)
		}
	}
}

// requireCorrelation fails unless the snapshot pins the correlation id with
// the given allowed source_tool value, so the stamped-repo shape and the graph
// assertion it relies on cannot drift apart silently.
func requireCorrelation(t *testing.T, snapshot Snapshot, id string, sourceTool string) {
	t.Helper()
	for _, rc := range snapshot.Graph.RequiredCorrelations {
		if rc.ID != id {
			continue
		}
		if !slices.Contains(rc.AllowedEdgePropertyValues["source_tool"], sourceTool) {
			t.Errorf("%s allowed source_tool = %v, want it to include %q", id, rc.AllowedEdgePropertyValues["source_tool"], sourceTool)
		}
		return
	}
	t.Errorf("snapshot has no required correlation %s", id)
}
