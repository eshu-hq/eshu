// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// failingBrowserSessionStore is a fakeBrowserSessionStore whose create,
// revoke, and switch calls return the matching error when it is set.
type failingBrowserSessionStore struct {
	fakeBrowserSessionStore
	createErr, revokeErr, switchErr error
}

func (s *failingBrowserSessionStore) CreateBrowserSession(ctx context.Context, record BrowserSessionCreateRecord) error {
	if s.createErr != nil {
		return s.createErr
	}
	return s.fakeBrowserSessionStore.CreateBrowserSession(ctx, record)
}

func (s *failingBrowserSessionStore) RevokeBrowserSession(ctx context.Context, hash string, at time.Time) error {
	if s.revokeErr != nil {
		return s.revokeErr
	}
	return s.fakeBrowserSessionStore.RevokeBrowserSession(ctx, hash, at)
}

func (s *failingBrowserSessionStore) SwitchBrowserSessionWorkspace(
	ctx context.Context, hash, tenantID, workspaceID string, at time.Time,
) (AuthContext, bool, error) {
	if s.switchErr != nil {
		return AuthContext{}, false, s.switchErr
	}
	return s.fakeBrowserSessionStore.SwitchBrowserSessionWorkspace(ctx, hash, tenantID, workspaceID, at)
}

// sessionIssuer builds the browser-session handler the session routes and the
// OIDC and GitHub callbacks issue sessions through.
func sessionIssuer(store BrowserSessionStore, newSecret func() (string, error)) *BrowserSessionHandler {
	return &BrowserSessionHandler{Store: store, NewSecret: newSecret}
}

// completedLogin is the auth context a successful SSO callback resolves.
func completedLogin() AuthContext {
	return AuthContext{
		Mode: AuthModeScoped, TenantID: "tenant_a", WorkspaceID: "workspace_a",
		SubjectClass: "external_oidc", SubjectIDHash: "sha256:subject",
	}
}

// TestBrowserSessionStepFailuresAnswerFixedText drives every browser-session
// step whose store or generator error reaches the answer, through the
// session routes and the OIDC and GitHub callbacks that issue a session
// through the same handler, with a backend fault, a client cancel, and an
// inner cancel (#7674).
func TestBrowserSessionStepFailuresAnswerFixedText(t *testing.T) {
	createFails := func(secretCall int) func(error) failure.Mounter {
		return func(err error) failure.Mounter {
			if secretCall > 0 {
				return sessionIssuer(&failingBrowserSessionStore{}, failingSecret(secretCall, err))
			}
			return sessionIssuer(&failingBrowserSessionStore{createErr: err}, failingSecret(0, nil))
		}
	}
	routes := []failure.Route{
		{
			Name: "create session secret", Method: http.MethodPost, Path: "/api/v0/auth/browser-session",
			Message: "failed to create browser session", Scope: bearerScope, Handler: createFails(1),
		},
		{
			Name: "create csrf secret", Method: http.MethodPost, Path: "/api/v0/auth/browser-session",
			Message: "failed to create browser session", Scope: bearerScope, Handler: createFails(2),
		},
		{
			Name: "create session store", Method: http.MethodPost, Path: "/api/v0/auth/browser-session",
			Message: "failed to create browser session", Scope: bearerScope, Handler: createFails(0),
		},
		{
			Name: "logout revoke", Method: http.MethodDelete, Path: "/api/v0/auth/browser-session",
			Message: "failed to revoke browser session", Scope: browserSessionScope,
			Handler: func(err error) failure.Mounter {
				return withSessionCookie(sessionIssuer(&failingBrowserSessionStore{revokeErr: err}, failingSecret(0, nil)))
			},
		},
		{
			Name: "workspace switch", Method: http.MethodPatch, Path: "/api/v0/auth/browser-session/context",
			Body:    `{"tenant_id":"tenant_b","workspace_id":"workspace_b"}`,
			Message: "failed to switch browser session workspace", Scope: browserSessionScope,
			Handler: func(err error) failure.Mounter {
				return withSessionCookie(sessionIssuer(&failingBrowserSessionStore{switchErr: err}, failingSecret(0, nil)))
			},
		},
		{
			Name: "oidc callback session store", Path: "/api/v0/auth/oidc/callback?state=s&code=c",
			Message: "failed to create browser session",
			Handler: func(err error) failure.Mounter {
				return &OIDCLoginHandler{
					Service:       &fakeOIDCLoginService{complete: OIDCLoginCompleteResponse{Auth: completedLogin()}},
					SessionIssuer: sessionIssuer(&failingBrowserSessionStore{createErr: err}, failingSecret(0, nil)),
				}
			},
		},
		{
			Name: "github callback session store", Path: "/api/v0/auth/github/callback?state=s&code=c",
			Message: "failed to create browser session",
			Handler: func(err error) failure.Mounter {
				return &GitHubLoginHandler{
					Service:       &fakeGitHubLoginService{complete: GitHubLoginCompleteResponse{Auth: completedLogin()}},
					SessionIssuer: sessionIssuer(&failingBrowserSessionStore{createErr: err}, failingSecret(0, nil)),
				}
			},
		},
	}
	runAuthFailureSteps(t, failure.Suite{Routes: routes}, errorFlowKinds)
}

