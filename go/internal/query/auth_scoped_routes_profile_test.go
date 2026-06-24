package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestScopedAuthProfileReadRoute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{"profile", http.MethodGet, "/api/v0/auth/profile", true},
		{"sessions", http.MethodGet, "/api/v0/auth/sessions", true},
		{"api-tokens list", http.MethodGet, "/api/v0/auth/local/api-tokens", true},
		{"profile post", http.MethodPost, "/api/v0/auth/profile", false},
		{"sessions delete", http.MethodDelete, "/api/v0/auth/sessions", false},
		{"unrelated", http.MethodGet, "/api/v0/auth/local/bootstrap", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if got := scopedAuthProfileReadRoute(req); got != tc.want {
				t.Fatalf("scopedAuthProfileReadRoute(%s %s) = %v, want %v", tc.method, tc.path, got, tc.want)
			}
		})
	}
}

// TestScopedTenantFilterAllowsProfileReads guards the integration gap a reviewer
// caught: in a browser-session deployment these GETs are made with only the
// session cookie, so the auth middleware must treat them as tenant-filter
// eligible — otherwise the profile page is rejected with permission_denied
// before the (self-scoped) handlers ever run.
func TestScopedTenantFilterAllowsProfileReads(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/api/v0/auth/profile",
		"/api/v0/auth/sessions",
		"/api/v0/auth/local/api-tokens",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if !scopedHTTPRouteSupportsTenantFilter(req) {
			t.Fatalf("scopedHTTPRouteSupportsTenantFilter(GET %s) = false, want true", path)
		}
	}
}
