package mcp

import "testing"

func TestResolveRouteMapsCallGraphMetricsToolToBoundedEndpoint(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("inspect_call_graph_metrics", map[string]any{
		"metric_type": "hub_functions",
		"repo_id":     "repo-1",
		"language":    "go",
		"limit":       float64(10),
		"offset":      float64(5),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/call-graph/metrics"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["metric_type"], "hub_functions"; got != want {
		t.Fatalf("body[metric_type] = %#v, want %#v", got, want)
	}
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 10; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	if got, want := body["offset"], 5; got != want {
		t.Fatalf("body[offset] = %#v, want %#v", got, want)
	}
}

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
