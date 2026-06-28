// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPIDocumentsCodeFlowRoutes(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	for _, route := range []string{
		"/api/v0/code/flow/taint-path",
		"/api/v0/code/flow/reaching-def",
		"/api/v0/code/flow/cfg-summary",
		"/api/v0/code/flow/pdg-summary",
	} {
		path := mustMapField(t, paths, route)
		post := mustMapField(t, path, "post")
		requestBody := mustMapField(t, post, "requestBody")
		content := mustMapField(t, requestBody, "content")
		jsonContent := mustMapField(t, content, "application/json")
		schema := mustMapField(t, jsonContent, "schema")
		if got, want := schema["$ref"], "#/components/schemas/CodeFlowRequest"; got != want {
			t.Fatalf("%s request schema = %#v, want %#v", route, got, want)
		}
		responses := mustMapField(t, post, "responses")
		for _, code := range []string{"200", "400", "501", "503", "500"} {
			if _, ok := responses[code]; !ok {
				t.Fatalf("%s responses missing %s: %#v", route, code, responses)
			}
		}
	}

	components := mustMapField(t, spec, "components")
	schemas := mustMapField(t, components, "schemas")
	request := mustMapField(t, schemas, "CodeFlowRequest")
	properties := mustMapField(t, request, "properties")
	for _, field := range []string{"repo_id", "language", "symbol", "file_path", "line", "limit"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("CodeFlowRequest missing %s", field)
		}
	}
	response := mustMapField(t, schemas, "CodeFlowResponse")
	responseProperties := mustMapField(t, response, "properties")
	for _, field := range []string{"paths", "definitions", "functions", "summaries", "coverage", "bounds", "source_backend"} {
		if _, ok := responseProperties[field]; !ok {
			t.Fatalf("CodeFlowResponse missing %s", field)
		}
	}
}
