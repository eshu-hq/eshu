// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
)

// TestOpenAPIDocumentsTheRankCursorWalk pins the #7746 wire contract on the
// two walk routes: the rank_cursor request field, the rank_walk response
// block with every key the handler emits, the 409 basis-mismatch answer, and
// the window size the walk is bounded by.
func TestOpenAPIDocumentsTheRankCursorWalk(t *testing.T) {
	t.Parallel()
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("OpenAPI spec is not JSON: %v", err)
	}
	field := func(value any, keys ...string) any {
		for _, key := range keys {
			object, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("spec path %v: %q is not an object", keys, key)
			}
			value = object[key]
		}
		return value
	}
	routes := map[string]string{
		"/api/v0/code/search":             "CodeSearchResponse",
		"/api/v0/content/entities/search": "EntityContentSearchResponse",
	}
	for path, response := range routes {
		post := field(spec, "paths", path, "post")
		properties := field(post, "requestBody", "content", "application/json", "schema", "properties")
		if field(properties, "rank_cursor", "type") != "string" {
			t.Fatalf("%s request has no string rank_cursor", path)
		}
		if field(post, "responses", "409", "$ref") != "#/components/responses/RankCursorConflict" {
			t.Fatalf("%s does not document the 409 basis mismatch", path)
		}
		if field(spec, "components", "schemas", response, "properties", "rank_walk", "$ref") != "#/components/schemas/RankWalk" {
			t.Fatalf("%s response has no rank_walk block", response)
		}
	}
	walk := field(spec, "components", "schemas", "RankWalk", "properties")
	page, err := rank.Walk(rank.Window{
		Tool: rank.ToolFindCode, Lane: rank.LaneGraph, Order: rank.OrderGraphName,
		Rows: []rank.Row{{ID: "a"}, {ID: "b"}},
	}, nil, rank.StartToken, 1)
	if err != nil {
		t.Fatalf("Walk error = %v", err)
	}
	for key := range page.Block() {
		if field(walk, key) == nil {
			t.Fatalf("RankWalk schema lacks %q, which the handler emits", key)
		}
	}
	if field(walk, "window_size", "maximum") != float64(rank.WindowSize) {
		t.Fatalf("RankWalk.window_size maximum = %v, want %d", field(walk, "window_size", "maximum"), rank.WindowSize)
	}
	errorSchema := field(spec, "components", "schemas", "RankCursorError", "properties")
	codes := field(errorSchema, "code", "enum").([]any)
	wantCodes := rank.ErrorCodes()
	if len(codes) != len(wantCodes) {
		t.Fatalf("RankCursorError.code enum = %v, want %v", codes, wantCodes)
	}
	for i, code := range wantCodes {
		if codes[i] != string(code) {
			t.Fatalf("RankCursorError.code enum = %v, want %v", codes, wantCodes)
		}
	}
	reasons := field(errorSchema, "reason", "enum").([]any)
	wantReasons := rank.ErrorReasons()
	if len(reasons) != len(wantReasons) {
		t.Fatalf("RankCursorError.reason enum = %v, want %v", reasons, wantReasons)
	}
	for i, reason := range wantReasons {
		if reasons[i] != reason {
			t.Fatalf("RankCursorError.reason enum = %v, want %v", reasons, wantReasons)
		}
	}
	if file := field(spec, "paths", "/api/v0/content/files/search", "post", "requestBody", "content", "application/json", "schema", "properties"); field(file, "rank_cursor") == nil {
		t.Fatal("the file search request must document rank_cursor as refused, so a client learns it is not ignored")
	}
}
