// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

const (
	setupClaimBody = `{"username":"admin","password":"bootstrap-secret"}`
	setupAdminBody = `{"username":"admin","password":"bootstrap-secret","new_password":"a-new-strong-password"}`
	setupMFABody   = `{"username":"admin","password":"bootstrap-secret"}`
)

// openSetupStore is a fakeSetupStore with the wizard open, the bootstrap
// credential valid, and an owner resolved, so every step succeeds unless a
// test sets one of its error fields.
func openSetupStore() *fakeSetupStore {
	return &fakeSetupStore{
		needsSetup: true, validUsername: "admin", validPassword: "bootstrap-secret",
		owner: SetupOwner{
			UserID: "user-1", SubjectIDHash: "sha256:owner", TenantID: "tenant_a", WorkspaceID: "workspace_a",
		},
	}
}

// setupFailureHandler builds the wizard handler over store, with a minimal
// bcrypt cost so the admin step stays fast.
func setupFailureHandler(store *fakeSetupStore, newSecret func() (string, error)) *SetupHandler {
	return &SetupHandler{
		Store: store, Sessions: &fakeBrowserSessionStore{}, NewSecret: newSecret, PasswordCost: 4,
	}
}

// TestSetupStepFailuresAnswerFixedText drives every first-run setup step
// whose store or generator error reaches the answer through a backend fault,
// a client cancel, and an inner cancel (#7674). A failed bootstrap-credential
// check answers 500 only for a store error; a wrong credential still answers
// the same 401 for every cause, so the 500 is no oracle for the credential.
func TestSetupStepFailuresAnswerFixedText(t *testing.T) {
	storeFails := func(set func(*fakeSetupStore, error)) func(error) failure.Mounter {
		return func(err error) failure.Mounter {
			store := openSetupStore()
			set(store, err)
			return setupFailureHandler(store, failingSecret(0, nil))
		}
	}
	setupNeededFails := storeFails(func(s *fakeSetupStore, err error) { s.needsSetupErr = err })
	verifyFails := storeFails(func(s *fakeSetupStore, err error) { s.verifyErr = err })
	ownerFails := storeFails(func(s *fakeSetupStore, err error) { s.resolveOwnerErr = err })
	routes := []failure.Route{
		{Name: "setup state", Path: "/api/v0/auth/setup-state", Message: "failed to determine setup state", Handler: setupNeededFails},
		{
			Name: "claim setup-open check", Method: http.MethodPost, Path: "/api/v0/auth/setup/claim", Body: setupClaimBody,
			Message: "failed to determine setup state", Handler: setupNeededFails,
		},
		{
			Name: "claim credential check", Method: http.MethodPost, Path: "/api/v0/auth/setup/claim", Body: setupClaimBody,
			Message: "failed to verify bootstrap credential", Handler: verifyFails,
		},
		{
			Name: "admin credential check", Method: http.MethodPost, Path: "/api/v0/auth/setup/admin", Body: setupAdminBody,
			Message: "failed to verify bootstrap credential", Handler: verifyFails,
		},
		{
			Name: "admin owner resolve", Method: http.MethodPost, Path: "/api/v0/auth/setup/admin", Body: setupAdminBody,
			Message: "failed to resolve setup owner", Handler: ownerFails,
		},
		{
			Name: "admin credential id generation", Method: http.MethodPost, Path: "/api/v0/auth/setup/admin", Body: setupAdminBody,
			Message: "failed to set administrator password",
			Handler: func(err error) failure.Mounter { return setupFailureHandler(openSetupStore(), failingSecret(1, err)) },
		},
		{
			Name: "admin password rotation", Method: http.MethodPost, Path: "/api/v0/auth/setup/admin", Body: setupAdminBody,
			Message: "failed to set administrator password",
			Handler: storeFails(func(s *fakeSetupStore, err error) { s.rotatePasswordErr = err }),
		},
		{
			Name: "mfa setup-open check", Method: http.MethodPost, Path: "/api/v0/auth/setup/mfa", Body: setupMFABody,
			Message: "failed to determine setup state", Handler: setupNeededFails,
		},
		{
			Name: "mfa credential check", Method: http.MethodPost, Path: "/api/v0/auth/setup/mfa", Body: setupMFABody,
			Message: "failed to verify bootstrap credential", Handler: verifyFails,
		},
		{
			Name: "mfa owner resolve", Method: http.MethodPost, Path: "/api/v0/auth/setup/mfa", Body: setupMFABody,
			Message: "failed to resolve setup owner", Handler: ownerFails,
		},
		{
			// The session secret and csrf secret are calls 1 and 2; call 3 is
			// the MFA factor id, generated after the session is issued.
			Name: "mfa factor id generation", Method: http.MethodPost, Path: "/api/v0/auth/setup/mfa", Body: setupMFABody,
			Message: "failed to complete setup",
			Handler: func(err error) failure.Mounter { return setupFailureHandler(openSetupStore(), failingSecret(3, err)) },
		},
		{
			Name: "mfa completion", Method: http.MethodPost, Path: "/api/v0/auth/setup/mfa", Body: setupMFABody,
			Message: "failed to complete setup",
			Handler: storeFails(func(s *fakeSetupStore, err error) { s.completeMFAErr = err }),
		},
	}
	runAuthFailureSteps(t, failure.Suite{Routes: routes}, errorFlowKinds)
}

// TestSetupRecoveryCodeFailureAnswersFixedText drives the recovery-code
// generation step. Its entropy read never sees the request context, so only
// the server fault case applies: the read error goes on the span and the
// body is the fixed message.
func TestSetupRecoveryCodeFailureAnswersFixedText(t *testing.T) {
	routes := []failure.Route{{
		Name: "mfa recovery code generation", Method: http.MethodPost, Path: "/api/v0/auth/setup/mfa", Body: setupMFABody,
		Message: "failed to generate recovery codes",
		Handler: func(err error) failure.Mounter {
			handler := setupFailureHandler(openSetupStore(), failingSecret(0, nil))
			handler.readEntropy = func([]byte) (int, error) { return 0, err }
			return handler
		},
	}}
	runAuthFailureSteps(t, failure.Suite{Routes: routes}, faultOnlyKinds)
}

// TestSetupMFAFailureAfterSessionKeepsTheIssuedSession pins what a failed
// final step has already committed: the browser session is issued before the
// MFA factor id and the atomic completion, so a 500 from the completion
// still carries the session cookie and leaves the session row in place
// (#7674 docs: auth-session-failures.md). The test drives the 500 path only;
// a 499 takes the same branch with a different status.
func TestSetupMFAFailureAfterSessionKeepsTheIssuedSession(t *testing.T) {
	t.Parallel()

	store := openSetupStore()
	store.completeMFAErr = errors.New("backend: " + failure.Canary)
	sessions := &fakeBrowserSessionStore{}
	handler := &SetupHandler{Store: store, Sessions: sessions, NewSecret: failingSecret(0, nil)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/setup/mfa", strings.NewReader(setupMFABody))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if len(sessions.created) != 1 {
		t.Fatalf("created sessions = %d, want 1: the session commits before the completion step", len(sessions.created))
	}
	if cookie := requireCookie(t, rec.Result(), BrowserSessionCookieName); cookie.Value == "" {
		t.Fatalf("session cookie is empty on the failed answer")
	}
	if store.completeMFACalls != 1 {
		t.Fatalf("CompleteSetupMFA calls = %d, want 1", store.completeMFACalls)
	}
}
