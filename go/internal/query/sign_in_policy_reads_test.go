// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSignInPolicyHandlePublicGetReturnsOnlyRequireSSO(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyReadStore{policy: SignInPolicy{
		RequireSSO:                       true,
		AllowLocalUserCreation:           false,
		SSOAdminVerifiedProviderConfigID: "pc_abc",
	}}
	handler := &SignInPolicyReadHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/sign-in-policy?tenant_id=tenant_a", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("public sign-in policy response leaked extra fields: %#v", body)
	}
	if got, ok := body["require_sso"].(bool); !ok || !got {
		t.Fatalf("require_sso = %#v, want true", body["require_sso"])
	}
}

func TestSignInPolicyHandlePublicGetDefaultsToFalseWithoutTenantID(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyReadStore{policy: SignInPolicy{RequireSSO: true}}
	handler := &SignInPolicyReadHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/sign-in-policy", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if got, ok := body["require_sso"].(bool); !ok || got {
		t.Fatalf("require_sso = %#v, want false (no tenant resolvable pre-auth)", body["require_sso"])
	}
	if len(store.tenantIDs) != 0 {
		t.Fatalf("store queried with no tenant_id: %#v", store.tenantIDs)
	}
}

func TestSignInPolicyHandleAdminGetRequiresAllScopeAdmin(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyReadStore{policy: SignInPolicy{}}
	handler := &SignInPolicyReadHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/admin/sign-in-policy", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode: AuthModeBrowserSession, AllScopes: false, TenantID: "tenant_a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestSignInPolicyHandleAdminGetReturnsFullDetail(t *testing.T) {
	t.Parallel()

	verifiedAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeSignInPolicyReadStore{policy: SignInPolicy{
		TenantID:                         "tenant_a",
		RequireSSO:                       true,
		AllowLocalUserCreation:           false,
		RequireMFAForAllUsers:            true,
		IdleTimeoutSeconds:               900,
		AbsoluteTimeoutSeconds:           43200,
		SSOAdminVerifiedAt:               verifiedAt,
		SSOAdminVerifiedProviderConfigID: "pc_abc",
		PolicyRevisionHash:               "sha256:rev1",
	}}
	handler := &SignInPolicyReadHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/admin/sign-in-policy", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode: AuthModeBrowserSession, AllScopes: true, TenantID: "tenant_a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if body["sso_admin_verified_provider_config_id"] != "pc_abc" {
		t.Fatalf("sso_admin_verified_provider_config_id = %#v, want pc_abc", body["sso_admin_verified_provider_config_id"])
	}
	if body["sso_admin_verified_at"] == nil {
		t.Fatal("sso_admin_verified_at missing from admin detail response")
	}
}
