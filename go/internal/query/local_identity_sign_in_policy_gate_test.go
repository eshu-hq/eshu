// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeSignInPolicyReadStore struct {
	policy    SignInPolicy
	err       error
	tenantIDs []string
}

func (s *fakeSignInPolicyReadStore) GetSignInPolicy(_ context.Context, tenantID string) (SignInPolicy, error) {
	s.tenantIDs = append(s.tenantIDs, tenantID)
	if s.err != nil {
		return SignInPolicy{}, s.err
	}
	return s.policy, nil
}

func TestLocalLoginDeniedForNonAdminWhenRequireSSOPolicyIsOn(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{
		authResult: LocalIdentityAuthenticationResult{
			Status:        LocalIdentityAuthAuthenticated,
			Authenticated: true,
			Auth: LocalIdentityAuthContext{
				TenantID:      "tenant_local",
				SubjectIDHash: "sha256:dev-subject",
				AllScopes:     false,
			},
		},
	}
	sessions := &fakeBrowserSessionStore{}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{RequireSSO: true}}
	handler := &LocalIdentityHandler{Store: store, Sessions: sessions, SignInPolicy: policy}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/login",
		bytes.NewBufferString(`{"login_id":"dev@example.test","password":"plaintext-password"}`),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if len(sessions.created) != 0 {
		t.Fatalf("created sessions = %d, want 0 (non-admin local login must be denied)", len(sessions.created))
	}
}

func TestLocalLoginAllowedForAdminWhenRequireSSOPolicyIsOnNoBypassFlagNeeded(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{
		authResult: LocalIdentityAuthenticationResult{
			Status:        LocalIdentityAuthAuthenticated,
			Authenticated: true,
			Auth: LocalIdentityAuthContext{
				TenantID:      "tenant_local",
				SubjectIDHash: "sha256:admin-subject",
				AllScopes:     true,
			},
		},
	}
	sessions := &fakeBrowserSessionStore{}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{RequireSSO: true}}
	handler := &LocalIdentityHandler{Store: store, Sessions: sessions, SignInPolicy: policy}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// Deliberately no request field claims a "break-glass" or "local=1"
	// intent: the server applies the identical admin-only rule regardless,
	// proving the guardrail has no client-suppliable bypass parameter.
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/login",
		bytes.NewBufferString(`{"login_id":"admin@example.test","password":"plaintext-password"}`),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(sessions.created) != 1 {
		t.Fatalf("created sessions = %d, want 1 (admin break-glass local login must succeed)", len(sessions.created))
	}
}

func TestLocalLoginUnaffectedWhenRequireSSOPolicyIsOff(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{
		authResult: LocalIdentityAuthenticationResult{
			Status:        LocalIdentityAuthAuthenticated,
			Authenticated: true,
			Auth: LocalIdentityAuthContext{
				TenantID:      "tenant_local",
				SubjectIDHash: "sha256:dev-subject",
				AllScopes:     false,
			},
		},
	}
	sessions := &fakeBrowserSessionStore{}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{RequireSSO: false}}
	handler := &LocalIdentityHandler{Store: store, Sessions: sessions, SignInPolicy: policy}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/login",
		bytes.NewBufferString(`{"login_id":"dev@example.test","password":"plaintext-password"}`),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(sessions.created) != 1 {
		t.Fatalf("created sessions = %d, want 1", len(sessions.created))
	}
}

func TestLocalLoginDeniedForNonAdminWhenSignInPolicyReadErrors(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{
		authResult: LocalIdentityAuthenticationResult{
			Status:        LocalIdentityAuthAuthenticated,
			Authenticated: true,
			Auth: LocalIdentityAuthContext{
				TenantID:      "tenant_local",
				SubjectIDHash: "sha256:dev-subject",
				AllScopes:     false,
			},
		},
	}
	sessions := &fakeBrowserSessionStore{}
	policy := &fakeSignInPolicyReadStore{err: context.DeadlineExceeded}
	handler := &LocalIdentityHandler{Store: store, Sessions: sessions, SignInPolicy: policy}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/login",
		bytes.NewBufferString(`{"login_id":"dev@example.test","password":"plaintext-password"}`),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (a policy read outage must not silently grant a non-admin a session on a require_sso tenant): %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if len(sessions.created) != 0 {
		t.Fatalf("created sessions = %d, want 0 (non-admin must not receive a session when the policy cannot be read)", len(sessions.created))
	}
}

