// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPIDocumentsCodeFlowRoutes(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	for _, route := range []string{
		"/api/v0/code/flow/taint-path",
		"/api/v0/code/flow/reaching-def",
		"/api/v0/code/flow/cfg-summary",
		"/api/v0/code/flow/pdg-summary",
	} {
		path := testutil.MustMapField(t, paths, route)
		post := testutil.MustMapField(t, path, "post")
		requestBody := testutil.MustMapField(t, post, "requestBody")
		content := testutil.MustMapField(t, requestBody, "content")
		jsonContent := testutil.MustMapField(t, content, "application/json")
		schema := testutil.MustMapField(t, jsonContent, "schema")
		if got, want := schema["$ref"], "#/components/schemas/CodeFlowRequest"; got != want {
			t.Fatalf("%s request schema = %#v, want %#v", route, got, want)
		}
		responses := testutil.MustMapField(t, post, "responses")
		for _, code := range []string{"200", "400", "501", "503", "500"} {
			if _, ok := responses[code]; !ok {
				t.Fatalf("%s responses missing %s: %#v", route, code, responses)
			}
		}
	}

	components := testutil.MustMapField(t, spec, "components")
	schemas := testutil.MustMapField(t, components, "schemas")
	request := testutil.MustMapField(t, schemas, "CodeFlowRequest")
	properties := testutil.MustMapField(t, request, "properties")
	for _, field := range []string{"repo_id", "language", "symbol", "file_path", "line", "limit"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("CodeFlowRequest missing %s", field)
		}
	}
	response := testutil.MustMapField(t, schemas, "CodeFlowResponse")
	responseProperties := testutil.MustMapField(t, response, "properties")
	// Owner: codequery/flow.go writeCodeFlowResponse emits only query,
	// coverage, ambiguity, bounds, and one result family. kind, count,
	// limit, truncated, source_backend, scope, and answer_metadata are
	// phantoms the owner never sends; asserting them here would pin a
	// contract mismatch instead of the wire.
	for _, field := range []string{"query", "coverage", "ambiguity", "bounds", "paths", "definitions", "functions", "summaries"} {
		if _, ok := responseProperties[field]; !ok {
			t.Fatalf("CodeFlowResponse missing %s", field)
		}
	}
	for _, phantom := range []string{"scope", "kind", "count", "limit", "truncated", "source_backend", "answer_metadata"} {
		if _, ok := responseProperties[phantom]; ok {
			t.Fatalf("CodeFlowResponse carries phantom %s not emitted by writeCodeFlowResponse", phantom)
		}
	}
}
