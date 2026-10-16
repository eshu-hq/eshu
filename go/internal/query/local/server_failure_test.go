// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package local

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/auth/session"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// ownerScope is a browser-session caller acting on its own identity.
func ownerScope(ctx context.Context) context.Context {
	return auth.ContextWithAuthContext(ctx, auth.AuthContext{Mode: auth.AuthModeBrowserSession, SubjectIDHash: "sha256:owner"})
}

// allScopeAdminScope is an all-scope browser-session admin.
func allScopeAdminScope(ctx context.Context) context.Context {
	return auth.ContextWithAuthContext(ctx, auth.AuthContext{
		Mode: auth.AuthModeBrowserSession, AllScopes: true, SubjectIDHash: "sha256:admin",
	})
}

// sharedOperatorScope is the shared operator.
func sharedOperatorScope(ctx context.Context) context.Context {
	return auth.ContextWithAuthContext(ctx, auth.AuthContext{Mode: auth.AuthModeShared, AllScopes: true})
}

// failingSecrets is a NewSecret double that always fails with err.
func failingSecrets(err error) func() (string, error) {
	return func() (string, error) { return "", err }
}

// sessionIssueRoute mounts a probe route over IssueSessionCookies, the
// session-issuance body the login, break-glass, rotation, and setup routes
// share, so each of its failing steps can be driven directly.
type sessionIssueRoute struct {
	sessions  session.BrowserSessionStore
	newSecret func() (string, error)
}

func (s sessionIssueRoute) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /session-issue", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := IssueSessionCookies(w, r, s.sessions, s.newSecret, time.Now().UTC(),
			time.Hour, 2*time.Hour, session.CookieSecureAuto, IdentityAuthContext{}); ok {
			w.WriteHeader(http.StatusNoContent)
		}
	})
}

// runFailureSteps drives every route through each case kind in kinds. The
// local identity routes run on the writer, so the reader-fence case never
// applies.
func runFailureSteps(t *testing.T, suite failure.Suite, kinds ...failure.Kind) {
	t.Helper()
	for _, route := range suite.Routes {
		for _, tc := range failure.Cases() {
			if !kindIn(tc.Kind, kinds) {
				continue
			}
			t.Run(route.Name+"/"+tc.Name, func(t *testing.T) {
				if err := failure.Check(suite, route, tc); err != nil {
					t.Error(err)
				}
			})
		}
	}
}

