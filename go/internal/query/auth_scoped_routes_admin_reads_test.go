package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestScopedAuthAdminReadRoute verifies the admin identity read routes are
// recognized only for GET and only for the exact admin paths.
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
		{"audit events", http.MethodGet, "/api/v0/auth/admin/audit/events", true},
		{"audit summary", http.MethodGet, "/api/v0/auth/admin/audit/summary", true},
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
// middleware must treat these admin GETs as tenant-filter eligible — otherwise
// the admin console is rejected with permission_denied before the (all-scope,
// tenant-scoped) handlers ever run.
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
