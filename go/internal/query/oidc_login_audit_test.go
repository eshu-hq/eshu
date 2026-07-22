// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// TestOIDCLoginHandlerCallbackAuditsSuccessfulLogin proves issue #5601's fix
// for the OIDC path: a successful callback now writes a durable
// identity_authentication governance-audit row.
func TestOIDCLoginHandlerCallbackAuditsSuccessfulLogin(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 21, 11, 0, 0, 0, time.UTC)
	audit := &fakeGovernanceAuditAppender{}
	service := &fakeOIDCLoginService{
		complete: OIDCLoginCompleteResponse{
			Auth: AuthContext{
				Mode:          AuthModeScoped,
				TenantID:      "tenant_a",
				WorkspaceID:   "workspace_a",
				SubjectClass:  "external_oidc_user",
				SubjectIDHash: "sha256:abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
				RoleIDs:       []string{"developer"},
			},
			ProviderConfigID:  "okta-dev",
			ProviderSubjectID: "sha256:abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
			ProviderProofAt:   now.Add(-time.Minute),
		},
	}
	handler := &OIDCLoginHandler{
		Service: service,
		Audit:   audit,
		SessionIssuer: &BrowserSessionHandler{
			Store:           &fakeBrowserSessionStore{},
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
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1: %#v", len(audit.events), audit.events)
	}
	event := audit.events[0]
	if event.Type != governanceaudit.EventTypeIdentityAuthentication {
		t.Fatalf("event type = %q, want identity_authentication", event.Type)
	}
	if event.Decision != governanceaudit.DecisionAllowed || event.ReasonCode != "sso_login_authenticated" {
		t.Fatalf("decision/reason = %q/%q, want allowed/sso_login_authenticated", event.Decision, event.ReasonCode)
	}
	if event.ActorIDHash != "sha256:abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234" {
		t.Fatalf("actor id hash = %q, want the hashed external subject", event.ActorIDHash)
	}
	if event.TenantID != "tenant_a" {
		t.Fatalf("tenant id = %q, want tenant_a", event.TenantID)
	}
	if event.WorkspaceID != "workspace_a" {
		t.Fatalf("workspace id = %q, want workspace_a", event.WorkspaceID)
	}
}

// TestOIDCLoginHandlerCallbackAuditsDeniedLogin proves the denial half: an
// OIDC callback that resolves no group->role grant is audited as denied
// with the "no_grants" classification (issue #5601's suggested reason code)
// instead of silently returning 403 with zero trace.
func TestOIDCLoginHandlerCallbackAuditsDeniedLogin(t *testing.T) {
	t.Parallel()

	audit := &fakeGovernanceAuditAppender{}
	service := &fakeOIDCLoginService{
		completeErr: &SSOLoginDeniedError{Sentinel: ErrOIDCLoginDenied, Reason: "no_grants"},
	}
	handler := &OIDCLoginHandler{
		Service: service,
		Audit:   audit,
		SessionIssuer: &BrowserSessionHandler{
			Store: &fakeBrowserSessionStore{},
			Now:   func() time.Time { return time.Date(2026, 7, 21, 11, 5, 0, 0, time.UTC) },
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/callback?state=state-secret&code=auth-code", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1: %#v", len(audit.events), audit.events)
	}
	if got := audit.events[0].Decision; got != governanceaudit.DecisionDenied {
		t.Fatalf("decision = %q, want denied", got)
	}
	if got := audit.events[0].ReasonCode; got != "no_grants" {
		t.Fatalf("reason code = %q, want no_grants", got)
	}
	if got := audit.events[0].ActorIDHash; got != "" {
		t.Fatalf("actor id hash = %q, want empty", got)
	}
}

