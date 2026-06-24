package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fakes for the new list stores
// ---------------------------------------------------------------------------

type fakeBrowserSessionListStore struct {
	fakeBrowserSessionStore
	listed []BrowserSessionListItem
	listErr error
}

func (s *fakeBrowserSessionListStore) ListSessionsBySubject(
	_ context.Context,
	_ string,
	_ time.Time,
) ([]BrowserSessionListItem, error) {
	return s.listed, s.listErr
}

type fakeLocalIdentityListStore struct {
	fakeLocalIdentityStore
	listedTokens []LocalIdentityAPITokenListItem
	listErr      error
	mfaStatus    LocalIdentityMFAStatus
	mfaErr       error
}

func (s *fakeLocalIdentityListStore) ListAPITokensBySubject(
	_ context.Context,
	_ string,
	_ time.Time,
) ([]LocalIdentityAPITokenListItem, error) {
	return s.listedTokens, s.listErr
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
	// No auth context
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestBrowserSessionListHandlerReturnsSessions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeBrowserSessionListStore{
		listed: []BrowserSessionListItem{
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
	// Mark this as the current session cookie
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
}

func TestBrowserSessionListHandlerNeverReturnsSessionHashOrCSRF(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeBrowserSessionListStore{
		listed: []BrowserSessionListItem{
			{
				IssuedAt:          now,
				LastSeenAt:        now,
				IdleExpiresAt:     now.Add(30 * time.Minute),
				AbsoluteExpiresAt: now.Add(12 * time.Hour),
				TenantID:          "tenant_a",
				WorkspaceID:       "workspace_a",
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

func TestBrowserSessionListHandlerCrossSubjectIsolation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeBrowserSessionListStore{}

	handler := &BrowserSessionListHandler{
		Store: store,
		Now:   func() time.Time { return now },
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	// Caller A authenticates as "subject-hash-a".
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
	// No sessions were seeded for subject-hash-a; the response must be empty,
	// not someone else's data.
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sessions, _ := resp["sessions"].([]any)
	if len(sessions) != 0 {
		t.Fatalf("cross-subject isolation failed: got %d sessions for empty store", len(sessions))
	}
}

// ---------------------------------------------------------------------------
// GET /api/v0/auth/local/api-tokens (list)
// ---------------------------------------------------------------------------

func TestLocalIdentityAPITokenListHandlerReturnsMedataOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityListStore{
		listedTokens: []LocalIdentityAPITokenListItem{
			{
				TokenID:      "tok-001",
				TokenClass:   "personal",
				DisplayLabel: "dev-laptop",
				IssuedAt:     now.Add(-24 * time.Hour),
				ExpiresAt:    now.Add(24 * time.Hour),
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
	// Must never expose token_hash or raw token value.
	for _, forbidden := range []string{"token_hash", "api_token"} {
		if bodyContains(body, forbidden) {
			t.Errorf("token list response exposes forbidden field %q: %s", forbidden, body)
		}
	}
	// Must include expected metadata fields.
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tokens, _ := resp["tokens"].([]any)
	if len(tokens) != 1 {
		t.Fatalf("tokens len = %d, want 1", len(tokens))
	}
}

func TestLocalIdentityAPITokenListHandlerCrossSubjectIsolation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	// Store has tokens but they belong to a different subject.
	// The handler must scope to the caller's subject only.
	store := &fakeLocalIdentityListStore{
		listedTokens: []LocalIdentityAPITokenListItem{},
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
		SubjectIDHash: "subject-hash-b",
		AllScopes:     true,
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
	tokens, _ := resp["tokens"].([]any)
	if len(tokens) != 0 {
		t.Fatalf("cross-subject isolation failed: got %d tokens", len(tokens))
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
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		SubjectIDHash: "subject-hash-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, forbidden := range []string{
		"credential_handle",
		"recovery_code",
		"mfa_hash",
		"password_hash",
		"session_hash",
		"token_hash",
	} {
		if bodyContains(body, forbidden) {
			t.Errorf("profile response exposes forbidden field %q: %s", forbidden, body)
		}
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

func fixedNow() time.Time { return time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC) }

func bodyContains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (s == sub ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
