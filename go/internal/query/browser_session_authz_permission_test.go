// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func catalogEnforcedAdminAuth(features ...string) AuthContext {
	return AuthContext{
		Mode:                         AuthModeBrowserSession,
		TenantID:                     "tenant_a",
		WorkspaceID:                  "workspace_a",
		SubjectIDHash:                "subject-redacted",
		AllScopes:                    true,
		PermissionCatalogEnforced:    true,
		AllowedPermissionFeatures:    features,
		AllowedPermissionDataClasses: []string{"admin_metadata", "catalog_metadata", "token_metadata", "audit_sensitive"},
	}
}

// TestBrowserSessionAdminMutationsRequireCatalogFeature covers the unsafe
// browser-session admin routes. CSRF is enforced by middleware before these
// handlers; this test guards the handler-level permission family after auth.

func TestBrowserSessionHandleCreateAPITokenHandleRevokeAPITokenHandleRotateAPITokenMutationsRequireTokenCatalogFeature(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{}
	audit := &fakeGovernanceAuditAppender{}
	secretCalls := 0
	handler := &LocalIdentityHandler{
		Store: store,
		Audit: audit,
		NewSecret: func() (string, error) {
			secretCalls++
			t.Fatal("NewSecret called before token permission check")
			return "", nil
		},
		Now: func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create", http.MethodPost, "/api/v0/auth/local/api-tokens", `{"token_class":"personal","user_id":"user_owner"}`},
		{"revoke", http.MethodPost, "/api/v0/auth/local/api-tokens/token-old/revoke", `{}`},
		{"rotate", http.MethodPost, "/api/v0/auth/local/api-tokens/token-old/rotate", `{}`},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req = req.WithContext(ContextWithAuthContext(req.Context(), catalogEnforcedAdminAuth("identity_admin")))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s status = %d, want 403: %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
	if store.createdAPIToken.TokenID != "" || store.revokedAPIToken.TokenID != "" || store.rotatedAPIToken.OldTokenID != "" {
		t.Fatalf("token store was called for denied token route: %#v %#v %#v",
			store.createdAPIToken, store.revokedAPIToken, store.rotatedAPIToken)
	}
	if secretCalls != 0 {
		t.Fatalf("NewSecret calls = %d, want 0 before token permission check", secretCalls)
	}
	assertPermissionCatalogDeniedAuditEvents(t, audit, 3)
}

func TestBrowserSessionHandleCreateInvitationHandleResetPasswordHandleResetMFAHandleDisableUserRequireCatalogFeature(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{}
	audit := &fakeGovernanceAuditAppender{}
	secretCalls := 0
	handler := &LocalIdentityHandler{
		Store: store,
		Audit: audit,
		NewSecret: func() (string, error) {
			secretCalls++
			t.Fatal("NewSecret called before local identity admin permission check")
			return "", nil
		},
		Now: func() time.Time { return time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC) },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, tc := range []struct {
		name   string
		path   string
		body   string
		method string
	}{
		{"create invitation", "/api/v0/auth/local/invitations", `{"invitee_handle":"new-user","role_id":"developer"}`, http.MethodPost},
		{"reset password", "/api/v0/auth/local/users/user_1/password", `{"password":"redacted"}`, http.MethodPost},
		{"reset mfa", "/api/v0/auth/local/users/user_1/mfa-reset", `{}`, http.MethodPost},
		{"disable user", "/api/v0/auth/local/users/user_1/disable", `{}`, http.MethodPost},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req = req.WithContext(ContextWithAuthContext(req.Context(), catalogEnforcedAdminAuth("tokens")))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s status = %d, want 403: %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
	if store.invitation.InviteID != "" || store.passwordReset.UserID != "" ||
		store.mfaReset.UserID != "" || store.disable.UserID != "" {
		t.Fatalf("local identity admin store was called for denied route: %#v %#v %#v %#v",
			store.invitation, store.passwordReset, store.mfaReset, store.disable)
	}
	if secretCalls != 0 {
		t.Fatalf("NewSecret calls = %d, want 0 before local identity admin permission check", secretCalls)
	}
	assertPermissionCatalogDeniedAuditEvents(t, audit, 4)
}

func assertPermissionCatalogDeniedAuditEvents(t *testing.T, audit *fakeGovernanceAuditAppender, want int) {
	t.Helper()

	if got := len(audit.events); got != want {
		t.Fatalf("audit events = %d, want %d permission catalog denials", got, want)
	}
	for i, event := range audit.events {
		if event.ReasonCode != "permission_catalog_denied" {
			t.Fatalf("audit event %d reason = %q, want permission_catalog_denied", i, event.ReasonCode)
		}
	}
}