func TestLocalLoginAllowedForAdminBreakGlassWhenSignInPolicyReadErrors(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{
		authResult: LocalIdentityAuthenticationResult{
			Status:        LocalIdentityAuthAuthenticated,
			Authenticated: true,
			Auth: LocalIdentityAuthContext{
				TenantID:      "tenant_local",
				SubjectIDHash: "sha256:admin-subject",
				AllScopes:     true,
			},
		},
	}
	sessions := &fakeBrowserSessionStore{}
	policy := &fakeSignInPolicyReadStore{err: context.DeadlineExceeded}
	handler := &LocalIdentityHandler{Store: store, Sessions: sessions, SignInPolicy: policy}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/login",
		bytes.NewBufferString(`{"login_id":"admin@example.test","password":"plaintext-password"}`),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (admin break-glass must survive a policy read outage): %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(sessions.created) != 1 {
		t.Fatalf("created sessions = %d, want 1 (admin break-glass must survive a policy read outage)", len(sessions.created))
	}
}

// TestLocalLoginDeniedForNonAdminMFARequiredWhenRequireSSOPolicyIsOn is the
// P2 review-finding regression guard (PR #5049 Codex review): a non-admin
// missing MFA under require_mfa_for_all_users can never complete that
// challenge through local login when require_sso is ALSO on for the tenant —
// require_sso already forbids issuing this identity a local session at all.
// Before this fix, handleLogin short-circuited on `!result.Authenticated`
// and returned 202 mfa_required without ever consulting require_sso, so this
// non-admin was invited to attempt an MFA proof that could never succeed.
// This asserts the corrected 403 (not 202) and that requireSSODecision's
// authoritative policy read is the one consulted — no second, duplicate
// read.
func TestLocalLoginDeniedForNonAdminMFARequiredWhenRequireSSOPolicyIsOn(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{
		authResult: LocalIdentityAuthenticationResult{
			Status: LocalIdentityAuthMFARequired,
			Auth: LocalIdentityAuthContext{
				TenantID:      "tenant_local",
				SubjectIDHash: "sha256:dev-subject",
				AllScopes:     false,
			},
		},
	}
	sessions := &fakeBrowserSessionStore{}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{RequireSSO: true}}
	handler := &LocalIdentityHandler{Store: store, Sessions: sessions, SignInPolicy: policy}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/login",
		bytes.NewBufferString(`{"login_id":"dev@example.test","password":"plaintext-password"}`),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (a non-admin mfa_required can never be satisfied when require_sso is also on): %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if len(sessions.created) != 0 {
		t.Fatalf("created sessions = %d, want 0", len(sessions.created))
	}
	if len(policy.tenantIDs) != 1 || policy.tenantIDs[0] != "tenant_local" {
		t.Fatalf("policy reads = %#v, want exactly one read for tenant_local (requireSSODecision stays the single authoritative read)", policy.tenantIDs)
	}
}

// TestLocalLoginMFARequiredAllowedForNonAdminWhenRequireSSOPolicyIsOff proves
// the P2 fix does not regress the ordinary mfa_required path: when
// require_sso is off, a non-admin mfa_required response still surfaces as
// 202 (not swallowed into a 403), so the client can still prompt for the
// recovery-code proof.
func TestLocalLoginMFARequiredAllowedForNonAdminWhenRequireSSOPolicyIsOff(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{
		authResult: LocalIdentityAuthenticationResult{
			Status: LocalIdentityAuthMFARequired,
			Auth: LocalIdentityAuthContext{
				TenantID:      "tenant_local",
				SubjectIDHash: "sha256:dev-subject",
				AllScopes:     false,
			},
		},
	}
	sessions := &fakeBrowserSessionStore{}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{RequireSSO: false}}
	handler := &LocalIdentityHandler{Store: store, Sessions: sessions, SignInPolicy: policy}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/login",
		bytes.NewBufferString(`{"login_id":"dev@example.test","password":"plaintext-password"}`),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (mfa_required must still surface when require_sso is off): %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if len(sessions.created) != 0 {
		t.Fatalf("created sessions = %d, want 0", len(sessions.created))
	}
}

func TestCreateInvitationDeniedWhenAllowLocalUserCreationIsOff(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{AllowLocalUserCreation: false}}
	handler := &LocalIdentityHandler{Store: store, SignInPolicy: policy}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/invitations",
		bytes.NewBufferString(`{"tenant_id":"tenant_local","workspace_id":"workspace_local","role_id":"developer"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode: AuthModeBrowserSession, AllScopes: true, TenantID: "tenant_local", WorkspaceID: "workspace_local",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if store.invitation.InviteID != "" {
		t.Fatalf("invitation was created despite allow_local_user_creation=false: %#v", store.invitation)
	}
}

func TestCreateInvitationAllowedWhenAllowLocalUserCreationIsOn(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{AllowLocalUserCreation: true}}
	handler := &LocalIdentityHandler{Store: store, SignInPolicy: policy}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/invitations",
		bytes.NewBufferString(`{"tenant_id":"tenant_local","workspace_id":"workspace_local","role_id":"developer"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode: AuthModeBrowserSession, AllScopes: true, TenantID: "tenant_local", WorkspaceID: "workspace_local",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if store.invitation.InviteID == "" {
		t.Fatal("invitation was not created despite allow_local_user_creation=true")
	}
}
