// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// #5167 code family: an empty scoped grant is answered without touching a
// backend, and that short-circuit is the one path through these handlers that
// never runs the loop building their result slices. A branch that returns the
// zero value of its result struct therefore hands the writer a nil slice, and
// encoding/json writes nil as `null`, not `[]`.
//
// Every one of these fields is declared as an array in the OpenAPI response
// schema, so `null` is a decode failure in a generated client -- and it happens
// only for a grantless token, which is exactly the caller least likely to be
// covered by anyone's integration test. The empty page a grantless caller gets
// has to be shaped like every other empty page.

type codeEmptyGrantShapeRoute struct {
	name    string
	path    string
	body    map[string]any
	fields  []string
	handler func() *CodeHandler
}

func codeEmptyGrantShapeRoutes() []codeEmptyGrantShapeRoute {
	contentHandler := func(store ContentStore) func() *CodeHandler {
		return func() *CodeHandler {
			return &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
		}
	}
	graphHandler := func() *CodeHandler {
		return &CodeHandler{
			Profile: ProfileLocalAuthoritative,
			Neo4j: fakeGraphReader{
				run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
					return nil, nil
				},
			},
		}
	}
	return []codeEmptyGrantShapeRoute{
		{
			name:    "inspect_structural_inventory",
			path:    "/api/v0/code/structure/inventory",
			body:    map[string]any{"inventory_kind": "entity", "language": "go"},
			fields:  []string{"results", "matches"},
			handler: contentHandler(&structuralInventoryGrantStore{}),
		},
		{
			name:    "inspect_structural_inventory_function_count_by_file",
			path:    "/api/v0/code/structure/inventory",
			body:    map[string]any{"inventory_kind": "function_count_by_file", "language": "go"},
			fields:  []string{"results", "matches"},
			handler: contentHandler(&structuralInventoryGrantStore{}),
		},
		{
			name:    "search_symbols",
			path:    "/api/v0/code/symbols/search",
			body:    map[string]any{"symbol": "RefreshSession"},
			fields:  []string{"results"},
			handler: contentHandler(&symbolSearchGrantStore{}),
		},
		{
			name:    "investigate_hardcoded_secrets",
			path:    "/api/v0/code/security/secrets/investigate",
			body:    map[string]any{},
			fields:  []string{"findings"},
			handler: contentHandler(&hardcodedSecretGrantStore{}),
		},
		{
			name:    "investigate_code_topic",
			path:    "/api/v0/code/topics/investigate",
			body:    map[string]any{"topic": "session refresh"},
			fields:  []string{"evidence_groups", "matched_files", "matched_symbols", "call_graph_handles"},
			handler: contentHandler(&codeTopicGrantContentStore{}),
		},
		{
			name:    "find_dead_code",
			path:    "/api/v0/code/dead-code",
			body:    map[string]any{"language": "go"},
			fields:  []string{"results"},
			handler: contentHandler(&deadCodeGrantContentStore{}),
		},
		{
			name:    "investigate_dead_code",
			path:    "/api/v0/code/dead-code/investigate",
			body:    map[string]any{"language": "go"},
			fields:  []string{"candidate_buckets.cleanup_ready", "candidate_buckets.ambiguous", "candidate_buckets.suppressed"},
			handler: contentHandler(&deadCodeGrantContentStore{}),
		},
		{
			name:    "get_complexity",
			path:    "/api/v0/code/complexity",
			body:    map[string]any{},
			fields:  []string{"results"},
			handler: graphHandler,
		},
		{
			name:    "inspect_code_quality",
			path:    "/api/v0/code/quality/inspect",
			body:    map[string]any{"check": "refactoring_candidates"},
			fields:  []string{"results"},
			handler: graphHandler,
		},
	}
}

func TestCodeRoutesEmptyGrantAnswersWithArraysNotNull(t *testing.T) {
	t.Parallel()

	for _, route := range codeEmptyGrantShapeRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			handler := route.handler()
			mux := http.NewServeMux()
			handler.Mount(mux)

			auth := codeGrantScopedAuthContext(nil)
			req := newCodeGrantRouteRequest(t, route.path, route.body, &auth)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			data := decodeEnvelopeData(t, rec.Body.Bytes())
			for _, field := range route.fields {
				value, ok := codeEmptyGrantShapeField(data, field)
				if !ok {
					t.Fatalf("response has no %q field: %s", field, rec.Body.String())
				}
				rows, ok := value.([]any)
				if !ok {
					t.Fatalf("%q = %#v, want an empty JSON array; a nil slice serializes as null and the OpenAPI schema declares an array: %s", field, value, rec.Body.String())
				}
				if len(rows) != 0 {
					t.Fatalf("%q = %#v, want no rows for a grantless caller", field, rows)
				}
			}
		})
	}
}

// codeEmptyGrantShapeField resolves a dotted path so a route whose rows live in
// a nested bucket map can be checked the same way as a flat results list.
func codeEmptyGrantShapeField(data map[string]any, path string) (any, bool) {
	current := any(data)
	for _, segment := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
