// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
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
			metadata := testutil.MustMapField(t, properties, "answer_metadata")
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

	paths := testutil.MustMapField(t, spec, "paths")
	route := testutil.MustMapField(t, paths, path)
	operation := testutil.MustMapField(t, route, method)
	responses := testutil.MustMapField(t, operation, "responses")
	okResponse := testutil.MustMapField(t, responses, "200")
	content := testutil.MustMapField(t, okResponse, "content")
	jsonContent := testutil.MustMapField(t, content, "application/json")
	schema := testutil.MustMapField(t, jsonContent, "schema")
	return testutil.MustMapField(t, schema, "properties")
}