// TestListSessionsStoreFailureAnswersFixedText drives GET
// /api/v0/auth/sessions through a failing session store with a backend
// fault, a client cancel, and an inner cancel (#7674). A client cancel is
// not logged as a failure.
func TestListSessionsStoreFailureAnswersFixedText(t *testing.T) {
	routes := []failure.Route{{
		Name: "session list", Path: "/api/v0/auth/sessions",
		Message: "failed to list browser sessions", Scope: browserSessionScope,
		Handler: func(err error) failure.Mounter {
			return &BrowserSessionListHandler{Store: &fakeBrowserSessionListStore{listErr: err}}
		},
	}}
	runAuthFailureSteps(t, failure.Suite{Routes: routes}, errorFlowKinds)
}

// TestSSOLoginServiceFailuresAnswerFixedText drives the OIDC and GitHub
// login and callback routes when the login service returns an error none of
// its sentinels classify. The body is the fixed "login failed" text; the
// service error, which can quote an IdP response or a provider config value,
// stays on the span. The sentinel-classified 503, 400, and 403 answers do not
// change.
func TestSSOLoginServiceFailuresAnswerFixedText(t *testing.T) {
	issuer := sessionIssuer(&failingBrowserSessionStore{}, failingSecret(0, nil))
	routes := []failure.Route{
		{
			Name: "oidc login start", Path: "/api/v0/auth/oidc/login?provider_config_id=okta", Message: "oidc login failed",
			Handler: func(err error) failure.Mounter {
				return &OIDCLoginHandler{Service: &fakeOIDCLoginService{startErr: err}, SessionIssuer: issuer}
			},
		},
		{
			Name: "oidc callback", Path: "/api/v0/auth/oidc/callback?state=s&code=c", Message: "oidc login failed",
			Handler: func(err error) failure.Mounter {
				return &OIDCLoginHandler{Service: &fakeOIDCLoginService{completeErr: err}, SessionIssuer: issuer}
			},
		},
		{
			Name: "github login start", Path: "/api/v0/auth/github/login?provider_config_id=gh", Message: "github login failed",
			Handler: func(err error) failure.Mounter {
				return &GitHubLoginHandler{Service: &fakeGitHubLoginService{startErr: err}, SessionIssuer: issuer}
			},
		},
		{
			Name: "github callback", Path: "/api/v0/auth/github/callback?state=s&code=c", Message: "github login failed",
			Handler: func(err error) failure.Mounter {
				return &GitHubLoginHandler{Service: &fakeGitHubLoginService{completeErr: err}, SessionIssuer: issuer}
			},
		},
	}
	runAuthFailureSteps(t, failure.Suite{Routes: routes}, errorFlowKinds)
}

// TestSessionSentinelFailuresAnswerFixedText drives the session and login
// steps that have no caller error of their own: a session secret that hashes
// to empty, and a login service that returned no redirect URL. Each answers
// its fixed message with a package sentinel on the span, so only the server
// fault case applies.
func TestSessionSentinelFailuresAnswerFixedText(t *testing.T) {
	blank := func() (string, error) { return "  ", nil }
	routes := []failure.Route{
		{
			Name: "create session empty secret", Method: http.MethodPost, Path: "/api/v0/auth/browser-session",
			Message: "failed to create browser session", Scope: bearerScope,
			Handler: func(error) failure.Mounter { return sessionIssuer(&failingBrowserSessionStore{}, blank) },
		},
		{
			Name: "oidc login empty redirect", Path: "/api/v0/auth/oidc/login", Message: "oidc login did not return a redirect URL",
			Handler: func(error) failure.Mounter { return &OIDCLoginHandler{Service: &fakeOIDCLoginService{}} },
		},
		{
			Name: "github login empty redirect", Path: "/api/v0/auth/github/login", Message: "github login did not return a redirect URL",
			Handler: func(error) failure.Mounter { return &GitHubLoginHandler{Service: &fakeGitHubLoginService{}} },
		},
	}
	runAuthFailureSteps(t, failure.Suite{Routes: routes}, faultOnlyKinds)
}
