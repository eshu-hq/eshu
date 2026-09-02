// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecServiceStoryExposesDossierFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	serviceStoryPath := querytestutil.MustMapField(t, paths, "/api/v0/services/{service_name}/story")
	serviceStoryGet := querytestutil.MustMapField(t, serviceStoryPath, "get")
	serviceStoryResponses := querytestutil.MustMapField(t, serviceStoryGet, "responses")
	serviceStoryOK := querytestutil.MustMapField(t, serviceStoryResponses, "200")
	serviceStoryContent := querytestutil.MustMapField(t, querytestutil.MustMapField(t, serviceStoryOK, "content"), "application/json")
	serviceStorySchema := querytestutil.MustMapField(t, querytestutil.MustMapField(t, serviceStoryContent, "schema"), "properties")

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

	components := querytestutil.MustMapField(t, spec, "components")
	schemas := querytestutil.MustMapField(t, components, "schemas")
	workloadContextSchema := querytestutil.MustMapField(t, schemas, "WorkloadContext")
	workloadContextProperties := querytestutil.MustMapField(t, workloadContextSchema, "properties")
	if _, ok := workloadContextProperties["entrypoint_candidates"]; !ok {
		t.Fatal("WorkloadContext schema missing entrypoint_candidates")
	}
}
