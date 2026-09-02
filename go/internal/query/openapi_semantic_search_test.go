// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecIncludesSemanticSearchRoute(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	item := querytestutil.MustMapField(t, paths, "/api/v0/search/semantic")
	post := querytestutil.MustMapField(t, item, "post")
	requestBody := querytestutil.MustMapField(t, post, "requestBody")
	content := querytestutil.MustMapField(t, requestBody, "content")
	jsonContent := querytestutil.MustMapField(t, content, "application/json")
	schema := querytestutil.MustMapField(t, jsonContent, "schema")
	required := mustSliceField(t, schema, "required")
	for _, want := range []string{"repo_id", "query", "mode", "limit", "timeout_ms"} {
		if !openAPIStringSliceContains(required, want) {
			t.Fatalf("semantic search required fields = %#v, want %q", required, want)
		}
	}
	properties := querytestutil.MustMapField(t, schema, "properties")
	for _, want := range []string{"source_kinds", "service_id", "workload_id", "environment", "rerank", "languages"} {
		if _, ok := properties[want]; !ok {
			t.Fatalf("semantic search request schema missing %q", want)
		}
	}

	responses := querytestutil.MustMapField(t, post, "responses")
	if _, ok := responses["409"]; !ok {
		t.Fatal("semantic search responses missing 409 ambiguous repository-scope response")
	}
	okResponse := querytestutil.MustMapField(t, responses, "200")
	okContent := querytestutil.MustMapField(t, okResponse, "content")
	okJSON := querytestutil.MustMapField(t, okContent, "application/json")
	okSchema := querytestutil.MustMapField(t, okJSON, "schema")
	okProperties := querytestutil.MustMapField(t, okSchema, "properties")
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

	resultItems := querytestutil.MustMapField(t, querytestutil.MustMapField(t, okProperties, "results"), "items")
	resultProperties := querytestutil.MustMapField(t, resultItems, "properties")
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
