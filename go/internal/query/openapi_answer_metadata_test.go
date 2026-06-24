// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecDocumentsAnswerMetadataOnAnswerRoutes(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	for _, tc := range []struct {
		path   string
		method string
	}{
		{path: "/api/v0/services/{service_name}/story", method: "get"},
		{path: "/api/v0/repositories/{repo_id}/story", method: "get"},
		{path: "/api/v0/code/topics/investigate", method: "post"},
		{path: "/api/v0/impact/change-surface/investigate", method: "post"},
		{path: "/api/v0/incidents/{incident_id}/context", method: "get"},
		{path: "/api/v0/compare/environments", method: "post"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			properties := openAPIResponseProperties(t, spec, tc.path, tc.method)
			metadata := mustMapField(t, properties, "answer_metadata")
			if got, want := metadata["type"], "object"; got != want {
				t.Fatalf("answer_metadata type = %#v, want %#v", got, want)
			}
		})
	}
}

func openAPIResponseProperties(
	t *testing.T,
	spec map[string]any,
	path string,
	method string,
) map[string]any {
	t.Helper()

	paths := mustMapField(t, spec, "paths")
	route := mustMapField(t, paths, path)
	operation := mustMapField(t, route, method)
	responses := mustMapField(t, operation, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, okResponse, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	return mustMapField(t, schema, "properties")
}
