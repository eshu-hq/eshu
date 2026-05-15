package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPICallGraphMetrics(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	metricsPath := mustMapField(t, paths, "/api/v0/code/call-graph/metrics")
	metricsPost := mustMapField(t, metricsPath, "post")
	metricsBody := mustMapField(t, mustMapField(t, metricsPost, "requestBody"), "content")
	metricsJSON := mustMapField(t, metricsBody, "application/json")
	metricsRequest := mustMapField(t, mustMapField(t, metricsJSON, "schema"), "properties")
	for _, field := range []string{"metric_type", "repo_id", "language", "limit", "offset"} {
		if _, ok := metricsRequest[field]; !ok {
			t.Fatalf("code/call-graph/metrics request schema missing %s", field)
		}
	}
	metricsResponses := mustMapField(t, metricsPost, "responses")
	metricsOK := mustMapField(t, metricsResponses, "200")
	metricsContent := mustMapField(t, mustMapField(t, metricsOK, "content"), "application/json")
	metricsResponse := mustMapField(t, mustMapField(t, metricsContent, "schema"), "properties")
	for _, field := range []string{"functions", "truncated", "next_offset", "source_backend", "coverage"} {
		if _, ok := metricsResponse[field]; !ok {
			t.Fatalf("code/call-graph/metrics response schema missing %s", field)
		}
	}
	for _, field := range []string{"results", "matches"} {
		if _, ok := metricsResponse[field]; ok {
			t.Fatalf("code/call-graph/metrics response schema includes ambiguous %s alias", field)
		}
	}
}
