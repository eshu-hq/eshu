// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeinteltools

import "testing"

func TestCallGraphMetricsToolSchemaRequiresRepoScopeAndBounds(t *testing.T) {
	t.Parallel()

	tool := callGraphMetricsTool()
	if got, want := tool.Name, "inspect_call_graph_metrics"; got != want {
		t.Fatalf("tool.Name = %q, want %q", got, want)
	}
	schema := tool.InputSchema.(map[string]any)
	required := schema["required"].([]string)
	if len(required) != 1 || required[0] != "repo_id" {
		t.Fatalf("required = %#v, want repo_id only", required)
	}
	properties := schema["properties"].(map[string]any)
	metricType := properties["metric_type"].(map[string]any)
	enums := metricType["enum"].([]string)
	if got, want := len(enums), 2; got != want {
		t.Fatalf("metric_type enum count = %d, want %d", got, want)
	}
	limit := properties["limit"].(map[string]any)
	if got, want := limit["maximum"], 200; got != want {
		t.Fatalf("limit maximum = %#v, want %#v", got, want)
	}
	if got, want := limit["minimum"], 1; got != want {
		t.Fatalf("limit minimum = %#v, want %#v", got, want)
	}
	offset := properties["offset"].(map[string]any)
	if got, want := offset["maximum"], 10000; got != want {
		t.Fatalf("offset maximum = %#v, want %#v", got, want)
	}
}
