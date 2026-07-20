// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

// GitHub admin CRUD create proofs (issue #5166, F-5), split from
// admin_provider_config_mutations_test.go to keep both files under the
// repo's 500-line cap. Reuses that file's fakeAdminProviderConfigMutationStore,
// newProviderConfigMutationMux, recordingAuditAppender, and
// providerConfigAdminAuth helpers (same package).

const validGitHubCreateBody = `{"provider_kind":"github","client_id":"gh-client-1","client_secret":"gh-s3cr3t-value","allowed_orgs":["Eshu-HQ"]}`

// TestHandleCreateAdminProviderConfigGitHub proves the admin CRUD create
// path accepts provider_kind "github", builds an "external_github" store
// write with a sealed client_secret, and never leaks the secret in the
// response — mirroring TestHandleCreateAdminProviderConfig's OIDC proof
// exactly.
func TestHandleCreateAdminProviderConfigGitHub(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{
		ProviderConfigID: "pc_gh_1", RevisionID: "rev_1", Status: "draft", Found: true, Changed: true,
	}}
	audit := &recordingAuditAppender{}
	mux := newProviderConfigMutationMux(store, nil, audit)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs", strings.NewReader(validGitHubCreateBody))
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("handleCreate status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.gotCreate.ProviderKind != "external_github" {
		t.Fatalf("store received ProviderKind = %q, want external_github", store.gotCreate.ProviderKind)
	}
	if store.gotCreate.PlaintextSecret == "" || !strings.Contains(store.gotCreate.PlaintextSecret, "gh-s3cr3t-value") {
		t.Fatalf("store did not receive the plaintext secret to seal: %q", store.gotCreate.PlaintextSecret)
	}
	if !strings.Contains(store.gotCreate.Configuration, "eshu-hq") {
		t.Fatalf("store configuration did not lowercase-normalize allowed_orgs: %q", store.gotCreate.Configuration)
	}
	if !audit.hasReason("provider_config_created") {
		t.Errorf("create did not audit provider_config_created: %#v", audit.events)
	}
	registry := redact.HostedGovernanceRegistry()
	if err := registry.AssertNoForbiddenCanary(redact.SurfaceAPIMCPBodies, rec.Body.Bytes()); err != nil {
		t.Errorf("response body failed canary check: %v", err)
	}
	if strings.Contains(rec.Body.String(), "gh-s3cr3t-value") {
		t.Fatalf("create response leaked the plaintext secret: %s", rec.Body.String())
	}
}

// TestHandleCreateAdminProviderConfigGitHubRejectsEmptyAllowedOrgs proves a
// GitHub provider config with no allowed_orgs is rejected at the admin API
// boundary (issue #5166: a GitHub provider with no org allow-list would let
// any GitHub account sign in) — the same fail-closed contract
// githublogin.ValidateConfig enforces for the env-file path.
func TestHandleCreateAdminProviderConfigGitHubRejectsEmptyAllowedOrgs(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{}
	mux := newProviderConfigMutationMux(store, nil, &recordingAuditAppender{})

	body := `{"provider_kind":"github","client_id":"gh-client-1","client_secret":"gh-s3cr3t-value"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs", strings.NewReader(body))
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if store.gotCreate.ProviderKind != "" {
		t.Fatalf("store should not have been called for an invalid request: %#v", store.gotCreate)
	}
}
