// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// enforcedAdminAuth builds an all-scope browser-session auth context with an
// enforced permission-catalog snapshot granting exactly the passed features.
// Test-local mirror of the query root's catalogEnforcedAdminAuth helper,
// which this package cannot import: the bodies are identical on purpose so
// the permission-gating proof below runs against the same auth values.
func enforcedAdminAuth(features ...string) queryauth.AuthContext {
	return queryauth.AuthContext{
		Mode:                         queryauth.AuthModeBrowserSession,
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
//
// Moved from the query root's browser-session permission proof (#6060, lane B
// S1) with only the package repoints (ReadHandler, queryauth, enforcedAdminAuth);
// the cases and assertions are unchanged.
func TestBrowserSessionHandleListInvitationsHandleListRoleAssignmentsHandleListRolesHandleListIdPProvidersHandleListIdPGroupMappingsHandleListAPITokensRequireCatalogFeature(t *testing.T) {
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
			handler := &ReadHandler{Store: store}
			mux := http.NewServeMux()
			handler.Mount(mux)

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, tc.path, enforcedAdminAuth("ask_search")))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("wrong feature status = %d, want 403: %s", rec.Code, rec.Body.String())
			}
			if store.gotTenantID != "" || store.gotWorkspaceID != "" {
				t.Fatalf("store was called for denied %s with scope %q/%q", tc.path, store.gotTenantID, store.gotWorkspaceID)
			}

			allowed := httptest.NewRecorder()
			mux.ServeHTTP(allowed, adminRequest(t, http.MethodGet, tc.path, enforcedAdminAuth(tc.feature)))
			if allowed.Code != http.StatusOK {
				t.Fatalf("allowed feature status = %d, want 200: %s", allowed.Code, allowed.Body.String())
			}
		})
	}
}

// TestBrowserSessionAdminMutationsRequireCatalogFeature covers the unsafe
// browser-session admin routes. CSRF is enforced by middleware before these
// handlers; this test guards the handler-level permission family after auth.
//
// Moved from the query root's browser-session permission proof (#6060, lane B
// S1) with only the package repoints; the cases and assertions are unchanged.
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
				inviteResult:        InvitationRevokeResult{Found: true, Revoked: true, Status: "revoked"},
				grantResult:         RoleAssignmentMutationResult{RoleValid: true, UserValid: true, Status: "active", Changed: true},
				roleRevokeResult:    RoleAssignmentMutationResult{Status: "revoked", Changed: true},
				mappingCreateResult: IdPGroupMappingCreateResult{ProviderValid: true, RoleValid: true, MappingRef: "ref_1", Status: "active", Created: true},
				mappingDeleteResult: IdPGroupMappingDeleteResult{Found: true, Deleted: true},
			}
			mux := newMutationMux(store, &querytestutil.FakeGovernanceAuditAppender{})

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, mutationRequest(tc.method, tc.target, tc.body, enforcedAdminAuth("ask_search")))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("wrong feature status = %d, want 403: %s", rec.Code, rec.Body.String())
			}
			if store.gotInviteRevoke.InviteID != "" || store.gotGrant.UserID != "" ||
				store.gotRoleRevoke.UserID != "" || store.gotMappingCreate.ProviderConfigID != "" ||
				store.gotMappingDelete.MappingRef != "" {
				t.Fatalf("mutation store was called for denied %s %s", tc.method, tc.target)
			}

			allowed := httptest.NewRecorder()
			mux.ServeHTTP(allowed, mutationRequest(tc.method, tc.target, tc.body, enforcedAdminAuth(tc.feature)))
			if allowed.Code < 200 || allowed.Code >= 300 {
				t.Fatalf("allowed feature status = %d, want 2xx: %s", allowed.Code, allowed.Body.String())
			}
		})
	}
}

// TestBrowserSessionHandleListAuditEventsAuditReadsRequireAuditCatalogFeature pins
// the audit-reads permission family.
//
// Moved from the query root's browser-session permission proof (#6060, lane B
// S1) with only the package repoints; the cases and assertions are unchanged.
func TestBrowserSessionHandleListAuditEventsAuditReadsRequireAuditCatalogFeature(t *testing.T) {
	t.Parallel()

	reader := &fakeAdminAuditReader{}
	handler := &ReadHandler{Audit: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, path := range []string{"/api/v0/auth/admin/audit/events", "/api/v0/auth/admin/audit/summary"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, path, enforcedAdminAuth("identity_admin")))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("GET %s wrong feature status = %d, want 403: %s", path, rec.Code, rec.Body.String())
		}

		allowed := httptest.NewRecorder()
		mux.ServeHTTP(allowed, adminRequest(t, http.MethodGet, path, enforcedAdminAuth("audit_export")))
		if allowed.Code != http.StatusOK {
			t.Fatalf("GET %s allowed feature status = %d, want 200: %s", path, allowed.Code, allowed.Body.String())
		}
	}
}
