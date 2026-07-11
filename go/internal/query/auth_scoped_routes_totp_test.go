// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestScopedTenantFilterAllowsTOTPEnrollment guards the integration gap a
// reviewer caught (issue #4986, PR #5065): the self-service TOTP enrollment
// routes are called from the profile page with only a browser-session cookie,
// so AuthMiddleware must treat them as tenant-filter eligible — otherwise the
// enrollment request is rejected before the handler (which resolves the acting
// user from the session subject) ever runs. Unrelated auth mutations that are
// NOT self-service must stay rejected.
func TestScopedTenantFilterAllowsTOTPEnrollment(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/api/v0/auth/local/mfa/totp/begin",
		"/api/v0/auth/local/mfa/totp/confirm",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		if !scopedHTTPRouteSupportsTenantFilter(req) {
			t.Fatalf("scopedHTTPRouteSupportsTenantFilter(POST %s) = false, want true so browser-session enrollment is not rejected before the handler", path)
		}
	}
	// A GET to the same path (no such route) and an unrelated auth mutation must
	// not be admitted by the enrollment allowance.
	if scopedHTTPRouteSupportsTenantFilter(httptest.NewRequest(http.MethodGet, "/api/v0/auth/local/mfa/totp/begin", nil)) {
		t.Fatal("scopedHTTPRouteSupportsTenantFilter(GET totp/begin) = true, want false (only POST enrolls)")
	}
	if scopedHTTPRouteSupportsTenantFilter(httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/bootstrap", nil)) {
		t.Fatal("scopedHTTPRouteSupportsTenantFilter(POST bootstrap) = true, want false")
	}
}
