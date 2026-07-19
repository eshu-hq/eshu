// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Split out of profile_handler_test.go to keep that file under the repo's
// 500-line cap (issue #5164). fixedNow and bodyContains are shared package
// test helpers defined in profile_handler_test.go.

// fakeBrowserSessionListStore captures the subject hash forwarded by the
// handler so isolation tests can assert (a) the right hash was forwarded and
// (b) a different subject gets only their own rows.
type fakeBrowserSessionListStore struct {
	fakeBrowserSessionStore
	// sessionsBySubject maps subject_id_hash → rows.  Callers seed rows for
	// the subjects they care about; an unknown subject gets nil/empty.
	sessionsBySubject map[string][]BrowserSessionListItem
	listErr           error
	// capturedSubject is set on each call so tests can verify forwarding.
	capturedSubject string
}

func (s *fakeBrowserSessionListStore) ListSessionsBySubject(
	_ context.Context,
	subjectIDHash string,
	_ time.Time,
	_ string,
	_ int,
	_ int,
) ([]BrowserSessionListItem, error) {
	s.capturedSubject = subjectIDHash
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.sessionsBySubject[subjectIDHash], nil
}

// ---------------------------------------------------------------------------
// GET /api/v0/auth/sessions
// ---------------------------------------------------------------------------

func TestBrowserSessionListHandlerRequiresBrowserSession(t *testing.T) {
	t.Parallel()

	store := &fakeBrowserSessionListStore{}
	handler := &BrowserSessionListHandler{Store: store, Now: fixedNow}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/sessions", nil)
	// No auth context — must reject.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestBrowserSessionListHandlerNilStorePanicsAs503(t *testing.T) {
	t.Parallel()

	handler := &BrowserSessionListHandler{Store: nil, Now: fixedNow}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/sessions", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		SubjectIDHash: "subject-hash-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil Store: status = %d, want 503", rec.Code)
	}
}

func TestBrowserSessionListHandlerReturnsSessions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeBrowserSessionListStore{
		sessionsBySubject: map[string][]BrowserSessionListItem{
			"subject-hash-a": {
				{
					IssuedAt:          now.Add(-2 * time.Hour),
					LastSeenAt:        now.Add(-1 * time.Minute),
					IdleExpiresAt:     now.Add(29 * time.Minute),
					AbsoluteExpiresAt: now.Add(10 * time.Hour),
					TenantID:          "tenant_a",
					WorkspaceID:       "workspace_a",
					Current:           true,
				},
			},
		},
	}
	handler := &BrowserSessionListHandler{Store: store, Now: func() time.Time { return now }}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/sessions", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		SubjectIDHash: "subject-hash-a",
		TenantID:      "tenant_a",
		WorkspaceID:   "workspace_a",
	}))
	req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "raw-secret"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	sessions, _ := resp["sessions"].([]any)
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(sessions))
	}
	s0, _ := sessions[0].(map[string]any)
	if s0["current"] != true {
		t.Errorf("sessions[0].current = %v, want true", s0["current"])
	}
	// Verify the handler forwarded the correct subject hash to the store.
	if store.capturedSubject != "subject-hash-a" {
		t.Errorf("store received subject %q, want subject-hash-a", store.capturedSubject)
	}
}

func TestBrowserSessionListHandlerPagination(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	// Return 3 sessions when limit=2 (limit+1 to trigger truncation).
	store := &fakeBrowserSessionListStore{
		sessionsBySubject: map[string][]BrowserSessionListItem{
			"subject-hash-a": {
				{IssuedAt: now, LastSeenAt: now, IdleExpiresAt: now, AbsoluteExpiresAt: now, TenantID: "t", WorkspaceID: "w"},
				{IssuedAt: now, LastSeenAt: now, IdleExpiresAt: now, AbsoluteExpiresAt: now, TenantID: "t", WorkspaceID: "w"},
				{IssuedAt: now, LastSeenAt: now, IdleExpiresAt: now, AbsoluteExpiresAt: now, TenantID: "t", WorkspaceID: "w"},
			},
		},
	}
	handler := &BrowserSessionListHandler{Store: store, Now: func() time.Time { return now }}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/sessions?limit=2&offset=0", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		SubjectIDHash: "subject-hash-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	sessions, _ := resp["sessions"].([]any)
	if len(sessions) != 2 {
		t.Fatalf("sessions len = %d, want 2 (truncated from 3 by limit)", len(sessions))
	}
	next, _ := resp["next"].(string)
	if next == "" {
		t.Fatal("expected next link when limit+1 items returned")
	}
	if !strings.Contains(next, "limit=2") || !strings.Contains(next, "offset=2") {
		t.Fatalf("next link = %q, want limit=2&offset=2", next)
	}

	prev, _ := resp["prev"].(string)
	if prev != "" {
		t.Fatalf("prev link should be empty when offset=0, got %q", prev)
	}
}

