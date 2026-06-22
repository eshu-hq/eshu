package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBrowserSessionHandlerCreateSetsSecureHashOnlyCookies(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 18, 0, 0, 0, time.UTC)
	store := &fakeBrowserSessionStore{}
	handler := &BrowserSessionHandler{
		Store:           store,
		NewSecret:       sequenceSecrets("session-secret", "csrf-secret"),
		Now:             func() time.Time { return now },
		IdleTimeout:     30 * time.Minute,
		AbsoluteTimeout: 12 * time.Hour,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/browser-session", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant_a",
		WorkspaceID:          "workspace_a",
		SubjectClass:         "human",
		SubjectIDHash:        "sha256:subject",
		PolicyRevisionHash:   "sha256:policy",
		AllowedScopeIDs:      []string{"scope_a"},
		AllowedRepositoryIDs: []string{"repo_a"},
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if len(store.created) != 1 {
		t.Fatalf("created sessions = %d, want 1", len(store.created))
	}
	created := store.created[0]
	if got, want := created.SessionHash, BrowserSessionSecretHash("session-secret"); got != want {
		t.Fatalf("session hash = %q, want %q", got, want)
	}
	if got, want := created.CSRFTokenHash, BrowserSessionSecretHash("csrf-secret"); got != want {
		t.Fatalf("csrf hash = %q, want %q", got, want)
	}
	if created.SessionHash == "session-secret" || created.CSRFTokenHash == "csrf-secret" {
		t.Fatalf("created session leaked raw secrets: %#v", created)
	}
	if got, want := created.IdleExpiresAt, now.Add(30*time.Minute); !got.Equal(want) {
		t.Fatalf("idle expiry = %v, want %v", got, want)
	}
	if got, want := created.AbsoluteExpiresAt, now.Add(12*time.Hour); !got.Equal(want) {
		t.Fatalf("absolute expiry = %v, want %v", got, want)
	}

	sessionCookie := requireCookie(t, rec.Result(), BrowserSessionCookieName)
	if sessionCookie.Value != "session-secret" || !sessionCookie.HttpOnly ||
		!sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteStrictMode ||
		sessionCookie.Path != "/" {
		t.Fatalf("session cookie attrs = %#v, want secure HttpOnly Strict host cookie", sessionCookie)
	}
	csrfCookie := requireCookie(t, rec.Result(), BrowserSessionCSRFCookieName)
	if csrfCookie.Value != "csrf-secret" || csrfCookie.HttpOnly ||
		!csrfCookie.Secure || csrfCookie.SameSite != http.SameSiteStrictMode ||
		csrfCookie.Path != "/" {
		t.Fatalf("csrf cookie attrs = %#v, want readable secure Strict host cookie", csrfCookie)
	}
	if strings.Contains(rec.Body.String(), "session-secret") {
		t.Fatalf("response body leaked session secret: %s", rec.Body.String())
	}

	var body BrowserSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if body.CSRFToken != "csrf-secret" {
		t.Fatalf("csrf token = %q, want csrf-secret", body.CSRFToken)
	}
	if body.Auth.Mode != AuthModeBrowserSession ||
		body.Auth.TenantID != "tenant_a" ||
		body.Auth.WorkspaceID != "workspace_a" {
		t.Fatalf("auth response = %#v, want browser session tenant/workspace", body.Auth)
	}
}

