// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPISpecIncludesSemanticSearchRoute(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	item := testutil.MustMapField(t, paths, "/api/v0/search/semantic")
	post := testutil.MustMapField(t, item, "post")
	requestBody := testutil.MustMapField(t, post, "requestBody")
	content := testutil.MustMapField(t, requestBody, "content")
	jsonContent := testutil.MustMapField(t, content, "application/json")
	schema := testutil.MustMapField(t, jsonContent, "schema")
	required := mustSliceField(t, schema, "required")
	for _, want := range []string{"repo_id", "query", "mode", "limit", "timeout_ms"} {
		if !openAPIStringSliceContains(required, want) {
			t.Fatalf("semantic search required fields = %#v, want %q", required, want)
		}
	}
	properties := testutil.MustMapField(t, schema, "properties")
	for _, want := range []string{"source_kinds", "service_id", "workload_id", "environment", "rerank", "languages"} {
		if _, ok := properties[want]; !ok {
			t.Fatalf("semantic search request schema missing %q", want)
		}
	}

	responses := testutil.MustMapField(t, post, "responses")
	if _, ok := responses["409"]; !ok {
		t.Fatal("semantic search responses missing 409 ambiguous repository-scope response")
	}
	okResponse := testutil.MustMapField(t, responses, "200")
	okContent := testutil.MustMapField(t, okResponse, "content")
	okJSON := testutil.MustMapField(t, okContent, "application/json")
	okSchema := testutil.MustMapField(t, okJSON, "schema")
	okProperties := testutil.MustMapField(t, okSchema, "properties")
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

	resultItems := testutil.MustMapField(t, testutil.MustMapField(t, okProperties, "results"), "items")
	resultProperties := testutil.MustMapField(t, resultItems, "properties")
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
