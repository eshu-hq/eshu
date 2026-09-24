// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPICallGraphMetrics(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	metricsPath := testutil.MustMapField(t, paths, "/api/v0/code/call-graph/metrics")
	metricsPost := testutil.MustMapField(t, metricsPath, "post")
	metricsBody := testutil.MustMapField(t, testutil.MustMapField(t, metricsPost, "requestBody"), "content")
	metricsJSON := testutil.MustMapField(t, metricsBody, "application/json")
	metricsRequest := testutil.MustMapField(t, testutil.MustMapField(t, metricsJSON, "schema"), "properties")
	for _, field := range []string{"metric_type", "repo_id", "language", "limit", "offset"} {
		if _, ok := metricsRequest[field]; !ok {
			t.Fatalf("code/call-graph/metrics request schema missing %s", field)
		}
	}
	limit := testutil.MustMapField(t, metricsRequest, "limit")
	if got, want := limit["minimum"], float64(1); got != want {
		t.Fatalf("limit minimum = %#v, want %#v", got, want)
	}
	metricsResponses := testutil.MustMapField(t, metricsPost, "responses")
	if _, ok := metricsResponses["422"]; !ok {
		t.Fatal("code/call-graph/metrics responses missing exact-scope overflow status 422")
	}
	metricsOK := testutil.MustMapField(t, metricsResponses, "200")
	metricsContent := testutil.MustMapField(t, testutil.MustMapField(t, metricsOK, "content"), "application/json")
	metricsResponse := testutil.MustMapField(t, testutil.MustMapField(t, metricsContent, "schema"), "properties")
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