func TestBrowserSessionHandlerCreateRejectsExistingBrowserSession(t *testing.T) {
	t.Parallel()

	handler := &BrowserSessionHandler{
		Store:     &fakeBrowserSessionStore{},
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
		Now:       func() time.Time { return time.Date(2026, 6, 21, 18, 30, 0, 0, time.UTC) },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/browser-session", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode: AuthModeBrowserSession,
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestBrowserSessionHandlerCreateAllowsScopedTokenWithoutAuditHashes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 18, 45, 0, 0, time.UTC)
	store := &fakeBrowserSessionStore{}
	handler := &BrowserSessionHandler{
		Store:           store,
		NewSecret:       sequenceSecrets("session-secret", "csrf-secret"),
		Now:             func() time.Time { return now },
		IdleTimeout:     30 * time.Minute,
		AbsoluteTimeout: 12 * time.Hour,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/browser-session", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant_a",
		WorkspaceID: "workspace_a",
		AllScopes:   true,
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if len(store.created) != 1 {
		t.Fatalf("created sessions = %d, want 1", len(store.created))
	}
	created := store.created[0]
	if created.SubjectIDHash != "" || created.SubjectClass != "" || created.PolicyRevisionHash != "" {
		t.Fatalf("created audit metadata = %q/%q/%q, want omitted", created.SubjectIDHash, created.SubjectClass, created.PolicyRevisionHash)
	}
	if created.TenantID != "tenant_a" || created.WorkspaceID != "workspace_a" || !created.AllScopes {
		t.Fatalf("created session = %#v, want tenant/workspace all-scope session", created)
	}
	if strings.Contains(rec.Body.String(), "session-secret") {
		t.Fatalf("response body leaked session secret: %s", rec.Body.String())
	}
}

func TestBrowserSessionHandlerLogoutRevokesHashedSessionAndClearsCookies(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 19, 0, 0, 0, time.UTC)
	store := &fakeBrowserSessionStore{}
	handler := &BrowserSessionHandler{
		Store: store,
		Now:   func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodDelete, "/api/v0/auth/browser-session", nil)
	req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode: AuthModeBrowserSession,
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if got, want := store.revokedHash, BrowserSessionSecretHash("session-secret"); got != want {
		t.Fatalf("revoked hash = %q, want %q", got, want)
	}
	if !store.revokedAt.Equal(now) {
		t.Fatalf("revoked at = %v, want %v", store.revokedAt, now)
	}
	if requireCookie(t, rec.Result(), BrowserSessionCookieName).MaxAge != -1 {
		t.Fatal("session cookie was not cleared")
	}
	if requireCookie(t, rec.Result(), BrowserSessionCSRFCookieName).MaxAge != -1 {
		t.Fatal("csrf cookie was not cleared")
	}
}

func TestBrowserSessionHandlerSwitchesWorkspaceByHashedSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 20, 0, 0, 0, time.UTC)
	store := &fakeBrowserSessionStore{
		switchAuth: AuthContext{
			Mode:               AuthModeBrowserSession,
			TenantID:           "tenant_b",
			WorkspaceID:        "workspace_b",
			SubjectClass:       "human",
			SubjectIDHash:      "sha256:subject",
			PolicyRevisionHash: "sha256:policy_b",
			AllScopes:          true,
		},
		switchOK: true,
	}
	handler := &BrowserSessionHandler{
		Store: store,
		Now:   func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := bytes.NewBufferString(`{"tenant_id":"tenant_b","workspace_id":"workspace_b"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/v0/auth/browser-session/context", body)
	req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode: AuthModeBrowserSession,
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got, want := store.switchSessionHash, BrowserSessionSecretHash("session-secret"); got != want {
		t.Fatalf("switch session hash = %q, want %q", got, want)
	}
	if store.switchTenantID != "tenant_b" || store.switchWorkspaceID != "workspace_b" {
		t.Fatalf("switch target = %q/%q, want tenant_b/workspace_b", store.switchTenantID, store.switchWorkspaceID)
	}

	var response BrowserSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if response.Auth.TenantID != "tenant_b" || response.Auth.WorkspaceID != "workspace_b" {
		t.Fatalf("response auth = %#v, want switched tenant/workspace", response.Auth)
	}
}

type fakeBrowserSessionStore struct {
	created           []BrowserSessionCreateRecord
	revokedHash       string
	revokedAt         time.Time
	switchSessionHash string
	switchTenantID    string
	switchWorkspaceID string
	switchAt          time.Time
	switchAuth        AuthContext
	switchOK          bool
}

func (s *fakeBrowserSessionStore) CreateBrowserSession(
	_ context.Context,
	record BrowserSessionCreateRecord,
) error {
	s.created = append(s.created, record)
	return nil
}

func (s *fakeBrowserSessionStore) RevokeBrowserSession(
	_ context.Context,
	sessionHash string,
	revokedAt time.Time,
) error {
	s.revokedHash = sessionHash
	s.revokedAt = revokedAt
	return nil
}

func (s *fakeBrowserSessionStore) SwitchBrowserSessionWorkspace(
	_ context.Context,
	sessionHash string,
	tenantID string,
	workspaceID string,
	switchedAt time.Time,
) (AuthContext, bool, error) {
	s.switchSessionHash = sessionHash
	s.switchTenantID = tenantID
	s.switchWorkspaceID = workspaceID
	s.switchAt = switchedAt
	return s.switchAuth, s.switchOK, nil
}

func sequenceSecrets(values ...string) func() (string, error) {
	index := 0
	return func() (string, error) {
		value := values[index]
		index++
		return value, nil
	}
}

func requireCookie(t *testing.T, res *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range res.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %q missing in %#v", name, res.Cookies())
	return nil
}
