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

func TestLocalLoginFailsOpenWhenSignInPolicyReadErrors(t *testing.T) {
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

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (policy read failure must fail open, not lock out every local login): %s", rec.Code, http.StatusOK, rec.Body.String())
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
