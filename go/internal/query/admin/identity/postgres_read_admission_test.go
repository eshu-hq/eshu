// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNewAdminIdentityReadAdmissionNilStoreIsNil proves the D3
// constructor fails closed: no store means no admission, and the
// handler's storeReady gate turns the nil into a 503 — never a
// confident empty page.
func TestNewAdminIdentityReadAdmissionNilStoreIsNil(t *testing.T) {
	t.Parallel()

	if NewAdminIdentityReadAdmission(nil) != nil {
		t.Error("NewAdminIdentityReadAdmission(nil) = non-nil, want nil: an unwired store must fail closed")
	}
}

// TestAdminIdentityReadsNilAuditReturns503 proves both audit legs fail
// closed when the D1/D6 audit-read pin is unwired: the 503 names the
// missing reader instead of inventing an empty audit trail. The nil
// check runs before any authorization, so no credential is needed.
func TestAdminIdentityReadsNilAuditReturns503(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/auth/admin/audit/events",
		"/api/v0/auth/admin/audit/summary",
	} {
		handler := &ReadHandler{Store: &fakeAdminIdentityReadStore{}}
		mux := http.NewServeMux()
		handler.Mount(mux)

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, adminRequest(t, http.MethodGet, path, allScopeAdminAuth("tenant_a", "workspace_a")))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s with nil audit: status = %d, want 503", path, rec.Code)
		}
	}
}
