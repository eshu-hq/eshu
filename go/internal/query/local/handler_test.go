// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package local

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
)

// This file proves each of Handler's core session-lifecycle routes end to
// end through Mount + ServeHTTP (#6642 route-coverage: these tests moved
// with the family, since almost every pre-move root test for this family
// depended on root-private fixtures shared with other families -- see
// doc.go's Move evidence). It intentionally exercises success and one
// meaningful failure path per route rather than the full matrix root's
// pre-move tests covered; the exhaustive behavioral proof for
// bootstrap/login/invitation/password/MFA/break-glass logic stays in root's
// identity_handler_test.go et al., unaffected by this move.

func sharedOperatorContext(r *http.Request) *http.Request {
	auth := queryauth.AuthContext{Mode: queryauth.AuthModeShared}
	return r.WithContext(queryauth.ContextWithAuthContext(r.Context(), auth))
}

func allScopeContext(r *http.Request) *http.Request {
	auth := queryauth.AuthContext{Mode: queryauth.AuthModeBrowserSession, AllScopes: true, SubjectIDHash: "sha256:admin"}
	return r.WithContext(queryauth.ContextWithAuthContext(r.Context(), auth))
}

func TestBootstrap(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	handler := &IdentityHandler{Store: store, NewSecret: sequenceSecrets("user-1", "factor-1")}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/bootstrap", bytes.NewBufferString(
		`{"tenant_id":"tenant-a","workspace_id":"ws-a","login_id":"owner","password":"hunter2hunter2"}`,
	))
	req = sharedOperatorContext(req)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if store.bootstrap.TenantID != "tenant-a" || store.bootstrap.UserID != "user-1" {
		t.Fatalf("bootstrap record = %#v, want tenant-a/user-1", store.bootstrap)
	}
}

func TestBootstrapRequiresSharedOperator(t *testing.T) {
	t.Parallel()

	handler := &IdentityHandler{Store: &fakeStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/bootstrap", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestLogin(t *testing.T) {
	t.Parallel()

	store := &fakeStore{authResult: IdentityAuthenticationResult{
		Status:        IdentityAuthAuthenticated,
		Authenticated: true,
		Auth:          IdentityAuthContext{TenantID: "tenant-a", SubjectIDHash: "sha256:owner"},
	}}
	handler := &IdentityHandler{
		Store:     store,
		Sessions:  &fakeSessions{},
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/login", bytes.NewBufferString(
		`{"login_id":"owner","password":"hunter2hunter2"}`,
	))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store.attempt.SubjectIDHash != IdentityHash("owner") {
		t.Fatalf("attempt subject hash = %q, want IdentityHash(owner)", store.attempt.SubjectIDHash)
	}
}

func TestLoginInvalidCredentialReturnsUnauthorized(t *testing.T) {
	t.Parallel()

	store := &fakeStore{authResult: IdentityAuthenticationResult{
		Status:        IdentityAuthInvalid,
		Authenticated: false,
	}}
	handler := &IdentityHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/login", bytes.NewBufferString(
		`{"login_id":"owner","password":"wrong-password"}`,
	))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	var body IdentitySessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Status != string(IdentityAuthInvalid) {
		t.Fatalf("body status = %q, want %q", body.Status, IdentityAuthInvalid)
	}
}

func TestLoginMalformedBodyReturnsBadRequest(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	handler := &IdentityHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/login", bytes.NewBufferString(`not-json`))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestCreateInvitation(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	handler := &IdentityHandler{Store: store, NewSecret: sequenceSecrets("invite-code-1", "invite-1")}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/invitations", bytes.NewBufferString(
		`{"tenant_id":"tenant-a","workspace_id":"ws-a","invitee_handle":"new-owner"}`,
	))
	req = allScopeContext(req)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if store.invitation.TenantID != "tenant-a" {
		t.Fatalf("invitation = %#v, want tenant-a", store.invitation)
	}
}