func TestBrowserSessionListHandlerPaginationPrevLink(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeBrowserSessionListStore{
		sessionsBySubject: map[string][]BrowserSessionListItem{
			"subject-hash-a": {
				{IssuedAt: now, LastSeenAt: now, IdleExpiresAt: now, AbsoluteExpiresAt: now, TenantID: "t", WorkspaceID: "w"},
			},
		},
	}
	handler := &BrowserSessionListHandler{Store: store, Now: func() time.Time { return now }}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// Request page 2 (offset=2, limit=2).
	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/sessions?limit=2&offset=2", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		SubjectIDHash: "subject-hash-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	prev, _ := resp["prev"].(string)
	if prev == "" {
		t.Fatal("expected prev link when offset > 0")
	}
	if !strings.Contains(prev, "limit=2") || !strings.Contains(prev, "offset=0") {
		t.Fatalf("prev link = %q, want limit=2&offset=0", prev)
	}
	next, _ := resp["next"].(string)
	if next != "" {
		t.Fatalf("next link should be empty when no more results, got %q", next)
	}
}

func TestBrowserSessionListHandlerNeverReturnsSessionHashOrCSRF(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeBrowserSessionListStore{
		sessionsBySubject: map[string][]BrowserSessionListItem{
			"subject-hash-a": {
				{
					IssuedAt:          now,
					LastSeenAt:        now,
					IdleExpiresAt:     now.Add(30 * time.Minute),
					AbsoluteExpiresAt: now.Add(12 * time.Hour),
					TenantID:          "tenant_a",
					WorkspaceID:       "workspace_a",
				},
			},
		},
	}
	handler := &BrowserSessionListHandler{Store: store, Now: func() time.Time { return now }}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/sessions", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		SubjectIDHash: "subject-hash-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, forbidden := range []string{"session_hash", "csrf_token", "csrf_token_hash", "token_hash"} {
		if bodyContains(body, forbidden) {
			t.Errorf("response body contains forbidden field %q: %s", forbidden, body)
		}
	}
}

// TestBrowserSessionListHandlerCrossSubjectIsolation proves that caller A
// cannot see caller B's sessions. The fake is keyed by subject — B has rows,
// A has none — and we assert A gets an empty list AND the handler forwarded A's
// subject hash (not B's) to the store.
func TestBrowserSessionListHandlerCrossSubjectIsolation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	// Seed sessions for subject-hash-b only.
	store := &fakeBrowserSessionListStore{
		sessionsBySubject: map[string][]BrowserSessionListItem{
			"subject-hash-b": {
				{
					IssuedAt:          now.Add(-1 * time.Hour),
					LastSeenAt:        now,
					IdleExpiresAt:     now.Add(30 * time.Minute),
					AbsoluteExpiresAt: now.Add(12 * time.Hour),
					TenantID:          "tenant_b",
					WorkspaceID:       "workspace_b",
				},
			},
		},
	}
	handler := &BrowserSessionListHandler{
		Store: store,
		Now:   func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// Caller authenticates as subject-hash-a.
	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/sessions", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		SubjectIDHash: "subject-hash-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Assert the handler forwarded A's subject, not B's.
	if store.capturedSubject != "subject-hash-a" {
		t.Errorf("handler forwarded subject %q, want subject-hash-a", store.capturedSubject)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sessions, _ := resp["sessions"].([]any)
	// A has no sessions — B's session must not leak to A.
	if len(sessions) != 0 {
		t.Fatalf("cross-subject isolation failed: A received %d sessions (B's data)", len(sessions))
	}
}
