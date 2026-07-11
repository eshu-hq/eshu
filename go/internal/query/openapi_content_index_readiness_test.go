// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecContentSearchDocumentsDeferredIndexUnavailable(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	for _, endpoint := range []struct {
		route  string
		method string
	}{
		{route: "/api/v0/content/files/search", method: "post"},
		{route: "/api/v0/content/entities/search", method: "post"},
		{route: "/api/v0/code/search", method: "post"},
		{route: "/api/v0/code/topics/investigate", method: "post"},
		{route: "/api/v0/code/structure/inventory", method: "post"},
		{route: "/api/v0/impact/trace-deployment-chain", method: "post"},
		{route: "/api/v0/workloads/{workload_id}/context", method: "get"},
		{route: "/api/v0/workloads/{workload_id}/story", method: "get"},
		{route: "/api/v0/services/{service_name}/context", method: "get"},
		{route: "/api/v0/services/{service_name}/story", method: "get"},
		{route: "/api/v0/investigations/services/{service_name}", method: "get"},
	} {
		searchPath := mustMapField(t, paths, endpoint.route)
		operation := mustMapField(t, searchPath, endpoint.method)
		responses := mustMapField(t, operation, "responses")
		if _, ok := responses["503"]; !ok {
			t.Fatalf("%s responses missing deferred-index 503", endpoint.route)
		}
	}
}
