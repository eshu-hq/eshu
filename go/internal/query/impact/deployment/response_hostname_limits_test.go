// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func traceLimitRows(kind string, n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{"target": fmt.Sprintf("%s-%03d.example.test", kind, i)})
	}
	return rows
}

// TestBuildDeploymentTraceResponseCapsHostnamesAndEntrypoints proves the trace
// response cuts hostnames and entrypoints to ContextStoryItemLimit, reports
// hostname_limits/entrypoint_limits with the pre-cut totals, and keeps the
// overview counts at the full totals (#7169).
func TestBuildDeploymentTraceResponseCapsHostnamesAndEntrypoints(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"id":          "workload-1",
		"name":        "payments-api",
		"kind":        "service",
		"repo_id":     "repo-1",
		"repo_name":   "payments",
		"hostnames":   traceLimitRows("host", 671),
		"entrypoints": traceLimitRows("entry", 671),
	}

	got := BuildDeploymentTraceResponse("payments-api", ctx, map[string]any{})

	if n := len(querycontract.MapSliceValue(got, "hostnames")); n != querycontract.ContextStoryItemLimit {
		t.Fatalf("hostnames len = %d, want %d", n, querycontract.ContextStoryItemLimit)
	}
	if n := len(querycontract.MapSliceValue(got, "entrypoints")); n != querycontract.ContextStoryItemLimit {
		t.Fatalf("entrypoints len = %d, want %d", n, querycontract.ContextStoryItemLimit)
	}
	for _, tc := range []struct{ key, count string }{
		{"hostname_limits", "hostname_count"},
		{"entrypoint_limits", "entrypoint_count"},
	} {
		limits := querycontract.MapValue(got, tc.key)
		if limits == nil {
			t.Fatalf("%s missing, want limit/total/truncated/drilldown_tool", tc.key)
		}
		if g := querycontract.IntVal(limits, "limit"); g != querycontract.ContextStoryItemLimit {
			t.Fatalf("%s.limit = %d, want %d", tc.key, g, querycontract.ContextStoryItemLimit)
		}
		if g := querycontract.IntVal(limits, "total"); g != 671 {
			t.Fatalf("%s.total = %d, want 671", tc.key, g)
		}
		if !querycontract.BoolVal(limits, "truncated") {
			t.Fatalf("%s.truncated = false next to a cut list, want true", tc.key)
		}
		if g := querycontract.StringVal(limits, "drilldown_tool"); g != "get_workload_context" {
			t.Fatalf("%s.drilldown_tool = %q, want get_workload_context", tc.key, g)
		}
		overview := querycontract.MapValue(got, "deployment_overview")
		if g := querycontract.IntVal(overview, tc.count); g != 671 {
			t.Fatalf("deployment_overview.%s = %d, want 671 (full total, not the cut)", tc.count, g)
		}
	}
}

// TestBuildDeploymentTraceResponseWithinCapReportsUntruncatedLimits proves a
// trace with a small hostname list is returned whole and reports its total
// with truncated false, never claiming a cut that did not happen.
func TestBuildDeploymentTraceResponseWithinCapReportsUntruncatedLimits(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"id":        "workload-1",
		"name":      "payments-api",
		"kind":      "service",
		"repo_id":   "repo-1",
		"repo_name": "payments",
		"hostnames": traceLimitRows("host", 3),
	}

	got := BuildDeploymentTraceResponse("payments-api", ctx, map[string]any{})

	if n := len(querycontract.MapSliceValue(got, "hostnames")); n != 3 {
		t.Fatalf("hostnames len = %d, want 3", n)
	}
	limits := querycontract.MapValue(got, "hostname_limits")
	if g := querycontract.IntVal(limits, "total"); g != 3 {
		t.Fatalf("hostname_limits.total = %d, want 3", g)
	}
	if querycontract.BoolVal(limits, "truncated") {
		t.Fatal("hostname_limits.truncated = true with nothing cut, want false")
	}
	if _, ok := got["entrypoint_limits"]; ok {
		t.Fatalf("entrypoint_limits = %#v, want absent when there are no entrypoints", got["entrypoint_limits"])
	}
}

// TestBuildDeploymentTraceResponseCapsNetworkPaths proves the trace response
// cuts network_paths (one row per entrypoint) to ContextStoryItemLimit, reports
// network_path_limits with the pre-cut total, and keeps the overview count at
// the full total (#7169).
func TestBuildDeploymentTraceResponseCapsNetworkPaths(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"id":            "workload-1",
		"name":          "payments-api",
		"kind":          "service",
		"repo_id":       "repo-1",
		"repo_name":     "payments",
		"network_paths": traceLimitRows("path", 671),
	}

	got := BuildDeploymentTraceResponse("payments-api", ctx, map[string]any{})

	if n := len(querycontract.MapSliceValue(got, "network_paths")); n != querycontract.ContextStoryItemLimit {
		t.Fatalf("network_paths len = %d, want %d", n, querycontract.ContextStoryItemLimit)
	}
	limits := querycontract.MapValue(got, "network_path_limits")
	if limits == nil {
		t.Fatal("network_path_limits missing, want limit/total/truncated/drilldown_tool")
	}
	if g := querycontract.IntVal(limits, "total"); g != 671 {
		t.Fatalf("network_path_limits.total = %d, want 671", g)
	}
	if !querycontract.BoolVal(limits, "truncated") {
		t.Fatal("network_path_limits.truncated = false next to a cut list, want true")
	}
	overview := querycontract.MapValue(got, "deployment_overview")
	if g := querycontract.IntVal(overview, "network_path_count"); g != 671 {
		t.Fatalf("deployment_overview.network_path_count = %d, want 671 (full total, not the cut)", g)
	}
}
