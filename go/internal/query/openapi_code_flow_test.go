// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPIDocumentsCodeFlowRoutes(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	for _, route := range []string{
		"/api/v0/code/flow/taint-path",
		"/api/v0/code/flow/reaching-def",
		"/api/v0/code/flow/cfg-summary",
		"/api/v0/code/flow/pdg-summary",
	} {
		path := querytestutil.MustMapField(t, paths, route)
		post := querytestutil.MustMapField(t, path, "post")
		requestBody := querytestutil.MustMapField(t, post, "requestBody")
		content := querytestutil.MustMapField(t, requestBody, "content")
		jsonContent := querytestutil.MustMapField(t, content, "application/json")
		schema := querytestutil.MustMapField(t, jsonContent, "schema")
		if got, want := schema["$ref"], "#/components/schemas/CodeFlowRequest"; got != want {
			t.Fatalf("%s request schema = %#v, want %#v", route, got, want)
		}
		responses := querytestutil.MustMapField(t, post, "responses")
		for _, code := range []string{"200", "400", "501", "503", "500"} {
			if _, ok := responses[code]; !ok {
				t.Fatalf("%s responses missing %s: %#v", route, code, responses)
			}
		}
	}

	components := querytestutil.MustMapField(t, spec, "components")
	schemas := querytestutil.MustMapField(t, components, "schemas")
	request := querytestutil.MustMapField(t, schemas, "CodeFlowRequest")
	properties := querytestutil.MustMapField(t, request, "properties")
	for _, field := range []string{"repo_id", "language", "symbol", "file_path", "line", "limit"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("CodeFlowRequest missing %s", field)
		}
	}
	response := querytestutil.MustMapField(t, schemas, "CodeFlowResponse")
	responseProperties := querytestutil.MustMapField(t, response, "properties")
	for _, field := range []string{"paths", "definitions", "functions", "summaries", "coverage", "bounds", "source_backend"} {
		if _, ok := responseProperties[field]; !ok {
			t.Fatalf("CodeFlowResponse missing %s", field)
		}
	}
}