func kindIn(kind failure.Kind, kinds []failure.Kind) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// TestLocalIdentityStepFailuresAnswerFixedText drives every local identity
// step whose error flows to the answer through a backend fault and a client
// cancel (#7674).
func TestLocalIdentityStepFailuresAnswerFixedText(t *testing.T) {
	resolveFails := func(err error) failure.Mounter {
		return &IdentityHandler{Store: &fakeStore{resolvedUserIDError: err}, NewSecret: sequenceSecrets("factor-1")}
	}
	secretFails := func(err error) failure.Mounter {
		return &IdentityHandler{Store: &fakeStore{}, NewSecret: failingSecrets(err)}
	}
	routes := []failure.Route{
		{
			Name: "totp begin user lookup", Method: http.MethodPost, Path: "/api/v0/auth/local/mfa/totp/begin",
			Message: totpEnrollBeginFailedMessage, Scope: ownerScope, Handler: resolveFails,
		},
		{
			Name: "totp confirm user lookup", Method: http.MethodPost, Path: "/api/v0/auth/local/mfa/totp/confirm",
			Body:    `{"factor_id":"factor-1","code":"123456"}`,
			Message: totpEnrollConfirmFailedMessage, Scope: ownerScope, Handler: resolveFails,
		},
		{
			Name: "self-service api token user lookup", Method: http.MethodPost, Path: "/api/v0/auth/local/api-tokens",
			Body:    `{"token_class":"personal"}`,
			Message: apiAccessKeyCreateFailedMessage, Scope: ownerScope, Handler: resolveFails,
		},
		{
			Name: "admin api token user lookup", Method: http.MethodPost, Path: "/api/v0/auth/local/api-tokens",
			Body:    `{"token_class":"personal"}`,
			Message: apiAccessKeyCreateFailedMessage, Scope: allScopeAdminScope, Handler: resolveFails,
		},
		{
			Name: "list api tokens", Path: "/api/v0/auth/local/api-tokens",
			Message: apiAccessKeyListFailedMessage, Scope: allScopeAdminScope,
			Handler: func(err error) failure.Mounter {
				return &IdentityHandler{Store: &fakeStore{apiTokenListError: err}}
			},
		},
		{
			Name: "api token generation", Method: http.MethodPost, Path: "/api/v0/auth/local/api-tokens",
			Body:    `{"user_id":"user-1","token_class":"personal"}`,
			Message: apiAccessKeyCreateFailedMessage, Scope: allScopeAdminScope, Handler: secretFails,
		},
		{
			Name: "api token rotation generation", Method: http.MethodPost, Path: "/api/v0/auth/local/api-tokens/tok-1/rotate",
			Body:    `{}`,
			Message: apiAccessKeyRotateFailedMessage, Scope: allScopeAdminScope, Handler: secretFails,
		},
		{
			Name: "session secret generation", Method: http.MethodPost, Path: "/session-issue",
			Message: sessionCreateFailedMessage,
			Handler: func(err error) failure.Mounter {
				return sessionIssueRoute{sessions: &fakeSessions{}, newSecret: failingSecrets(err)}
			},
		},
		{
			Name: "session csrf generation", Method: http.MethodPost, Path: "/session-issue",
			Message: sessionCreateFailedMessage,
			Handler: func(err error) failure.Mounter {
				calls := 0
				return sessionIssueRoute{sessions: &fakeSessions{}, newSecret: func() (string, error) {
					calls++
					if calls == 1 {
						return "session-value", nil
					}
					return "", err
				}}
			},
		},
		{
			Name: "session create", Method: http.MethodPost, Path: "/session-issue",
			Message: sessionCreateFailedMessage,
			Handler: func(err error) failure.Mounter {
				return sessionIssueRoute{sessions: &fakeSessions{createErr: err}, newSecret: sequenceSecrets("s-1", "c-1")}
			},
		},
	}
	runFailureSteps(t, failure.Suite{Routes: routes}, failure.Fault, failure.ClientCancel, failure.InnerCancel)
}

// TestLocalIdentityEmptyGeneratedValueAnswersFixedText drives the steps where
// a generator came back empty with no error of its own. They answer the
// fixed message with errGeneratedValueEmpty on the span, so only the server
// fault case applies: no caller error flows to them to classify as a cancel.
func TestLocalIdentityEmptyGeneratedValueAnswersFixedText(t *testing.T) {
	emptyIDs := func(err error) failure.Mounter {
		return &IdentityHandler{Store: &fakeStore{resolvedUserID: "user-owner", resolvedUserIDFound: true}, NewSecret: failingSecrets(err)}
	}
	routes := []failure.Route{
		{
			Name: "bootstrap ids", Method: http.MethodPost, Path: "/api/v0/auth/local/bootstrap",
			Body:    `{"login_id":"owner","password":"correct horse battery staple"}`,
			Message: bootstrapCreateFailedMessage, Scope: sharedOperatorScope, Handler: emptyIDs,
		},
		{
			Name: "invitation ids", Method: http.MethodPost, Path: "/api/v0/auth/local/invitations",
			Body:    `{"invitee_handle":"dev@example.test"}`,
			Message: invitationCreateFailedMessage, Scope: allScopeAdminScope, Handler: emptyIDs,
		},
		{
			Name: "totp factor id", Method: http.MethodPost, Path: "/api/v0/auth/local/mfa/totp/begin",
			Message: totpEnrollBeginFailedMessage, Scope: ownerScope, Handler: emptyIDs,
		},
	}
	runFailureSteps(t, failure.Suite{Routes: routes}, failure.Fault)
}

// TestGeneratedValueFailureKeepsTheGeneratorError pins that a generator's own
// error is what the span records, and the sentinel only stands in for none.
func TestGeneratedValueFailureKeepsTheGeneratorError(t *testing.T) {
	t.Parallel()
	cause := errors.New("entropy unavailable")
	if got := generatedValueFailure(cause); !errors.Is(got, cause) {
		t.Fatalf("generatedValueFailure(cause) = %v, want cause", got)
	}
	if got := generatedValueFailure(nil); !errors.Is(got, errGeneratedValueEmpty) {
		t.Fatalf("generatedValueFailure(nil) = %v, want errGeneratedValueEmpty", got)
	}
}
