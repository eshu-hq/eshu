// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Split out of profile_handler_test.go to keep that file under the repo's
// 500-line cap (issue #5164). fakeLocalIdentityListStore, fixedNow, and
// bodyContains are shared package test helpers defined in
// profile_handler_test.go.

// ---------------------------------------------------------------------------
// GET /api/v0/auth/local/api-tokens (list)
// ---------------------------------------------------------------------------

func TestLocalIdentityAPITokenListHandlerNilStorePanicsAs503(t *testing.T) {
	t.Parallel()

	// fakeLocalIdentityListStore with nil tokensBySubject — but we test the
	// nil-Store guard at the handler level.  Build handler with nil Store.
	handler := &LocalIdentityHandler{
		Store: nil,
		Now:   fixedNow,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/local/api-tokens", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		SubjectIDHash: "subject-hash-a",
		AllScopes:     true,
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil Store: status = %d, want 503", rec.Code)
	}
}

func TestLocalIdentityAPITokenListHandlerReturnsMedataOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityListStore{
		tokensBySubject: map[string][]LocalIdentityAPITokenListItem{
			"subject-hash-a": {
				{
					TokenID:    "tok-001",
					TokenClass: "personal",
					IssuedAt:   now.Add(-24 * time.Hour),
					ExpiresAt:  now.Add(24 * time.Hour),
				},
			},
		},
	}
	handler := &LocalIdentityHandler{
		Store: store,
		Now:   func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/local/api-tokens", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		SubjectIDHash: "subject-hash-a",
		AllScopes:     true,
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// display_label is intentionally NOT in this forbidden list (issue #3708):
	// it is the real, non-secret operator-facing label, unlike token_hash,
	// api_token, and display_handle_hash (a hash, not a label). This fixture's
	// seeded item carries no label, so "display_label" is correctly absent —
	// see TestLocalIdentityAPITokenListHandlerIncludesDisplayLabelWhenPresent
	// for the positive case.
	for _, forbidden := range []string{"token_hash", "api_token", "display_handle_hash"} {
		if bodyContains(body, forbidden) {
			t.Errorf("token list response exposes forbidden field %q: %s", forbidden, body)
		}
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tokens, _ := resp["tokens"].([]any)
	if len(tokens) != 1 {
		t.Fatalf("tokens len = %d, want 1", len(tokens))
	}
	if bodyContains(body, "display_label") {
		t.Errorf("token list response includes display_label for an item with no label: %s", body)
	}
	// Assert the handler forwarded the correct subject hash to the store.
	if store.capturedSubject != "subject-hash-a" {
		t.Errorf("store received subject %q, want subject-hash-a", store.capturedSubject)
	}
}

// TestLocalIdentityAPITokenListHandlerIncludesDisplayLabelWhenPresent is the
// positive counterpart to the metadata-only test above: when a token's
// display_label is set (issue #3708), it must appear verbatim in the list
// response so the console can render a human-readable name for the token.
func TestLocalIdentityAPITokenListHandlerIncludesDisplayLabelWhenPresent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityListStore{
		tokensBySubject: map[string][]LocalIdentityAPITokenListItem{
			"subject-hash-a": {
				{
					TokenID:      "tok-001",
					TokenClass:   "personal",
					DisplayLabel: "owner laptop",
					IssuedAt:     now.Add(-24 * time.Hour),
					ExpiresAt:    now.Add(24 * time.Hour),
				},
			},
		},
	}
	handler := &LocalIdentityHandler{
		Store: store,
		Now:   func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/local/api-tokens", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		SubjectIDHash: "subject-hash-a",
		AllScopes:     true,
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tokens, _ := resp["tokens"].([]any)
	if len(tokens) != 1 {
		t.Fatalf("tokens len = %d, want 1", len(tokens))
	}
	token, _ := tokens[0].(map[string]any)
	if got, want := token["display_label"], "owner laptop"; got != want {
		t.Fatalf("token display_label = %v, want %q", got, want)
	}
}

// TestLocalIdentityAPITokenListHandlerCrossSubjectIsolation proves that caller
// A cannot see caller B's tokens. The fake is keyed by subject — B has tokens,
// A has none — and we assert A gets empty AND the forwarded hash is A's.
func TestLocalIdentityAPITokenListHandlerCrossSubjectIsolation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	// Seed tokens for subject-hash-b only.
	store := &fakeLocalIdentityListStore{
		tokensBySubject: map[string][]LocalIdentityAPITokenListItem{
			"subject-hash-b": {
				{
					TokenID:    "tok-b-001",
					TokenClass: "personal",
					IssuedAt:   now.Add(-1 * time.Hour),
				},
			},
		},
	}
	handler := &LocalIdentityHandler{
		Store: store,
		Now:   func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// Caller authenticates as subject-hash-a.
	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/local/api-tokens", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		SubjectIDHash: "subject-hash-a",
		AllScopes:     true,
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// Assert handler forwarded A's subject, not B's.
	if store.capturedSubject != "subject-hash-a" {
		t.Errorf("handler forwarded subject %q, want subject-hash-a", store.capturedSubject)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tokens, _ := resp["tokens"].([]any)
	// A has no tokens — B's token must not leak to A.
	if len(tokens) != 0 {
		t.Fatalf("cross-subject isolation failed: A received %d tokens (B's data)", len(tokens))
	}
}
