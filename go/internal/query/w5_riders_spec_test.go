// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Wave-5 rider seven-leg replication, leg 7: spec (Refs
// eshu-hq/eshu#9669, satellite eshu-hq/eshu-api#101). Core-side
// reading of the W8a spec legs: the served openapi.json stays 200
// valid JSON, carries the three base-leg keys with their pinned
// operationIds, and promises nothing for the rider spellings — no
// patch under /api/v0/code/bundles and no slash-twin keys. Riders are
// spellings, not routes.

// w5RidersSpecOperations pins the served method plus operationId per
// base-leg path key, mirroring the owning core fragments.
var w5RidersSpecOperations = map[string]map[string]string{
	"/api/v0/code/bundles":        {"post": "searchCodeBundles"},
	"/api/v0/auth/sign-in-policy": {"get": "getPublicSignInPolicy"},
	"/api/v0/auth/providers":      {"get": "listAuthProviders"},
}

// w5RidersServedSpec fetches the served openapi.json as a decoded
// document.
func w5RidersServedSpec(t *testing.T) map[string]any {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/openapi.json", ServeOpenAPI)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/openapi.json", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("served spec status = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Result().Body)
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("served spec is not valid JSON: %v", err)
	}
	return doc
}

// TestW5RidersNativeServesBaseSpecKeys pins that the served document
// carries every rider base-leg key with its pinned operationId.
func TestW5RidersNativeServesBaseSpecKeys(t *testing.T) {
	t.Parallel()

	doc := w5RidersServedSpec(t)
	paths, _ := doc["paths"].(map[string]any)
	for key, ops := range w5RidersSpecOperations {
		item, ok := paths[key].(map[string]any)
		if !ok {
			t.Errorf("served spec lacks rider base path key %s", key)
			continue
		}
		for method, wantID := range ops {
			op, _ := item[method].(map[string]any)
			if got := op["operationId"]; got != wantID {
				t.Errorf("%s %s operationId = %v, want %q", key, method, got, wantID)
			}
		}
	}
}

// TestW5RidersSpellingsStayOutOfSpec pins the disclosed absence: the
// bundles key carries no patch method, and no slash-twin key exists —
// the 3 rider spellings are judgments, not routes, and promise
// nothing in the document.
func TestW5RidersSpellingsStayOutOfSpec(t *testing.T) {
	t.Parallel()

	doc := w5RidersServedSpec(t)
	paths, _ := doc["paths"].(map[string]any)
	if item, ok := paths["/api/v0/code/bundles"].(map[string]any); ok {
		if _, hasPatch := item["patch"]; hasPatch {
			t.Errorf("served spec carries patch under /api/v0/code/bundles, want the rider spelling absent")
		}
	}
	for _, twin := range []string{"/api/v0/auth/sign-in-policy/", "/api/v0/auth/providers/"} {
		if _, exists := paths[twin]; exists {
			t.Errorf("served spec carries slash-twin key %s, want rider spellings absent", twin)
		}
	}
}
