// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

func TestAuthMiddlewareWithBrowserSessionsAttachesAuthContextFromCookie(t *testing.T) {
	t.Parallel()

	resolver := &fakeBrowserSessionResolver{
		context: AuthContext{
			Mode:                 AuthModeBrowserSession,
			TenantID:             "tenant_a",
			WorkspaceID:          "workspace_a",
			SubjectClass:         "local_user",
			SubjectIDHash:        "sha256:subject",
			PolicyRevisionHash:   "sha256:policy",
			AllowedScopeIDs:      []string{"scope_a"},
			AllowedRepositoryIDs: []string{"repo_a"},
		},
		ok: true,
	}
	handler := AuthMiddlewareWithBrowserSessionsAndScopedTokens("", nil, resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, ok := AuthContextFromContext(r.Context())
		if !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		if auth.Mode != AuthModeBrowserSession || auth.TenantID != "tenant_a" ||
			auth.WorkspaceID != "workspace_a" || auth.SubjectClass != "local_user" {
			t.Fatalf("auth context = %#v, want browser-session subject", auth)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := resolver.sessionHash, BrowserSessionSecretHash("session-secret"); got != want {
		t.Fatalf("resolver session hash = %q, want %q", got, want)
	}
	if resolver.requireCSRF {
		t.Fatal("requireCSRF = true for GET, want false")
	}
}

// TestAuthMiddlewareWithBrowserSessionsDevModeSkipsAuthWhenTokenEmpty proves
// that a browser-session-resolver-only deployment (no shared key, no
// scoped-token file, no OIDC bearer audience) keeps headerless reads OPEN.
// The legacy AuthMiddlewareWithBrowserSessionsAndScopedTokens constructor
// derives enforcement from the shared key alone, and the enforcement
// predicate deliberately excludes the browser-session resolver: the cookie
// path self-enforces before the dev-open branch, so a cookieless headerless
// request in the open posture stays open. See the Fable auth-gate ruling and
// auth_enforcement_wiring_test.go in cmd/api for the real-composition proof.
func TestAuthMiddlewareWithBrowserSessionsDevModeSkipsAuthWhenTokenEmpty(t *testing.T) {
	t.Parallel()

	resolver := &fakeBrowserSessionResolver{err: context.Canceled}
	handler := AuthMiddlewareWithBrowserSessionsAndScopedTokens("", nil, resolver, mockHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if resolver.called {
		t.Fatal("browser session resolver called for empty-token dev-mode request")
	}
}

func TestAuthMiddlewareWithBrowserSessionsRequiresCSRFHeaderForUnsafeCookieRequests(t *testing.T) {
	t.Parallel()

	resolver := &fakeBrowserSessionResolver{err: ErrBrowserSessionCSRFInvalid}
	handler := AuthMiddlewareWithBrowserSessionsAndScopedTokens("", nil, resolver, mockHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/browser-session/context", nil)
	req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if !resolver.called {
		t.Fatal("session resolver was not called")
	}
	if !resolver.requireCSRF {
		t.Fatal("requireCSRF = false, want true")
	}
	if resolver.csrfTokenHash != "" {
		t.Fatalf("csrf token hash = %q, want empty for missing header", resolver.csrfTokenHash)
	}
}

func TestAuthMiddlewareWithBrowserSessionsDeniesRefreshRequiredSession(t *testing.T) {
	t.Parallel()

	resolver := &fakeBrowserSessionResolver{err: ErrBrowserSessionRefreshRequired}
	audit := &fakeGovernanceAuditAppender{}
	handler := AuthMiddlewareWithBrowserSessionsScopedTokensAndGovernanceAudit(
		"shared-token",
		nil,
		resolver,
		mockHandler(),
		audit,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(audit.events))
	}
	event := audit.events[0]
	if got, want := event.Type, governanceaudit.EventTypeReadAuthorization; got != want {
		t.Fatalf("event type = %q, want %q", got, want)
	}
	if got, want := event.ReasonCode, "oidc_session_reauth_required"; got != want {
		t.Fatalf("event reason = %q, want %q", got, want)
	}
}

func TestAuthMiddlewareWithBrowserSessionsRevokesStaleSessionBeforeMissingCSRF(t *testing.T) {
	t.Parallel()

	resolver := &fakeBrowserSessionResolver{err: ErrBrowserSessionRefreshRequired}
	audit := &fakeGovernanceAuditAppender{}
	handler := AuthMiddlewareWithBrowserSessionsScopedTokensAndGovernanceAudit(
		"shared-token",
		nil,
		resolver,
		mockHandler(),
		audit,
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/browser-session/context", nil)
	req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if !resolver.requireCSRF {
		t.Fatal("requireCSRF = false, want true")
	}
	if resolver.csrfTokenHash != "" {
		t.Fatalf("csrf token hash = %q, want empty for missing header", resolver.csrfTokenHash)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(audit.events))
	}
	if got, want := audit.events[0].ReasonCode, "oidc_session_reauth_required"; got != want {
		t.Fatalf("event reason = %q, want %q", got, want)
	}
}

func TestAuthMiddlewareWithBrowserSessionsHashesCSRFHeaderForUnsafeCookieRequests(t *testing.T) {
	t.Parallel()

	resolver := &fakeBrowserSessionResolver{ok: true}
	handler := AuthMiddlewareWithBrowserSessionsAndScopedTokens("", nil, resolver, mockHandler())

	req := httptest.NewRequest(http.MethodPatch, "/api/v0/auth/browser-session/context", nil)
	req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
	req.Header.Set(BrowserSessionCSRFHeaderName, "csrf-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if !resolver.requireCSRF {
		t.Fatal("requireCSRF = false, want true")
	}
	if got, want := resolver.csrfTokenHash, BrowserSessionSecretHash("csrf-secret"); got != want {
		t.Fatalf("resolver csrf hash = %q, want %q", got, want)
	}
}

func TestAuthMiddlewareWithBrowserSessionsAllowsLocalIdentityAPITokenLifecycleRoute(t *testing.T) {
	t.Parallel()

	resolver := &fakeBrowserSessionResolver{
		context: AuthContext{
			Mode:        AuthModeBrowserSession,
			TenantID:    "tenant_a",
			WorkspaceID: "workspace_a",
			AllScopes:   true,
		},
		ok: true,
	}
	handler := AuthMiddlewareWithBrowserSessionsAndScopedTokens("", nil, resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, ok := AuthContextFromContext(r.Context())
		if !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		if auth.TenantID != "tenant_a" || auth.WorkspaceID != "workspace_a" || !auth.AllScopes {
			t.Fatalf("auth context = %#v, want all-scope browser-session context", auth)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/api-tokens/token-old/revoke", nil)
	req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
	req.Header.Set(BrowserSessionCSRFHeaderName, "csrf-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if !resolver.requireCSRF {
		t.Fatal("requireCSRF = false, want true")
	}
}

func TestAuthMiddlewareWithBrowserSessionsKeepsBearerTokensSeparateFromCSRF(t *testing.T) {
	t.Parallel()

	resolver := &fakeBrowserSessionResolver{ok: true}
	handler := AuthMiddlewareWithBrowserSessionsAndScopedTokens("shared-token", nil, resolver, mockHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer shared-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if resolver.called {
		t.Fatal("browser session resolver called for bearer-token request")
	}
}

type fakeBrowserSessionResolver struct {
	context AuthContext
	ok      bool
	err     error

	called        bool
	sessionHash   string
	csrfTokenHash string
	requireCSRF   bool
	asOf          time.Time
}

func (f *fakeBrowserSessionResolver) ResolveBrowserSession(
	_ context.Context,
	sessionHash string,
	csrfTokenHash string,
	requireCSRF bool,
	asOf time.Time,
) (AuthContext, bool, error) {
	f.called = true
	f.sessionHash = sessionHash
	f.csrfTokenHash = csrfTokenHash
	f.requireCSRF = requireCSRF
	f.asOf = asOf
	return f.context, f.ok, f.err
}
