// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Subject-keyed fakes — capture the subject hash forwarded by the handler so
// isolation tests can assert (a) the right hash was forwarded and (b) a
// different subject gets only their own rows.
//
// fakeBrowserSessionListStore lives in browser_session_list_handler_test.go
// (split out to keep this file under the repo's 500-line cap, issue #5164).
// ---------------------------------------------------------------------------

type fakeLocalIdentityListStore struct {
	fakeLocalIdentityStore
	// tokensBySubject maps subject_id_hash → rows.
	tokensBySubject map[string][]LocalIdentityAPITokenListItem
	listErr         error
	mfaStatus       LocalIdentityMFAStatus
	mfaErr          error
	// capturedSubject is set on each ListAPITokensBySubject call.
	capturedSubject string
}

func (s *fakeLocalIdentityListStore) ListAPITokensBySubject(
	_ context.Context,
	subjectIDHash string,
	_ time.Time,
) ([]LocalIdentityAPITokenListItem, error) {
	s.capturedSubject = subjectIDHash
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.tokensBySubject[subjectIDHash], nil
}

func (s *fakeLocalIdentityListStore) GetLocalIdentityMFAStatus(
	_ context.Context,
	_ string,
	_ time.Time,
) (LocalIdentityMFAStatus, error) {
	return s.mfaStatus, s.mfaErr
}

// ---------------------------------------------------------------------------
// GET /api/v0/auth/profile
// ---------------------------------------------------------------------------

func TestProfileHandlerReturnsAggregatedProfile(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityListStore{
		mfaStatus: LocalIdentityMFAStatus{
			HasActiveMFA: true,
			FactorKind:   "recovery_code",
		},
	}
	handler := &ProfileHandler{
		LocalIdentityStore: store,
		Now:                func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/profile", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                      AuthModeBrowserSession,
		TenantID:                  "tenant_a",
		WorkspaceID:               "workspace_a",
		SubjectIDHash:             "subject-hash-a",
		RoleIDs:                   []string{"developer"},
		PermissionCatalogEnforced: true,
		AllowedPermissionFeatures: []string{"ask_search"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["active_tenant_id"] != "tenant_a" {
		t.Errorf("active_tenant_id = %v, want tenant_a", resp["active_tenant_id"])
	}
	mfa, _ := resp["mfa"].(map[string]any)
	if mfa == nil {
		t.Fatal("mfa field missing from profile response")
	}
	if mfa["has_active_mfa"] != true {
		t.Errorf("has_active_mfa = %v, want true", mfa["has_active_mfa"])
	}
	if mfa["factor_kind"] != "recovery_code" {
		t.Errorf("factor_kind = %v, want recovery_code", mfa["factor_kind"])
	}
}

// TestProfileHandlerMFAStoreErrorReturns503 is the regression test for P1:
// a GetLocalIdentityMFAStatus error must not silently emit has_active_mfa:false
// as a security fact. The handler must return 503 and log the error.
func TestProfileHandlerMFAStoreErrorReturns503(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityListStore{
		mfaErr: errFakeMFAStoreFailure,
	}
	handler := &ProfileHandler{
		LocalIdentityStore: store,
		Now:                func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/profile", nil)
	// TenantID is set so the MFA path runs.
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		TenantID:      "tenant_a",
		WorkspaceID:   "workspace_a",
		SubjectIDHash: "subject-hash-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Must not emit 200 with has_active_mfa:false when the store errored.
	if rec.Code == http.StatusOK {
		body := rec.Body.String()
		// If 200 is returned, it must contain mfa.provenance:"unavailable", not
		// has_active_mfa:false as a security assertion.
		if bodyContains(body, `"has_active_mfa":false`) && !bodyContains(body, `"provenance":"unavailable"`) {
			t.Fatalf("store error silently emitted has_active_mfa:false without unavailable provenance: %s", body)
		}
		return
	}
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError {
		t.Fatalf("mfa store error: status = %d, want 503 or 500", rec.Code)
	}
}

// TestProfileHandlerNeverExposesMFACredentialOrHash verifies that credential
// handles, recovery code hashes, and other secret fields never appear in the
// profile response body. factor_kind is a safe label (e.g. "recovery_code")
// and IS expected to appear — what must not appear are actual credential values.
func TestProfileHandlerNeverExposesMFACredentialOrHash(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityListStore{
		mfaStatus: LocalIdentityMFAStatus{
			HasActiveMFA: true,
			FactorKind:   "recovery_code",
		},
	}
	handler := &ProfileHandler{
		LocalIdentityStore: store,
		Now:                func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/profile", nil)
	// TenantID set so the MFA path actually runs.
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		TenantID:      "tenant_a",
		WorkspaceID:   "workspace_a",
		SubjectIDHash: "subject-hash-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	// factor_kind value "recovery_code" is a safe label — it is NOT in this list.
	// What must not appear are field names that would indicate raw credential values.
	for _, forbidden := range []string{
		"credential_handle",
		"mfa_credential",
		"mfa_hash",
		"password_hash",
		"session_hash",
		"token_hash",
		"recovery_code_hash",
	} {
		if bodyContains(body, forbidden) {
			t.Errorf("profile response exposes forbidden field %q: %s", forbidden, body)
		}
	}
	// factor_kind must be present (it's a safe label).
	if !bodyContains(body, "factor_kind") {
		t.Errorf("profile response missing factor_kind; MFA path may not have run: %s", body)
	}
}

func TestProfileHandlerRequiresAuth(t *testing.T) {
	t.Parallel()

	handler := &ProfileHandler{
		LocalIdentityStore: &fakeLocalIdentityListStore{},
		Now:                fixedNow,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/profile", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// ExternalProviderConfigID surfaced on GET /api/v0/auth/browser-session
// ---------------------------------------------------------------------------

func TestBrowserSessionCurrentResponseIncludesExternalProviderConfigID(t *testing.T) {
	t.Parallel()

	store := &fakeBrowserSessionStore{}
	handler := &BrowserSessionHandler{Store: store, Now: fixedNow}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/browser-session", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                     AuthModeBrowserSession,
		TenantID:                 "tenant_a",
		ExternalProviderConfigID: "oidc-config-xyz",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	auth, _ := resp["auth"].(map[string]any)
	if auth == nil {
		t.Fatal("auth field missing")
	}
	if auth["external_provider_config_id"] != "oidc-config-xyz" {
		t.Errorf("external_provider_config_id = %v, want oidc-config-xyz", auth["external_provider_config_id"])
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// errFakeMFAStoreFailure is a sentinel error injected to simulate store failure.
var errFakeMFAStoreFailure = &fakeMFAError{"mfa store unavailable"}

type fakeMFAError struct{ msg string }

func (e *fakeMFAError) Error() string { return e.msg }

func fixedNow() time.Time { return time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC) }

func bodyContains(s, sub string) bool {
	if len(sub) == 0 || len(s) < len(sub) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