// TestOIDCLoginHandlerCallbackAuditsUnclassifiedCompleteError mirrors the
// GitHub-path equivalent (P1a/P1b review fix): an error CompleteOIDCLogin
// returns that is neither ErrOIDCLoginUnavailable nor ErrOIDCLoginDenied — a
// raw, unwrapped ErrOIDCLoginInvalidRequest is the example used here — is
// still audited via auditOIDCSSOLogin's default case, as denied with reason
// "sso_login_error", instead of silently recording nothing.
func TestOIDCLoginHandlerCallbackAuditsUnclassifiedCompleteError(t *testing.T) {
	t.Parallel()

	audit := &fakeGovernanceAuditAppender{}
	service := &fakeOIDCLoginService{completeErr: ErrOIDCLoginInvalidRequest}
	handler := &OIDCLoginHandler{
		Service: service,
		Audit:   audit,
		SessionIssuer: &BrowserSessionHandler{
			Store: &fakeBrowserSessionStore{},
			Now:   func() time.Time { return time.Date(2026, 7, 21, 11, 10, 0, 0, time.UTC) },
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/callback?state=state-secret&code=auth-code", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1 for an unclassified CompleteOIDCLogin error: %#v", len(audit.events), audit.events)
	}
	event := audit.events[0]
	if event.Decision != governanceaudit.DecisionDenied {
		t.Fatalf("decision = %q, want denied", event.Decision)
	}
	if event.ReasonCode != "sso_login_error" {
		t.Fatalf("reason code = %q, want sso_login_error", event.ReasonCode)
	}
}

// TestOIDCLoginHandlerCallbackAuditsUnavailableLogin proves a technical
// failure downstream of state validation (e.g. grant resolution unreachable)
// is audited as unavailable, distinct from a denied login — the OIDC mirror
// of TestGitHubLoginHandlerCallbackAuditsUnavailableLogin.
func TestOIDCLoginHandlerCallbackAuditsUnavailableLogin(t *testing.T) {
	t.Parallel()

	audit := &fakeGovernanceAuditAppender{}
	service := &fakeOIDCLoginService{
		completeErr: &SSOLoginDeniedError{Sentinel: ErrOIDCLoginUnavailable, Reason: "grant_resolution_unavailable"},
	}
	handler := &OIDCLoginHandler{
		Service: service,
		Audit:   audit,
		SessionIssuer: &BrowserSessionHandler{
			Store: &fakeBrowserSessionStore{},
			Now:   func() time.Time { return time.Date(2026, 7, 21, 10, 10, 0, 0, time.UTC) },
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/oidc/callback?state=state-secret&code=auth-code", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1: %#v", len(audit.events), audit.events)
	}
	if got := audit.events[0].Decision; got != governanceaudit.DecisionUnavailable {
		t.Fatalf("decision = %q, want unavailable", got)
	}
	if got := audit.events[0].ReasonCode; got != "grant_resolution_unavailable" {
		t.Fatalf("reason code = %q, want grant_resolution_unavailable", got)
	}
}

// TestOIDCLoginHandlerCallbackNilAuditIsSafe proves a handler with no Audit
// wired (e.g. an older deployment) never panics, mirroring
// TestGitHubLoginHandlerCallbackNilAuditIsSafe. nil Audit is a valid
// deployment configuration (adminRecoveryAuditAppender can return nil), and
// recordSSOLoginAuthentication already guards on it.
func TestOIDCLoginHandlerCallbackNilAuditIsSafe(t *testing.T) {
	t.Parallel()

	service := &fakeOIDCLoginService{
		complete: OIDCLoginCompleteResponse{
			Auth:              AuthContext{Mode: AuthModeScoped, TenantID: "tenant_a", WorkspaceID: "workspace_a"},
			ProviderSubjectID: "sha256:abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
		},
	}
	handler := &OIDCLoginHandler{
		Service: service,
		SessionIssuer: &BrowserSessionHandler{
			Store: &fakeBrowserSessionStore{},
			Now:   func() time.Time { return time.Date(2026, 7, 21, 11, 20, 0, 0, time.UTC) },
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
}
