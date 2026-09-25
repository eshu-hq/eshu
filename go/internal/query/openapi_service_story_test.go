// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPISpecServiceStoryExposesDossierFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	serviceStoryPath := testutil.MustMapField(t, paths, "/api/v0/services/{service_name}/story")
	serviceStoryGet := testutil.MustMapField(t, serviceStoryPath, "get")
	serviceStoryResponses := testutil.MustMapField(t, serviceStoryGet, "responses")
	serviceStoryOK := testutil.MustMapField(t, serviceStoryResponses, "200")
	serviceStoryContent := testutil.MustMapField(t, testutil.MustMapField(t, serviceStoryOK, "content"), "application/json")
	serviceStorySchema := testutil.MustMapField(t, testutil.MustMapField(t, serviceStoryContent, "schema"), "properties")

	for _, field := range []string{
		"service_identity",
		"code_to_runtime_trace",
		"api_surface",
		"entrypoint_candidates",
		"deployment_lanes",
		"upstream_dependencies",
		"downstream_consumers",
		"evidence_graph",
		"result_limits",
		"investigation",
		"cloud_resources",
		"uncorrelated_cloud_resources",
		"uncorrelated_cloud_resources_truncated",
		"evidence_boundaries",
	} {
		if _, ok := serviceStorySchema[field]; !ok {
			t.Fatalf("services/{service_name}/story response schema missing %s", field)
		}
	}
}

func TestOpenAPISpecServiceContextExposesEntrypointCandidates(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	components := testutil.MustMapField(t, spec, "components")
	schemas := testutil.MustMapField(t, components, "schemas")
	workloadContextSchema := testutil.MustMapField(t, schemas, "WorkloadContext")
	workloadContextProperties := testutil.MustMapField(t, workloadContextSchema, "properties")
	if _, ok := workloadContextProperties["entrypoint_candidates"]; !ok {
		t.Fatal("WorkloadContext schema missing entrypoint_candidates")
	}
}
