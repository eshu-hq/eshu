// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPIImportDependencyInvestigation(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	importDependencyPath := testutil.MustMapField(t, paths, "/api/v0/code/imports/investigate")
	importDependencyPost := testutil.MustMapField(t, importDependencyPath, "post")
	importDependencyBody := testutil.MustMapField(t, testutil.MustMapField(t, importDependencyPost, "requestBody"), "content")
	importDependencyJSON := testutil.MustMapField(t, importDependencyBody, "application/json")
	importDependencyRequestSchema := testutil.MustMapField(t, testutil.MustMapField(t, importDependencyJSON, "schema"), "properties")
	for _, field := range []string{"query_type", "repo_id", "language", "source_file", "target_file", "source_module", "target_module", "limit", "offset"} {
		if _, ok := importDependencyRequestSchema[field]; !ok {
			t.Fatalf("code/imports/investigate request schema missing %s", field)
		}
	}
	importDependencyResponses := testutil.MustMapField(t, importDependencyPost, "responses")
	importDependencyTooBroad, ok := importDependencyResponses["422"]
	if !ok {
		t.Fatal("code/imports/investigate responses missing 422 scope-too-broad contract")
	}
	tooBroadResponse, ok := importDependencyTooBroad.(map[string]any)
	if !ok {
		t.Fatalf("code/imports/investigate 422 response = %#v, want object", importDependencyTooBroad)
	}
	if _, hasReferenceSibling := tooBroadResponse["$ref"]; hasReferenceSibling {
		t.Fatalf("code/imports/investigate 422 response = %#v, want concrete response without ignored reference siblings", tooBroadResponse)
	}
	tooBroadContent := testutil.MustMapField(t, tooBroadResponse, "content")
	_ = testutil.MustMapField(t, tooBroadContent, "application/json")
	importDependencyOK := testutil.MustMapField(t, importDependencyResponses, "200")
	importDependencyContent := testutil.MustMapField(t, testutil.MustMapField(t, importDependencyOK, "content"), "application/json")
	importDependencyResponseSchema := testutil.MustMapField(t, testutil.MustMapField(t, importDependencyContent, "schema"), "properties")
	for _, field := range []string{"dependencies", "modules", "cycles", "cross_module_calls", "truncated", "next_offset", "source_backend", "coverage"} {
		if _, ok := importDependencyResponseSchema[field]; !ok {
			t.Fatalf("code/imports/investigate response schema missing %s", field)
		}
	}
	cycles := testutil.MustMapField(t, importDependencyResponseSchema, "cycles")
	cycleItems := testutil.MustMapField(t, cycles, "items")
	cycleProperties := testutil.MustMapField(t, cycleItems, "properties")
	for _, field := range []string{"repo_id", "repo_name", "source_file", "target_file", "relationship_type", "cycle_path", "cycle_edges"} {
		if _, ok := cycleProperties[field]; !ok {
			t.Fatalf("code/imports/investigate cycle row schema missing %s", field)
		}
	}
	for _, field := range []string{"results", "matches"} {
		if _, ok := importDependencyResponseSchema[field]; ok {
			t.Fatalf("code/imports/investigate response schema includes ambiguous %s alias", field)
		}
	}
}