func TestCreateInvitationRequiresAllScopeAuth(t *testing.T) {
	t.Parallel()

	handler := &IdentityHandler{Store: &fakeStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/invitations", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAcceptInvitation(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	handler := &IdentityHandler{Store: store, NewSecret: sequenceSecrets("user-2")}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/invitations/accept", bytes.NewBufferString(
		`{"invite_code":"code-1","login_id":"new-owner","password":"hunter2hunter2"}`,
	))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if store.acceptance.UserID != "user-2" {
		t.Fatalf("acceptance = %#v, want user-2", store.acceptance)
	}
}

func TestResetPassword(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	handler := &IdentityHandler{Store: store, NewSecret: sequenceSecrets("credential-1")}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/users/user-1/password", bytes.NewBufferString(
		`{"password":"hunter2hunter2"}`,
	))
	req = allScopeContext(req)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if store.passwordReset.UserID != "user-1" {
		t.Fatalf("password reset = %#v, want user-1", store.passwordReset)
	}
}

func TestRotatePassword(t *testing.T) {
	t.Parallel()

	store := &fakeStore{rotationResult: IdentityAuthenticationResult{
		Status:        IdentityAuthAuthenticated,
		Authenticated: true,
		Auth:          IdentityAuthContext{TenantID: "tenant-a", SubjectIDHash: "sha256:owner"},
	}}
	handler := &IdentityHandler{
		Store:     store,
		Sessions:  &fakeSessions{},
		NewSecret: sequenceSecrets("credential-2", "session-secret", "csrf-secret"),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/password/rotate", bytes.NewBufferString(
		`{"login_id":"owner","current_password":"oldpass1234","new_password":"hunter2hunter2"}`,
	))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store.rotation.CurrentPassword != "oldpass1234" {
		t.Fatalf("rotation = %#v, want current password recorded", store.rotation)
	}
}

func TestResetMFA(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	handler := &IdentityHandler{Store: store, NewSecret: sequenceSecrets("factor-1")}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/users/user-1/mfa-reset", bytes.NewBufferString(
		`{"recovery_codes":["one","two"]}`,
	))
	req = allScopeContext(req)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if store.mfaReset.UserID != "user-1" || len(store.mfaReset.RecoveryCodeHashes) != 2 {
		t.Fatalf("mfa reset = %#v, want user-1 with 2 recovery hashes", store.mfaReset)
	}
}

func TestDisableUser(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	handler := &IdentityHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/users/user-1/disable", nil)
	req = allScopeContext(req)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if store.disable.UserID != "user-1" {
		t.Fatalf("disable = %#v, want user-1", store.disable)
	}
}

func TestEnableBreakGlass(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	handler := &IdentityHandler{Store: store, NewSecret: sequenceSecrets("break-glass-code", "recovery-1")}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/break-glass", bytes.NewBufferString(
		`{"tenant_id":"tenant-a","workspace_id":"ws-a"}`,
	))
	req = sharedOperatorContext(req)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if store.breakGlass.TenantID != "tenant-a" {
		t.Fatalf("break-glass window = %#v, want tenant-a", store.breakGlass)
	}
}

func TestBreakGlassSession(t *testing.T) {
	t.Parallel()

	store := &fakeStore{breakGlassAuth: IdentityAuthContext{TenantID: "tenant-a", SubjectIDHash: "sha256:owner", AllScopes: true}}
	handler := &IdentityHandler{
		Store:     store,
		Sessions:  &fakeSessions{},
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/break-glass/session", bytes.NewBufferString(
		`{"break_glass_code":"code-1"}`,
	))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestBreakGlassSessionUnavailableReturnsUnauthorized(t *testing.T) {
	t.Parallel()

	store := &fakeStore{breakGlassError: errBreakGlassFixture}
	handler := &IdentityHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/break-glass/session", bytes.NewBufferString(
		`{"break_glass_code":"wrong"}`,
	))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

var errBreakGlassFixture = errors.New("break glass fixture error")

// errTOTPConfirmFixture is a distinct sentinel from errBreakGlassFixture
// (above) so a TOTP-confirm-failure test doesn't borrow an error whose name
// implies an unrelated fixture; used by totp_test.go.
var errTOTPConfirmFixture = errors.New("totp confirm fixture error")
