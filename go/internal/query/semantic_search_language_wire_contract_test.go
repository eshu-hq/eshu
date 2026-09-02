// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// This test stays in package query rather than moving with the handler family
// (#6060): it binds this package's generated OpenAPI spec to the route's
// behavior, and OpenAPISpec() lives here. It drives the handler through Mount
// and a real mux, which is what the deployed API does, so a route that stopped
// being registered fails here instead of passing.
//
// TestSemanticSearchLanguagesOpenAPIDescriptionMatchesHandler binds the wire
// description of the `languages` filter to what the handler actually does
// (#6271).
//
// The runtime contract is open-pass: semanticSearchLanguages
// (semanticsearch/semantic_search_params.go) lowercases and trims each token
// and returns no error, so there is no code path that rejects a language value. An unmatched
// language reaches the index and comes back as a 200 with an empty result set.
// The public reference documents that. The generated OpenAPI path text
// contradicted it, telling callers unknown values are rejected with HTTP 400 —
// a promise the server has never kept and that pushes a caller into
// pre-validating against a registry the index is not bound by.
//
// The behavioural half runs first so this is not a string-matching test about a
// string: if the handler ever does start rejecting, the 200 assertion fails and
// the reader is pointed at the description rather than left to guess which side
// moved. The MCP tool description carries the same claim and is pinned by
// TestSearchSemanticContextLanguagesDescriptionMatchesHandler in
// go/internal/mcp/semantic.
func TestSemanticSearchLanguagesOpenAPIDescriptionMatchesHandler(t *testing.T) {
	t.Parallel()

	index := &stubSemanticSearchIndex{
		result: SemanticSearchIndexResult{IndexedDocumentCount: 0},
	}
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := querytestutil.SemanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-1",
		"query":      "service",
		"mode":       "keyword",
		"limit":      10,
		"timeout_ms": 250,
		"languages":  []string{"not_a_real_language_xyz"},
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("unknown language status = %d, want %d; body = %s", got, want, rec.Body.String())
	}

	description := semanticSearchLanguagesOpenAPIDescription(t)
	assertLanguageFilterDescriptionDoesNotPromiseRejection(t, "OpenAPI", description)
}

// semanticSearchLanguagesOpenAPIDescription reads the `languages` request-body
// property description out of the generated spec, so the assertion runs against
// what a caller downloads rather than against the Go literal.
func semanticSearchLanguagesOpenAPIDescription(t *testing.T) string {
	t.Helper()

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
	properties := querytestutil.MustMapField(t, schema, "properties")
	languages := querytestutil.MustMapField(t, properties, "languages")
	description, ok := languages["description"].(string)
	if !ok {
		t.Fatalf("languages description is %T, want string", languages["description"])
	}
	return description
}

// assertLanguageFilterDescriptionDoesNotPromiseRejection is the shared check
// both wire surfaces run. It asserts the negative (no rejection promise) and
// the positive (the empty-result outcome is stated), because dropping the false
// sentence without saying what does happen would leave the caller with no
// answer at all.
func assertLanguageFilterDescriptionDoesNotPromiseRejection(t *testing.T, surface, description string) {
	t.Helper()

	lowered := strings.ToLower(description)
	for _, forbidden := range []string{"400", "reject"} {
		if strings.Contains(lowered, forbidden) {
			t.Fatalf("%s languages description promises a rejection the handler never performs (%q in %q); "+
				"an unmatched language returns 200 with an empty result set", surface, forbidden, description)
		}
	}
	if !strings.Contains(lowered, "empty result") {
		t.Fatalf("%s languages description does not say what an unmatched language actually does "+
			"(expected it to name the empty result set): %q", surface, description)
	}
}
