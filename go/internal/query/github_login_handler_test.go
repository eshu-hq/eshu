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

// TestGitHubLoginHandlerCallbackAuditsSuccessfulLogin proves issue #5601's
// fix: a successful GitHub SSO callback now writes a durable
// identity_authentication governance-audit row (previously: none at all —
// the only trace was the browser_sessions row, which expires).
func TestGitHubLoginHandlerCallbackAuditsSuccessfulLogin(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessionStore := &fakeBrowserSessionStore{}
	audit := &fakeGovernanceAuditAppender{}
	service := &fakeGitHubLoginService{
		complete: GitHubLoginCompleteResponse{
			Auth: AuthContext{
				Mode:          AuthModeScoped,
				TenantID:      "tenant_a",
				WorkspaceID:   "workspace_a",
				SubjectClass:  "external_github_user",
				SubjectIDHash: "sha256:subject",
				RoleIDs:       []string{"developer"},
			},
			ProviderConfigID:  "github-dev",
			ProviderSubjectID: "sha256:subject",
			ProviderProofAt:   now.Add(-time.Minute),
		},
	}
	handler := &GitHubLoginHandler{
		Service: service,
		Audit:   audit,
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

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/github/callback?state=state-secret&code=auth-code", nil)
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
	if event.Decision != governanceaudit.DecisionAllowed {
		t.Fatalf("decision = %q, want allowed", event.Decision)
	}
	if event.ReasonCode != "sso_login_authenticated" {
		t.Fatalf("reason code = %q, want sso_login_authenticated", event.ReasonCode)
	}
	if event.ActorIDHash != "sha256:subject" {
		t.Fatalf("actor id hash = %q, want the hashed external subject", event.ActorIDHash)
	}
	if event.ActorClass != governanceaudit.ActorClassOperator {
		t.Fatalf("actor class = %q, want operator", event.ActorClass)
	}
	if event.TenantID != "tenant_a" {
		t.Fatalf("tenant id = %q, want tenant_a", event.TenantID)
	}
	if event.WorkspaceID != "workspace_a" {
		t.Fatalf("workspace id = %q, want workspace_a", event.WorkspaceID)
	}
}

// TestGitHubLoginHandlerCallbackAuditsDeniedLogin proves the second half of
// issue #5601: a denied GitHub SSO callback (e.g. the user is outside every
// allowed org) now writes a denied identity_authentication row carrying the
// classification, matching the local-login precedent — previously only a
// slog.WarnContext line existed, with no queryable audit trail.
func TestGitHubLoginHandlerCallbackAuditsDeniedLogin(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 21, 10, 5, 0, 0, time.UTC)
	audit := &fakeGovernanceAuditAppender{}
	service := &fakeGitHubLoginService{
		completeErr: &SSOLoginDeniedError{Sentinel: ErrGitHubLoginDenied, Reason: "org_not_allowed"},
	}
	handler := &GitHubLoginHandler{
		Service: service,
		Audit:   audit,
		SessionIssuer: &BrowserSessionHandler{
			Store: &fakeBrowserSessionStore{},
			Now:   func() time.Time { return now },
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/github/callback?state=state-secret&code=auth-code", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1: %#v", len(audit.events), audit.events)
	}
	event := audit.events[0]
	if event.Decision != governanceaudit.DecisionDenied {
		t.Fatalf("decision = %q, want denied", event.Decision)
	}
	if event.ReasonCode != "org_not_allowed" {
		t.Fatalf("reason code = %q, want org_not_allowed", event.ReasonCode)
	}
	if event.ActorIDHash != "" {
		t.Fatalf("actor id hash = %q, want empty (no identity resolved before denial)", event.ActorIDHash)
	}
	if event.ActorClass != governanceaudit.ActorClassAnonymous {
		t.Fatalf("actor class = %q, want anonymous", event.ActorClass)
	}
}

// TestGitHubLoginHandlerCallbackAuditsUnavailableLogin proves a technical
// failure downstream of state validation (e.g. grant resolution unreachable)
// is audited as unavailable, distinct from a denied login.
func TestGitHubLoginHandlerCallbackAuditsUnavailableLogin(t *testing.T) {
	t.Parallel()

	audit := &fakeGovernanceAuditAppender{}
	service := &fakeGitHubLoginService{
		completeErr: &SSOLoginDeniedError{Sentinel: ErrGitHubLoginUnavailable, Reason: "grant_resolution_unavailable"},
	}
	handler := &GitHubLoginHandler{
		Service: service,
		Audit:   audit,
		SessionIssuer: &BrowserSessionHandler{
			Store: &fakeBrowserSessionStore{},
			Now:   func() time.Time { return time.Date(2026, 7, 21, 10, 10, 0, 0, time.UTC) },
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/github/callback?state=state-secret&code=auth-code", nil)
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

// TestGitHubLoginHandlerCallbackDoesNotAuditMalformedRequest mirrors
// LocalIdentityHandler not auditing a request-parsing failure: a malformed
// callback (unknown state/code combination that never reaches an
// authentication attempt) is not a completed login attempt and is not
// audited.
func TestGitHubLoginHandlerCallbackDoesNotAuditMalformedRequest(t *testing.T) {
	t.Parallel()

	audit := &fakeGovernanceAuditAppender{}
	service := &fakeGitHubLoginService{completeErr: ErrGitHubLoginInvalidRequest}
	handler := &GitHubLoginHandler{
		Service: service,
		Audit:   audit,
		SessionIssuer: &BrowserSessionHandler{
			Store: &fakeBrowserSessionStore{},
			Now:   func() time.Time { return time.Date(2026, 7, 21, 10, 15, 0, 0, time.UTC) },
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/github/callback?state=state-secret&code=auth-code", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(audit.events) != 0 {
		t.Fatalf("audit events = %d, want 0 for a malformed callback: %#v", len(audit.events), audit.events)
	}
}

// TestGitHubLoginHandlerCallbackNilAuditIsSafe proves a handler with no
// Audit wired (e.g. an older deployment or a unit test) never panics.
func TestGitHubLoginHandlerCallbackNilAuditIsSafe(t *testing.T) {
	t.Parallel()

	service := &fakeGitHubLoginService{
		complete: GitHubLoginCompleteResponse{
			Auth:              AuthContext{Mode: AuthModeScoped, TenantID: "tenant_a", WorkspaceID: "workspace_a"},
			ProviderSubjectID: "sha256:subject",
		},
	}
	handler := &GitHubLoginHandler{
		Service: service,
		SessionIssuer: &BrowserSessionHandler{
			Store: &fakeBrowserSessionStore{},
			Now:   func() time.Time { return time.Date(2026, 7, 21, 10, 20, 0, 0, time.UTC) },
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/github/callback?state=state-secret&code=auth-code", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

type fakeGitHubLoginService struct {
	start       GitHubLoginStartResponse
	startErr    error
	startReq    GitHubLoginStartRequest
	complete    GitHubLoginCompleteResponse
	completeErr error
	completeReq GitHubLoginCompleteRequest
}

func (s *fakeGitHubLoginService) StartGitHubLogin(
	_ context.Context,
	req GitHubLoginStartRequest,
) (GitHubLoginStartResponse, error) {
	s.startReq = req
	return s.start, s.startErr
}

func (s *fakeGitHubLoginService) CompleteGitHubLogin(
	_ context.Context,
	req GitHubLoginCompleteRequest,
) (GitHubLoginCompleteResponse, error) {
	s.completeReq = req
	return s.complete, s.completeErr
}
