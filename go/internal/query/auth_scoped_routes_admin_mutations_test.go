// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestScopedAuthAdminMutationRoute verifies the tenant-admin identity mutation
// routes are recognized only for their exact unsafe method and path, and that
// path-templated routes require a non-empty, single-segment id. CSRF for
// browser-session callers is enforced by the auth middleware ahead of the
// handler; listing these here only makes the cookie-session tenant filter
// eligible, it does not relax CSRF.
func TestScopedAuthAdminMutationRoute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{"revoke invitation", http.MethodPost, "/api/v0/auth/local/invitations/inv_1/revoke", true},
		{"grant role assignment", http.MethodPost, "/api/v0/auth/admin/role-assignments", true},
		{"revoke role assignment", http.MethodPost, "/api/v0/auth/admin/role-assignments/revoke", true},
		{"create group mapping", http.MethodPost, "/api/v0/auth/admin/idp-group-mappings", true},
		{"delete group mapping", http.MethodDelete, "/api/v0/auth/admin/idp-group-mappings/ref_1", true},
		// Wrong method for the path.
		{"invitation revoke wrong method", http.MethodGet, "/api/v0/auth/local/invitations/inv_1/revoke", false},
		{"group mapping create wrong method", http.MethodDelete, "/api/v0/auth/admin/idp-group-mappings", false},
		// Empty / multi-segment ids must not match.
		{"empty invite id", http.MethodPost, "/api/v0/auth/local/invitations//revoke", false},
		{"multi-segment mapping ref", http.MethodDelete, "/api/v0/auth/admin/idp-group-mappings/a/b", false},
		{"empty mapping ref", http.MethodDelete, "/api/v0/auth/admin/idp-group-mappings/", false},
		// Unrelated routes.
		{"unrelated", http.MethodPost, "/api/v0/auth/local/bootstrap", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if got := scopedAuthAdminMutationRoute(req); got != tc.want {
				t.Fatalf("scopedAuthAdminMutationRoute(%s %s) = %v, want %v", tc.method, tc.path, got, tc.want)
			}
		})
	}
}

// TestScopedTenantFilterAllowsAdminMutations guards the browser-session
// deployment path: admins drive the console with only a session cookie, so the
// auth middleware must treat these tenant-admin mutations as tenant-filter
// eligible — otherwise the admin console mutation is rejected with
// permission_denied before the (all-scope, tenant-scoped) handlers ever run.
func TestScopedTenantFilterAllowsAdminMutations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v0/auth/local/invitations/inv_1/revoke"},
		{http.MethodPost, "/api/v0/auth/admin/role-assignments"},
		{http.MethodPost, "/api/v0/auth/admin/role-assignments/revoke"},
		{http.MethodPost, "/api/v0/auth/admin/idp-group-mappings"},
		{http.MethodDelete, "/api/v0/auth/admin/idp-group-mappings/ref_1"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if !scopedHTTPRouteSupportsTenantFilter(req) {
			t.Fatalf("scopedHTTPRouteSupportsTenantFilter(%s %s) = false, want true", tc.method, tc.path)
		}
	}
}
