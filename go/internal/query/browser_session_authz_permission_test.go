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

// TestBrowserSessionAdminReadsRequireCatalogFeature proves that an all-scope
// browser session with an enforced catalog snapshot must still carry the route's
// permission family. The denial must happen before the tenant-scoped store read.
func TestBrowserSessionAdminReadsRequireCatalogFeature(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		path    string
		feature string
	}{
		{"invitations", "/api/v0/auth/local/invitations", "identity_admin"},
		{"role assignments", "/api/v0/auth/admin/role-assignments", "roles_grants"},
		{"roles", "/api/v0/auth/admin/roles", "roles_grants"},
		{"idp providers", "/api/v0/auth/admin/idp-providers", "identity_admin"},
		{"idp group mappings", "/api/v0/auth/admin/idp-group-mappings", "roles_grants"},
		{"admin api tokens", "/api/v0/auth/admin/api-tokens", "tokens"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeAdminIdentityReadStore{}
			handler := &AdminIdentityReadHandler{Store: store}
			mux := http.NewServeMux()
			handler.Mount(mux)

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, tc.path, catalogEnforcedAdminAuth("ask_search")))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("wrong feature status = %d, want 403: %s", rec.Code, rec.Body.String())
			}
			if store.gotTenantID != "" || store.gotWorkspaceID != "" {
				t.Fatalf("store was called for denied %s with scope %q/%q", tc.path, store.gotTenantID, store.gotWorkspaceID)
			}

			allowed := httptest.NewRecorder()
			mux.ServeHTTP(allowed, adminRequest(t, http.MethodGet, tc.path, catalogEnforcedAdminAuth(tc.feature)))
			if allowed.Code != http.StatusOK {
				t.Fatalf("allowed feature status = %d, want 200: %s", allowed.Code, allowed.Body.String())
			}
		})
	}
}

// TestBrowserSessionAdminMutationsRequireCatalogFeature covers the unsafe
// browser-session admin routes. CSRF is enforced by middleware before these
// handlers; this test guards the handler-level permission family after auth.
func TestBrowserSessionAdminMutationsRequireCatalogFeature(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		method  string
		target  string
		body    string
		feature string
	}{
		{"revoke invitation", http.MethodPost, "/api/v0/auth/local/invitations/inv_1/revoke", "", "identity_admin"},
		{"grant role", http.MethodPost, "/api/v0/auth/admin/role-assignments", `{"user_id":"u1","role_id":"developer"}`, "roles_grants"},
		{"revoke role", http.MethodPost, "/api/v0/auth/admin/role-assignments/revoke", `{"user_id":"u1","role_id":"developer"}`, "roles_grants"},
		{"create idp group mapping", http.MethodPost, "/api/v0/auth/admin/idp-group-mappings", `{"provider_config_id":"prov_1","external_group":"group-redacted","role_id":"developer"}`, "roles_grants"},
		{"delete idp group mapping", http.MethodDelete, "/api/v0/auth/admin/idp-group-mappings/ref_1", "", "roles_grants"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeAdminMutationStore{
				inviteResult:        AdminInvitationRevokeResult{Found: true, Revoked: true, Status: "revoked"},
				grantResult:         AdminRoleAssignmentMutationResult{RoleValid: true, UserValid: true, Status: "active", Changed: true},
				roleRevokeResult:    AdminRoleAssignmentMutationResult{Status: "revoked", Changed: true},
				mappingCreateResult: AdminIdPGroupMappingCreateResult{ProviderValid: true, RoleValid: true, MappingRef: "ref_1", Status: "active", Created: true},
				mappingDeleteResult: AdminIdPGroupMappingDeleteResult{Found: true, Deleted: true},
			}
			mux := newMutationMux(store, &recordingAuditAppender{})

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, mutationRequest(tc.method, tc.target, tc.body, catalogEnforcedAdminAuth("ask_search")))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("wrong feature status = %d, want 403: %s", rec.Code, rec.Body.String())
			}
			if store.gotInviteRevoke.InviteID != "" || store.gotGrant.UserID != "" ||
				store.gotRoleRevoke.UserID != "" || store.gotMappingCreate.ProviderConfigID != "" ||
				store.gotMappingDelete.MappingRef != "" {
				t.Fatalf("mutation store was called for denied %s %s", tc.method, tc.target)
			}

			allowed := httptest.NewRecorder()
			mux.ServeHTTP(allowed, mutationRequest(tc.method, tc.target, tc.body, catalogEnforcedAdminAuth(tc.feature)))
			if allowed.Code < 200 || allowed.Code >= 300 {
				t.Fatalf("allowed feature status = %d, want 2xx: %s", allowed.Code, allowed.Body.String())
			}
		})
	}
}

func TestBrowserSessionAPITokenMutationsRequireTokenCatalogFeature(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{}
	secretCalls := 0
	handler := &LocalIdentityHandler{
		Store: store,
		NewSecret: func() (string, error) {
			secretCalls++
			return "unused-generated-value", nil
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
}

func TestBrowserSessionLocalIdentityAdminRoutesRequireCatalogFeature(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{}
	secretCalls := 0
	handler := &LocalIdentityHandler{
		Store: store,
		NewSecret: func() (string, error) {
			secretCalls++
			return "unused-generated-value", nil
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
}

func TestBrowserSessionAuditReadsRequireAuditCatalogFeature(t *testing.T) {
	t.Parallel()

	reader := &fakeAdminAuditReader{}
	handler := &AdminIdentityReadHandler{Audit: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, path := range []string{"/api/v0/auth/admin/audit/events", "/api/v0/auth/admin/audit/summary"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, path, catalogEnforcedAdminAuth("identity_admin")))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("GET %s wrong feature status = %d, want 403: %s", path, rec.Code, rec.Body.String())
		}

		allowed := httptest.NewRecorder()
		mux.ServeHTTP(allowed, adminRequest(t, http.MethodGet, path, catalogEnforcedAdminAuth("audit_export")))
		if allowed.Code != http.StatusOK {
			t.Fatalf("GET %s allowed feature status = %d, want 200: %s", path, allowed.Code, allowed.Body.String())
		}
	}
}
