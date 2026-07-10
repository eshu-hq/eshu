// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// TestLocalIdentityLoginMustChangePasswordDoesNotIssueSession proves the
// wire mapping for issue #4976: a login whose credential proof succeeded but
// carries must_change_password=true returns the distinct "must_change_password"
// status (not "authenticated", not the generic "invalid" default), 202
// Accepted, and no session.
func TestLocalIdentityLoginMustChangePasswordDoesNotIssueSession(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{
		authResult: LocalIdentityAuthenticationResult{
			Status: LocalIdentityAuthMustChangePassword,
			Auth: LocalIdentityAuthContext{
				TenantID:      "tenant_local",
				WorkspaceID:   "workspace_local",
				SubjectIDHash: "sha256:owner-subject",
				AllScopes:     true,
			},
		},
	}
	sessions := &fakeBrowserSessionStore{}
	audit := &fakeGovernanceAuditAppender{}
	handler := &LocalIdentityHandler{Store: store, Sessions: sessions, Audit: audit}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/login",
		bytes.NewBufferString(`{"login_id":"admin","password":"env-seeded-password","recovery_code":"one-time-a"}`),
	)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if len(sessions.created) != 0 {
		t.Fatalf("created sessions = %d, want 0", len(sessions.created))
	}
	var response LocalIdentitySessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if response.Status != "must_change_password" {
		t.Fatalf("response status = %q, want must_change_password", response.Status)
	}
	if len(audit.events) != 1 || audit.events[0].ReasonCode != "must_change_password" {
		t.Fatalf("audit events = %#v, want one must_change_password denial", audit.events)
	}
}

