// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// The shared test double rides the pinned admission shape, not a
// parallel signature: if the port re-signs GetSignInPolicy, this file
// fails compilation the same way the frozen contract test fails. It
// proves the admin-leg wire proof below exercises the pinned read.
var _ auth.SignInPolicyReadAdmission = (*fakeSignInPolicyReadStore)(nil)

// frozenAdminSignInPolicyKeys pins the exact admin detail key set the
// GET /api/v0/auth/admin/sign-in-policy serves: the 8 always-present
// keys. The 2 conditional keys (sso_admin_verified_at,
// sso_admin_verified_provider_config_id) are pinned separately below.
// An added, removed, or renamed key fails here until the pin record
// (contracts/signin-policy-read-admission.md) moves with it.
var frozenAdminSignInPolicyKeys = []string{
	"absolute_timeout_seconds",
	"allow_local_user_creation",
	"idle_timeout_seconds",
	"policy_revision_hash",
	"require_mfa_for_all_users",
	"require_sso",
	"tenant_id",
	"updated_at",
}

// adminSignInPolicyTestContext carries the all-scope admin identity
// the admin GET requires, mirroring the sibling read tests.
func adminSignInPolicyTestContext(r *http.Request) *http.Request {
	return r.WithContext(ContextWithAuthContext(r.Context(), AuthContext{
		Mode: AuthModeBrowserSession, AllScopes: true, TenantID: "tenant_a",
	}))
}

// serveAdminSignInPolicyGet drives the mounted admin GET with a stub
// store and returns the recorder.
func serveAdminSignInPolicyGet(t *testing.T, store *fakeSignInPolicyReadStore) *httptest.ResponseRecorder {
	t.Helper()

	handler := &SignInPolicyReadHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/admin/sign-in-policy", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyTestContext(req))
	return rec
}

// TestSignInPolicyAdminGetWireIsByteIdenticalToCoreProjection proves
// the admin-leg verbatim coverage behind SignInPolicyReadAdmission
// v0: the served bytes are exactly the core detail projection of the
// SignInPolicy value the pinned read returns. No wire key exists
// outside the pinned read's return shape — the body carries every
// field of the port value and nothing else — so a dest consumer
// forwarding core bytes serves this leg verbatim by construction.
func TestSignInPolicyAdminGetWireIsByteIdenticalToCoreProjection(t *testing.T) {
	t.Parallel()

	verifiedAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 7, 2, 3, 4, 5, 0, time.UTC)
	policy := SignInPolicy{
		TenantID:                         "tenant_a",
		RequireSSO:                       true,
		AllowLocalUserCreation:           false,
		RequireMFAForAllUsers:            true,
		IdleTimeoutSeconds:               900,
		AbsoluteTimeoutSeconds:           43200,
		SSOAdminVerifiedAt:               verifiedAt,
		SSOAdminVerifiedProviderConfigID: "pc_abc",
		PolicyRevisionHash:               "sha256:rev1",
		UpdatedAt:                        updatedAt,
	}
	rec := serveAdminSignInPolicyGet(t, &fakeSignInPolicyReadStore{policy: policy})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	want := map[string]any{
		"tenant_id":                             "tenant_a",
		"require_sso":                           true,
		"allow_local_user_creation":             false,
		"require_mfa_for_all_users":             true,
		"idle_timeout_seconds":                  float64(900),
		"absolute_timeout_seconds":              float64(43200),
		"sso_admin_verified_at":                 "2026-06-01T00:00:00Z",
		"sso_admin_verified_provider_config_id": "pc_abc",
		"policy_revision_hash":                  "sha256:rev1",
		"updated_at":                            "2026-07-02T03:04:05Z",
	}
	if len(body) != len(want) {
		t.Fatalf("admin detail keys = %d, want %d: %#v", len(body), len(want), body)
	}
	for key, wantValue := range want {
		if body[key] != wantValue {
			t.Errorf("admin detail[%q] = %#v, want %#v", key, body[key], wantValue)
		}
	}
}

// TestSignInPolicyAdminGetKeysAreFrozen proves the served admin key
// set is exactly the frozen list: no credential field, no tenant
// echo beyond tenant_id, no extra key the pin record does not name.
func TestSignInPolicyAdminGetKeysAreFrozen(t *testing.T) {
	t.Parallel()

	rec := serveAdminSignInPolicyGet(t, &fakeSignInPolicyReadStore{policy: SignInPolicy{
		TenantID: "tenant_a",
	}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	got := make([]string, 0, len(body))
	for key := range body {
		got = append(got, key)
	}
	sort.Strings(got)
	if len(got) != len(frozenAdminSignInPolicyKeys) {
		t.Fatalf("admin detail keys = %q, want frozen %q", got, frozenAdminSignInPolicyKeys)
	}
	for i := range got {
		if got[i] != frozenAdminSignInPolicyKeys[i] {
			t.Errorf("admin detail keys differ at index %d: got %q, want %q", i, got[i], frozenAdminSignInPolicyKeys[i])
		}
	}
}

// TestSignInPolicyAdminGetStoreErrorFailsClosedWithFixedMessage proves
// the fail-closed leg the pin record names: a store error is a 500
// with the fixed signInPolicyReadFailedMessage, never a partial or
// defaulted policy.
func TestSignInPolicyAdminGetStoreErrorFailsClosedWithFixedMessage(t *testing.T) {
	t.Parallel()

	rec := serveAdminSignInPolicyGet(t, &fakeSignInPolicyReadStore{err: errors.New("policy store unavailable")})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if body["detail"] != signInPolicyReadFailedMessage {
		t.Errorf("failure body = %#v, want the fixed message %q in detail", body, signInPolicyReadFailedMessage)
	}
}
