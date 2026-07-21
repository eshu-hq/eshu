// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

// TestOpenAPISpecDocumentsEvidenceBoundariesOnBoundaryRoutes proves the
// evidence_boundaries response field (evidence_boundaries.go) stays in
// lockstep with the OpenAPI schema for all four routes that can attach it:
// get_service_story and get_workload_story are covered by dedicated
// assertions elsewhere (openapi_service_story_test.go); this test covers the
// two remaining routes plus reconfirms shape for all four.
func TestOpenAPISpecDocumentsEvidenceBoundariesOnBoundaryRoutes(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	for _, tc := range []struct {
		path   string
		method string
	}{
		{path: "/api/v0/workloads/{workload_id}/story", method: "get"},
		{path: "/api/v0/services/{service_name}/story", method: "get"},
		{path: "/api/v0/impact/trace-deployment-chain", method: "post"},
		{path: "/api/v0/repositories/{repo_id}/story", method: "get"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			properties := openAPIResponseProperties(t, spec, tc.path, tc.method)
			boundaries := mustMapField(t, properties, "evidence_boundaries")
			if got, want := boundaries["type"], "array"; got != want {
				t.Fatalf("evidence_boundaries type = %#v, want %#v", got, want)
			}
			items := mustMapField(t, boundaries, "items")
			itemProperties := mustMapField(t, items, "properties")
			for _, field := range []string{"domain", "read_surface", "reason"} {
				if _, ok := itemProperties[field]; !ok {
					t.Fatalf("%s evidence_boundaries[].%s missing from schema", tc.path, field)
				}
			}
		})
	}
}
