// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOIDCLoginHandlerStartRedirectsUsingService(t *testing.T) {
	t.Parallel()

	service := &fakeOIDCLoginService{
		start: OIDCLoginStartResponse{RedirectURL: "https://idp.example.test/oauth2/v1/authorize?state=state-secret"},
	}
	handler := &OIDCLoginHandler{Service: service}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/auth/oidc/login?provider_config_id=okta-dev&tenant_id=tenant_a&workspace_id=workspace_a&return_to=/console",
		nil,
	)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got, want := rec.Header().Get("Location"), service.start.RedirectURL; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
	if service.startReq.ProviderConfigID != "okta-dev" ||
		service.startReq.TenantID != "tenant_a" ||
		service.startReq.WorkspaceID != "workspace_a" ||
		service.startReq.ReturnToPath != "/console" {
		t.Fatalf("start request = %#v, want provider/tenant/workspace/return_to", service.startReq)
	}
}

func TestOIDCLoginHandlerCallbackIssuesHashOnlyBrowserSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	sessionStore := &fakeBrowserSessionStore{}
	service := &fakeOIDCLoginService{
		complete: OIDCLoginCompleteResponse{
			Auth: AuthContext{
				Mode:                 AuthModeScoped,
				TenantID:             "tenant_a",
				WorkspaceID:          "workspace_a",
				SubjectClass:         "external_oidc_user",
				SubjectIDHash:        "sha256:subject",
				PolicyRevisionHash:   "sha256:policy",
				RoleIDs:              []string{"developer"},
				AllowedScopeIDs:      []string{"scope_a"},
				AllowedRepositoryIDs: []string{"repo_a"},
			},
			ProviderConfigID:    "okta-dev",
			ProviderSubjectID:   "sha256:subject",
			ProviderGroupHashes: []string{"sha256:group"},
			ProviderProofAt:     now.Add(-time.Minute),
			ReturnToPath:        "/console",
		},
	}
	handler := &OIDCLoginHandler{
		Service: service,
		SessionIssuer: &BrowserSessionHandler{
			Store:           sessionStore,
			NewSecret:       sequenceSecrets("session-secret", "csrf-secret"),
			Now:             func() time.Time { return now },
			IdleTimeout:     30 * time.Minute,
			AbsoluteTimeout: 12 * time.Hour,
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/callback?state=state-secret&code=auth-code", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if got, want := rec.Header().Get("Location"), "/console"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
	if service.completeReq.State != "state-secret" || service.completeReq.Code != "auth-code" {
		t.Fatalf("complete request = %#v, want state/code", service.completeReq)
	}
	if len(sessionStore.created) != 1 {
		t.Fatalf("created sessions = %d, want 1", len(sessionStore.created))
	}
	created := sessionStore.created[0]
	if got, want := created.SessionHash, BrowserSessionSecretHash("session-secret"); got != want {
		t.Fatalf("session hash = %q, want %q", got, want)
	}
	if got, want := created.CSRFTokenHash, BrowserSessionSecretHash("csrf-secret"); got != want {
		t.Fatalf("csrf hash = %q, want %q", got, want)
	}
	if got := created.RoleIDs; len(got) != 1 || got[0] != "developer" {
		t.Fatalf("created role ids = %#v, want [developer]", got)
	}
	if requireCookie(t, rec.Result(), BrowserSessionCookieName).Value != "session-secret" {
		t.Fatal("session cookie was not set from generated secret")
	}
	if requireCookie(t, rec.Result(), BrowserSessionCSRFCookieName).Value != "csrf-secret" {
		t.Fatal("csrf cookie was not set from generated secret")
	}
}

func TestOIDCLoginHandlerCallbackMarksSessionForBoundedReauth(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC)
	sessionStore := &fakeBrowserSessionStore{}
	service := &fakeOIDCLoginService{
		complete: OIDCLoginCompleteResponse{
			Auth: AuthContext{
				Mode:               AuthModeScoped,
				TenantID:           "tenant_a",
				WorkspaceID:        "workspace_a",
				SubjectClass:       "external_oidc_user",
				SubjectIDHash:      "sha256:subject",
				PolicyRevisionHash: "sha256:policy",
				RoleIDs:            []string{"developer"},
				AllowedScopeIDs:    []string{"scope_a"},
			},
			ProviderConfigID:    "okta-dev",
			ProviderSubjectID:   "sha256:subject",
			ProviderGroupHashes: []string{"sha256:group"},
			ProviderProofAt:     now.Add(-time.Minute),
		},
	}
	handler := &OIDCLoginHandler{
		Service:              service,
		SessionRefreshWindow: 15 * time.Minute,
		SessionIssuer: &BrowserSessionHandler{
			Store:           sessionStore,
			NewSecret:       sequenceSecrets("session-secret", "csrf-secret"),
			Now:             func() time.Time { return now },
			IdleTimeout:     30 * time.Minute,
			AbsoluteTimeout: 12 * time.Hour,
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/callback?state=state-secret&code=auth-code", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if len(sessionStore.created) != 1 {
		t.Fatalf("created sessions = %d, want 1", len(sessionStore.created))
	}
	created := sessionStore.created[0]
	if created.ExternalProviderConfigID != "okta-dev" {
		t.Fatalf("external provider config id = %q, want okta-dev", created.ExternalProviderConfigID)
	}
	if created.ExternalSubjectIDHash != "sha256:subject" {
		t.Fatalf("external subject hash = %q, want sha256:subject", created.ExternalSubjectIDHash)
	}
	if got := created.ExternalGroupHashes; len(got) != 1 || got[0] != "sha256:group" {
		t.Fatalf("external group hashes = %#v, want [sha256:group]", got)
	}
	if !created.ExternalAuthValidatedAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("external auth validated at = %v, want %v", created.ExternalAuthValidatedAt, now.Add(-time.Minute))
	}
	if got, want := created.ExternalAuthStaleAfter, now.Add(14*time.Minute); !got.Equal(want) {
		t.Fatalf("external auth stale after = %v, want %v", got, want)
	}
	if !created.AbsoluteExpiresAt.Equal(now.Add(12 * time.Hour)) {
		t.Fatalf("absolute expiry = %v, want independent browser-session timeout", created.AbsoluteExpiresAt)
	}
}

func TestOIDCLoginRoutesArePublicOnlyForExactGETPaths(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/auth/oidc/login", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := AuthMiddleware("shared-token", mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("GET login status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v0/auth/oidc/login", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST login status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/login/extra", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("adjacent login status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

type fakeOIDCLoginService struct {
	start       OIDCLoginStartResponse
	startErr    error
	startReq    OIDCLoginStartRequest
	complete    OIDCLoginCompleteResponse
	completeErr error
	completeReq OIDCLoginCompleteRequest
}

func (s *fakeOIDCLoginService) StartOIDCLogin(
	_ context.Context,
	req OIDCLoginStartRequest,
) (OIDCLoginStartResponse, error) {
	s.startReq = req
	return s.start, s.startErr
}

func (s *fakeOIDCLoginService) CompleteOIDCLogin(
	_ context.Context,
	req OIDCLoginCompleteRequest,
) (OIDCLoginCompleteResponse, error) {
	s.completeReq = req
	return s.complete, s.completeErr
}
