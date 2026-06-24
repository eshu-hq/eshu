// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestScopedAuthAdminReadRoute verifies the tenant-admin identity read routes
// are recognized only for GET and only for the exact admin paths. The audit
// routes (/audit/events, /audit/summary) are included in the allowlist (#3717)
// so browser-session tenant admins can reach the handler; the handler's
// auditScope gate enforces correct scoping (own-tenant only for admins,
// all-tenant for shared-operators).
func TestScopedAuthAdminReadRoute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{"invitations", http.MethodGet, "/api/v0/auth/local/invitations", true},
		{"role assignments", http.MethodGet, "/api/v0/auth/admin/role-assignments", true},
		{"roles", http.MethodGet, "/api/v0/auth/admin/roles", true},
		{"idp providers", http.MethodGet, "/api/v0/auth/admin/idp-providers", true},
		{"idp group mappings", http.MethodGet, "/api/v0/auth/admin/idp-group-mappings", true},
		{"api tokens", http.MethodGet, "/api/v0/auth/admin/api-tokens", true},
		// Audit routes are in the allowlist (#3717): tenant admins reach the handler
		// scoped to their tenant; shared-operators see all events.
		{"audit events included", http.MethodGet, "/api/v0/auth/admin/audit/events", true},
		{"audit summary included", http.MethodGet, "/api/v0/auth/admin/audit/summary", true},
		{"invitations post", http.MethodPost, "/api/v0/auth/local/invitations", false},
		{"roles delete", http.MethodDelete, "/api/v0/auth/admin/roles", false},
		{"unrelated", http.MethodGet, "/api/v0/auth/local/bootstrap", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if got := scopedAuthAdminReadRoute(req); got != tc.want {
				t.Fatalf("scopedAuthAdminReadRoute(%s %s) = %v, want %v", tc.method, tc.path, got, tc.want)
			}
		})
	}
}

// TestScopedTenantFilterAllowsAdminReads guards the browser-session deployment
// path: admins drive the console with only a session cookie, so the auth
// middleware must treat these tenant-admin GETs as tenant-filter eligible —
// otherwise the admin console is rejected with permission_denied before the
// (all-scope, tenant-scoped) handlers ever run.
//
// The audit routes are included (#3717): browser-session tenant admins must
// reach the audit handlers so auditScope can scope the read to their tenant.
func TestScopedTenantFilterAllowsAdminReads(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/api/v0/auth/local/invitations",
		"/api/v0/auth/admin/role-assignments",
		"/api/v0/auth/admin/roles",
		"/api/v0/auth/admin/idp-providers",
		"/api/v0/auth/admin/idp-group-mappings",
		"/api/v0/auth/admin/api-tokens",
		"/api/v0/auth/admin/audit/events",
		"/api/v0/auth/admin/audit/summary",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if !scopedHTTPRouteSupportsTenantFilter(req) {
			t.Fatalf("scopedHTTPRouteSupportsTenantFilter(GET %s) = false, want true", path)
		}
	}
}

// TestAuditRoutesInTenantFilterAllowlist confirms the audit routes ARE in the
// browser-session tenant-filter allowlist (#3717). Tenant admins (AllScopes +
// TenantID, cookie session) must reach the audit handler so auditScope can
// return only their own tenant's events. The handler enforces scoping; the
// allowlist only gates whether the browser-session middleware forwards the
// request at all.
func TestAuditRoutesInTenantFilterAllowlist(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/api/v0/auth/admin/audit/events",
		"/api/v0/auth/admin/audit/summary",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if !scopedHTTPRouteSupportsTenantFilter(req) {
			t.Fatalf("scopedHTTPRouteSupportsTenantFilter(GET %s) = false, want true: "+
				"audit routes must be in the browser-session allowlist so tenant admins can reach the handler (#3717)", path)
		}
	}
}
