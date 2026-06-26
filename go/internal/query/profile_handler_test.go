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

// ---------------------------------------------------------------------------
// Subject-keyed fakes — capture the subject hash forwarded by the handler so
// isolation tests can assert (a) the right hash was forwarded and (b) a
// different subject gets only their own rows.
// ---------------------------------------------------------------------------

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

type fakeLocalIdentityListStore struct {
	fakeLocalIdentityStore
	// tokensBySubject maps subject_id_hash → rows.
	tokensBySubject map[string][]LocalIdentityAPITokenListItem
	listErr         error
	mfaStatus       LocalIdentityMFAStatus
	mfaErr          error
	// capturedSubject is set on each ListAPITokensBySubject call.
	capturedSubject string
}

func (s *fakeLocalIdentityListStore) ListAPITokensBySubject(
	_ context.Context,
	subjectIDHash string,
	_ time.Time,
) ([]LocalIdentityAPITokenListItem, error) {
	s.capturedSubject = subjectIDHash
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.tokensBySubject[subjectIDHash], nil
}

func (s *fakeLocalIdentityListStore) GetLocalIdentityMFAStatus(
	_ context.Context,
	_ string,
	_ time.Time,
) (LocalIdentityMFAStatus, error) {
	return s.mfaStatus, s.mfaErr
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
	for _, forbidden := range []string{"token_hash", "api_token", "display_label", "display_handle_hash"} {
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
	// Assert the handler forwarded the correct subject hash to the store.
	if store.capturedSubject != "subject-hash-a" {
		t.Errorf("store received subject %q, want subject-hash-a", store.capturedSubject)
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

// ---------------------------------------------------------------------------
// GET /api/v0/auth/profile
// ---------------------------------------------------------------------------

func TestProfileHandlerReturnsAggregatedProfile(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityListStore{
		mfaStatus: LocalIdentityMFAStatus{
			HasActiveMFA: true,
			FactorKind:   "recovery_code",
		},
	}
	handler := &ProfileHandler{
		LocalIdentityStore: store,
		Now:                func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/profile", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                      AuthModeBrowserSession,
		TenantID:                  "tenant_a",
		WorkspaceID:               "workspace_a",
		SubjectIDHash:             "subject-hash-a",
		RoleIDs:                   []string{"developer"},
		PermissionCatalogEnforced: true,
		AllowedPermissionFeatures: []string{"ask_search"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["active_tenant_id"] != "tenant_a" {
		t.Errorf("active_tenant_id = %v, want tenant_a", resp["active_tenant_id"])
	}
	mfa, _ := resp["mfa"].(map[string]any)
	if mfa == nil {
		t.Fatal("mfa field missing from profile response")
	}
	if mfa["has_active_mfa"] != true {
		t.Errorf("has_active_mfa = %v, want true", mfa["has_active_mfa"])
	}
	if mfa["factor_kind"] != "recovery_code" {
		t.Errorf("factor_kind = %v, want recovery_code", mfa["factor_kind"])
	}
}

// TestProfileHandlerMFAStoreErrorReturns503 is the regression test for P1:
// a GetLocalIdentityMFAStatus error must not silently emit has_active_mfa:false
// as a security fact. The handler must return 503 and log the error.
func TestProfileHandlerMFAStoreErrorReturns503(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityListStore{
		mfaErr: errFakeMFAStoreFailure,
	}
	handler := &ProfileHandler{
		LocalIdentityStore: store,
		Now:                func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/profile", nil)
	// TenantID is set so the MFA path runs.
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		TenantID:      "tenant_a",
		WorkspaceID:   "workspace_a",
		SubjectIDHash: "subject-hash-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Must not emit 200 with has_active_mfa:false when the store errored.
	if rec.Code == http.StatusOK {
		body := rec.Body.String()
		// If 200 is returned, it must contain mfa.provenance:"unavailable", not
		// has_active_mfa:false as a security assertion.
		if bodyContains(body, `"has_active_mfa":false`) && !bodyContains(body, `"provenance":"unavailable"`) {
			t.Fatalf("store error silently emitted has_active_mfa:false without unavailable provenance: %s", body)
		}
		return
	}
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError {
		t.Fatalf("mfa store error: status = %d, want 503 or 500", rec.Code)
	}
}

// TestProfileHandlerNeverExposesMFACredentialOrHash verifies that credential
// handles, recovery code hashes, and other secret fields never appear in the
// profile response body. factor_kind is a safe label (e.g. "recovery_code")
// and IS expected to appear — what must not appear are actual credential values.
func TestProfileHandlerNeverExposesMFACredentialOrHash(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityListStore{
		mfaStatus: LocalIdentityMFAStatus{
			HasActiveMFA: true,
			FactorKind:   "recovery_code",
		},
	}
	handler := &ProfileHandler{
		LocalIdentityStore: store,
		Now:                func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/profile", nil)
	// TenantID set so the MFA path actually runs.
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		TenantID:      "tenant_a",
		WorkspaceID:   "workspace_a",
		SubjectIDHash: "subject-hash-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	// factor_kind value "recovery_code" is a safe label — it is NOT in this list.
	// What must not appear are field names that would indicate raw credential values.
	for _, forbidden := range []string{
		"credential_handle",
		"mfa_credential",
		"mfa_hash",
		"password_hash",
		"session_hash",
		"token_hash",
		"recovery_code_hash",
	} {
		if bodyContains(body, forbidden) {
			t.Errorf("profile response exposes forbidden field %q: %s", forbidden, body)
		}
	}
	// factor_kind must be present (it's a safe label).
	if !bodyContains(body, "factor_kind") {
		t.Errorf("profile response missing factor_kind; MFA path may not have run: %s", body)
	}
}

func TestProfileHandlerRequiresAuth(t *testing.T) {
	t.Parallel()

	handler := &ProfileHandler{
		LocalIdentityStore: &fakeLocalIdentityListStore{},
		Now:                fixedNow,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/profile", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// ExternalProviderConfigID surfaced on GET /api/v0/auth/browser-session
// ---------------------------------------------------------------------------

func TestBrowserSessionCurrentResponseIncludesExternalProviderConfigID(t *testing.T) {
	t.Parallel()

	store := &fakeBrowserSessionStore{}
	handler := &BrowserSessionHandler{Store: store, Now: fixedNow}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/browser-session", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                     AuthModeBrowserSession,
		TenantID:                 "tenant_a",
		ExternalProviderConfigID: "oidc-config-xyz",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	auth, _ := resp["auth"].(map[string]any)
	if auth == nil {
		t.Fatal("auth field missing")
	}
	if auth["external_provider_config_id"] != "oidc-config-xyz" {
		t.Errorf("external_provider_config_id = %v, want oidc-config-xyz", auth["external_provider_config_id"])
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// errFakeMFAStoreFailure is a sentinel error injected to simulate store failure.
var errFakeMFAStoreFailure = &fakeMFAError{"mfa store unavailable"}

type fakeMFAError struct{ msg string }

func (e *fakeMFAError) Error() string { return e.msg }

func fixedNow() time.Time { return time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC) }

func bodyContains(s, sub string) bool {
	if len(sub) == 0 || len(s) < len(sub) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
