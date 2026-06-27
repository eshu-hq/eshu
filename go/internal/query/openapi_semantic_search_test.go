// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesSemanticSearchRoute(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	item := mustMapField(t, paths, "/api/v0/search/semantic")
	post := mustMapField(t, item, "post")
	requestBody := mustMapField(t, post, "requestBody")
	content := mustMapField(t, requestBody, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	required := mustSliceField(t, schema, "required")
	for _, want := range []string{"repo_id", "query", "mode", "limit", "timeout_ms"} {
		if !openAPIStringSliceContains(required, want) {
			t.Fatalf("semantic search required fields = %#v, want %q", required, want)
		}
	}
	properties := mustMapField(t, schema, "properties")
	for _, want := range []string{"source_kinds", "service_id", "workload_id", "environment", "rerank", "languages"} {
		if _, ok := properties[want]; !ok {
			t.Fatalf("semantic search request schema missing %q", want)
		}
	}

	responses := mustMapField(t, post, "responses")
	okResponse := mustMapField(t, responses, "200")
	okContent := mustMapField(t, okResponse, "content")
	okJSON := mustMapField(t, okContent, "application/json")
	okSchema := mustMapField(t, okJSON, "schema")
	okProperties := mustMapField(t, okSchema, "properties")
	for _, want := range []string{
		"search_mode",
		"truncated",
		"false_canonical_claim_count",
		"indexed_document_count",
		"retrieval_state",
		"corpus_may_be_truncated",
		"facets",
		"results",
		"rerank",
		"recommended_next_calls",
	} {
		if _, ok := okProperties[want]; !ok {
			t.Fatalf("semantic search response schema missing %q", want)
		}
	}

	resultItems := mustMapField(t, mustMapField(t, okProperties, "results"), "items")
	resultProperties := mustMapField(t, resultItems, "properties")
	if _, ok := resultProperties["ranking_basis"]; !ok {
		t.Fatalf("semantic search result schema missing %q", "ranking_basis")
	}
}

func openAPIStringSliceContains(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
