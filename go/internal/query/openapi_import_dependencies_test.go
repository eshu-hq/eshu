// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPIImportDependencyInvestigation(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	importDependencyPath := querytestutil.MustMapField(t, paths, "/api/v0/code/imports/investigate")
	importDependencyPost := querytestutil.MustMapField(t, importDependencyPath, "post")
	importDependencyBody := querytestutil.MustMapField(t, querytestutil.MustMapField(t, importDependencyPost, "requestBody"), "content")
	importDependencyJSON := querytestutil.MustMapField(t, importDependencyBody, "application/json")
	importDependencyRequest := querytestutil.MustMapField(t, querytestutil.MustMapField(t, importDependencyJSON, "schema"), "properties")
	for _, field := range []string{"query_type", "repo_id", "language", "source_file", "target_file", "source_module", "target_module", "limit", "offset"} {
		if _, ok := importDependencyRequest[field]; !ok {
			t.Fatalf("code/imports/investigate request schema missing %s", field)
		}
	}
	importDependencyResponses := querytestutil.MustMapField(t, importDependencyPost, "responses")
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
	tooBroadContent := querytestutil.MustMapField(t, tooBroadResponse, "content")
	_ = querytestutil.MustMapField(t, tooBroadContent, "application/json")
	importDependencyOK := querytestutil.MustMapField(t, importDependencyResponses, "200")
	importDependencyContent := querytestutil.MustMapField(t, querytestutil.MustMapField(t, importDependencyOK, "content"), "application/json")
	importDependencyResponse := querytestutil.MustMapField(t, querytestutil.MustMapField(t, importDependencyContent, "schema"), "properties")
	for _, field := range []string{"dependencies", "modules", "cycles", "cross_module_calls", "truncated", "next_offset", "source_backend", "coverage"} {
		if _, ok := importDependencyResponse[field]; !ok {
			t.Fatalf("code/imports/investigate response schema missing %s", field)
		}
	}
	cycles := querytestutil.MustMapField(t, importDependencyResponse, "cycles")
	cycleItems := querytestutil.MustMapField(t, cycles, "items")
	cycleProperties := querytestutil.MustMapField(t, cycleItems, "properties")
	for _, field := range []string{"repo_id", "repo_name", "source_file", "target_file", "relationship_type", "cycle_path", "cycle_edges"} {
		if _, ok := cycleProperties[field]; !ok {
			t.Fatalf("code/imports/investigate cycle row schema missing %s", field)
		}
	}
	for _, field := range []string{"results", "matches"} {
		if _, ok := importDependencyResponse[field]; ok {
			t.Fatalf("code/imports/investigate response schema includes ambiguous %s alias", field)
		}
	}
}