// TestLocalIdentityRotatePasswordIssuesSessionAndForwardsHashedProof proves
// handleRotatePassword hashes the submitted current/new password and
// recovery code exactly like login/bootstrap (never forwarding plaintext to
// the store) and issues a session on a successful rotation.
func TestLocalIdentityRotatePasswordIssuesSessionAndForwardsHashedProof(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 13, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{
		rotationResult: LocalIdentityAuthenticationResult{
			Status:        LocalIdentityAuthAuthenticated,
			Authenticated: true,
			Auth: LocalIdentityAuthContext{
				TenantID:      "tenant_local",
				WorkspaceID:   "workspace_local",
				SubjectIDHash: "sha256:owner-subject",
				SubjectClass:  "local_user",
				AllScopes:     true,
			},
		},
	}
	sessions := &fakeBrowserSessionStore{}
	audit := &fakeGovernanceAuditAppender{}
	handler := &LocalIdentityHandler{
		Store:        store,
		Sessions:     sessions,
		Audit:        audit,
		Now:          func() time.Time { return now },
		NewSecret:    sequenceSecrets("credential-id", "session-secret", "csrf-secret"),
		PasswordCost: bcrypt.MinCost,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := bytes.NewBufferString(`{
		"login_id":"admin",
		"current_password":"env-seeded-password",
		"new_password":"new-strong-password",
		"recovery_code":"one-time-a"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/password/rotate", body)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store.rotation.SubjectIDHash == "admin" || store.rotation.MFARecoveryCodeHash == "one-time-a" {
		t.Fatalf("rotation request leaked raw identifiers/codes: %#v", store.rotation)
	}
	if store.rotation.CurrentPassword != "env-seeded-password" {
		t.Fatalf("rotation current password = %q, want the submitted plaintext proof forwarded for bcrypt compare", store.rotation.CurrentPassword)
	}
	if store.rotation.NewPasswordHash == "" || store.rotation.NewPasswordHash == "new-strong-password" ||
		bcrypt.CompareHashAndPassword([]byte(store.rotation.NewPasswordHash), []byte("new-strong-password")) != nil {
		t.Fatalf("rotation new password hash did not store bcrypt proof: %q", store.rotation.NewPasswordHash)
	}
	if store.rotation.CredentialID != "credential-id" {
		t.Fatalf("rotation credential id = %q, want generated id", store.rotation.CredentialID)
	}
	if len(sessions.created) != 1 {
		t.Fatalf("created sessions = %d, want 1", len(sessions.created))
	}
	if strings.Contains(rec.Body.String(), "env-seeded-password") || strings.Contains(rec.Body.String(), "new-strong-password") {
		t.Fatalf("rotation response leaked plaintext passwords: %s", rec.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].ReasonCode != "local_password_rotation_forced" {
		t.Fatalf("audit events = %#v, want one local_password_rotation_forced allow", audit.events)
	}
}

// TestLocalIdentityRotatePasswordDeniedForNonAdminWhenRequireSSO is the P1
// regression guard (codex PR #5054 review): the rotation route issues a
// browser session, so it must honor the same require_sso gate handleLogin
// enforces. A non-admin in a require_sso=true tenant who proves their current
// password must NOT obtain a local session by rotating — that would bypass the
// tenant's SSO-only lockdown. Expect 403 and no session.
func TestLocalIdentityRotatePasswordDeniedForNonAdminWhenRequireSSO(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 13, 30, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{
		rotationResult: LocalIdentityAuthenticationResult{
			Status:        LocalIdentityAuthAuthenticated,
			Authenticated: true,
			Auth: LocalIdentityAuthContext{
				TenantID:      "tenant_local",
				SubjectIDHash: "sha256:member-subject",
				SubjectClass:  "local_user",
				AllScopes:     false,
			},
		},
	}
	sessions := &fakeBrowserSessionStore{}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{RequireSSO: true}}
	handler := &LocalIdentityHandler{
		Store: store, Sessions: sessions, SignInPolicy: policy,
		Now: func() time.Time { return now }, PasswordCost: bcrypt.MinCost,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/password/rotate", bytes.NewBufferString(`{
		"login_id":"member@example.test",
		"current_password":"member-password",
		"new_password":"new-strong-password"
	}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (require_sso must block a non-admin rotation session): %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if len(sessions.created) != 0 {
		t.Fatalf("created sessions = %d, want 0", len(sessions.created))
	}
}

// TestLocalIdentityRotatePasswordAllowedForAdminBreakGlassWhenRequireSSO proves
// the require_sso gate on rotation preserves admin break-glass: an admin
// (AllScopes) in a require_sso=true tenant still rotates and receives a
// session, exactly as handleLogin's break-glass path does.
func TestLocalIdentityRotatePasswordAllowedForAdminBreakGlassWhenRequireSSO(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 13, 35, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{
		rotationResult: LocalIdentityAuthenticationResult{
			Status:        LocalIdentityAuthAuthenticated,
			Authenticated: true,
			Auth: LocalIdentityAuthContext{
				TenantID:      "tenant_local",
				SubjectIDHash: "sha256:owner-subject",
				SubjectClass:  "local_user",
				AllScopes:     true,
			},
		},
	}
	sessions := &fakeBrowserSessionStore{}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{RequireSSO: true}}
	handler := &LocalIdentityHandler{
		Store: store, Sessions: sessions, SignInPolicy: policy,
		Now: func() time.Time { return now }, NewSecret: sequenceSecrets("credential-id", "session-secret", "csrf-secret"), PasswordCost: bcrypt.MinCost,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/password/rotate", bytes.NewBufferString(`{
		"login_id":"admin",
		"current_password":"env-seeded-password",
		"new_password":"new-strong-password",
		"recovery_code":"one-time-a"
	}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (admin break-glass rotation under require_sso): %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(sessions.created) != 1 {
		t.Fatalf("created sessions = %d, want 1 (admin break-glass session issued)", len(sessions.created))
	}
}

// TestLocalIdentityRotatePasswordRejectsWrongCurrentPassword proves a failed
// rotation attempt does not issue a session.
func TestLocalIdentityRotatePasswordRejectsWrongCurrentPassword(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{
		rotationResult: LocalIdentityAuthenticationResult{Status: LocalIdentityAuthInvalid},
	}
	sessions := &fakeBrowserSessionStore{}
	handler := &LocalIdentityHandler{Store: store, Sessions: sessions, PasswordCost: bcrypt.MinCost}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/password/rotate",
		bytes.NewBufferString(`{"login_id":"admin","current_password":"wrong","new_password":"new-strong-password"}`),
	)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if len(sessions.created) != 0 {
		t.Fatalf("created sessions = %d, want 0", len(sessions.created))
	}
}
