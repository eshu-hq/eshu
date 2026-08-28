// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantictools

import (
	"strings"
	"testing"
)

// TestSearchSemanticContextLanguagesDescriptionMatchesHandler pins the MCP half
// of the #6271 wire contract.
//
// `search_semantic_context` forwards to POST /api/v0/search/semantic, and that
// handler does not validate language values at all: semanticSearchLanguages
// (go/internal/query/semantic_search_params.go) lowercases and trims each token
// and returns no error, so an unmatched language comes back as a 200 with an
// empty result set. The tool description told callers the opposite — that
// unknown values are rejected with HTTP 400 — which is a promise the server has
// never kept.
//
// The behavioural proof lives with the handler
// (TestSemanticSearchHandlerUnknownLanguageReturnsEmptyResult and
// TestSemanticSearchLanguagesOpenAPIDescriptionMatchesHandler in
// go/internal/query); this package cannot reach it without importing the query
// handler into the tool-definition package, so it asserts the description only.
func TestSearchSemanticContextLanguagesDescriptionMatchesHandler(t *testing.T) {
	t.Parallel()

	tools := SearchTools()
	if got, want := len(tools), 1; got != want {
		t.Fatalf("search tool count = %d, want %d", got, want)
	}
	tool := tools[0]
	if got, want := tool.Name, "search_semantic_context"; got != want {
		t.Fatalf("search tool name = %q, want %q", got, want)
	}

	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema is %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties is %T, want map[string]any", schema["properties"])
	}
	languages, ok := properties["languages"].(map[string]any)
	if !ok {
		t.Fatalf("languages property is %T, want map[string]any", properties["languages"])
	}
	description, ok := languages["description"].(string)
	if !ok {
		t.Fatalf("languages description is %T, want string", languages["description"])
	}

	lowered := strings.ToLower(description)
	for _, forbidden := range []string{"400", "reject"} {
		if strings.Contains(lowered, forbidden) {
			t.Fatalf("search_semantic_context languages description promises a rejection the handler never "+
				"performs (%q in %q); an unmatched language returns 200 with an empty result set",
				forbidden, description)
		}
	}
	if !strings.Contains(lowered, "empty result") {
		t.Fatalf("search_semantic_context languages description does not say what an unmatched language "+
			"actually does (expected it to name the empty result set): %q", description)
	}
}
