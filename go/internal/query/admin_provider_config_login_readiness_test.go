// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleEnableAdminProviderConfigRejectsMissingRedirectURL proves issue
// #5604's fix: enabling a login-capable OIDC or GitHub provider whose stored
// configuration has no redirect_url is rejected with a clear 400 naming the
// missing field, even though the connection test passes (redirect_url is
// optional at create/test-connection but required by
// oidclogin/githublogin.ResolveSealedProviderConfig at login) — instead of
// silently activating a provider that will 503 on every login attempt.
func TestHandleEnableAdminProviderConfigRejectsMissingRedirectURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		label        string
		providerKind string
		configJSON   map[string]any
	}{
		{
			label:        "oidc",
			providerKind: "external_oidc",
			configJSON:   map[string]any{"issuer": "https://idp.example.test", "client_id": "client-1"},
		},
		{
			label:        "github",
			providerKind: "external_github",
			configJSON:   map[string]any{"client_id": "gh-client-1", "allowed_orgs": []any{"eshu-hq"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()
			store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{Found: true, Changed: true, Status: "active"}}
			tester := &fakeProviderConfigConnectionTester{result: AdminProviderConfigConnectionTestResult{OK: true, Detail: "ok", RevisionID: "rev_tested_1"}}
			readStore := &fakeAdminProviderConfigReadStore{details: map[string]AdminProviderConfigDetail{
				"pc_1": {ProviderConfigID: "pc_1", ProviderKind: tc.providerKind, Status: "draft", Configuration: tc.configJSON},
			}}
			audit := &recordingAuditAppender{}
			mux := newProviderConfigMutationMux(store, tester, audit, readStore)

			req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/enable", nil)
			req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("enable with no redirect_url status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "redirect_url") {
				t.Fatalf("400 body does not name the missing field redirect_url: %s", rec.Body.String())
			}
			if store.gotEnableID != "" {
				t.Fatal("store.EnableProviderConfig was called despite a missing required login field")
			}
			if !audit.hasReason("provider_config_enable_missing_login_field") {
				t.Errorf("enable rejection did not audit provider_config_enable_missing_login_field: %#v", audit.events)
			}
		})
	}
}

// TestHandleEnableAdminProviderConfigRejectsMissingSAMLLoginFields proves the
// same enable-time guard for SAML: service_provider_entity_id,
// service_provider_acs_url, and metadata_xml are all optional at
// write/test-connection time but required by
// samlauth.ResolveSealedProviderConfig at login (metadata_url alone is not
// enough — the login resolver only accepts inline metadata_xml).
func TestHandleEnableAdminProviderConfigRejectsMissingSAMLLoginFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		label      string
		configJSON map[string]any
		wantField  string
	}{
		{
			label:      "missing service_provider_entity_id and acs_url",
			configJSON: map[string]any{"entity_id": "https://sp.example.test", "metadata_xml": "<md/>"},
			wantField:  "service_provider_entity_id",
		},
		{
			label: "metadata_url only, no inline metadata_xml",
			configJSON: map[string]any{
				"entity_id": "https://sp.example.test", "metadata_url": "https://idp.example.test/metadata",
				"service_provider_entity_id": "https://eshu.example.test/sp", "service_provider_acs_url": "https://eshu.example.test/sso/acs",
			},
			wantField: "metadata_xml",
		},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()
			store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{Found: true, Changed: true, Status: "active"}}
			tester := &fakeProviderConfigConnectionTester{result: AdminProviderConfigConnectionTestResult{OK: true, Detail: "ok", RevisionID: "rev_tested_1"}}
			readStore := &fakeAdminProviderConfigReadStore{details: map[string]AdminProviderConfigDetail{
				"pc_1": {ProviderConfigID: "pc_1", ProviderKind: "external_saml", Status: "draft", Configuration: tc.configJSON},
			}}
			audit := &recordingAuditAppender{}
			mux := newProviderConfigMutationMux(store, tester, audit, readStore)

			req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/enable", nil)
			req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("enable with missing saml login field status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantField) {
				t.Fatalf("400 body does not name the missing field %s: %s", tc.wantField, rec.Body.String())
			}
			if store.gotEnableID != "" {
				t.Fatal("store.EnableProviderConfig was called despite a missing required login field")
			}
		})
	}
}

// TestHandleEnableAdminProviderConfigSucceedsWithRedirectURLPresent proves
// the guard does not false-reject a provider whose stored configuration
// already carries every field ResolveSealedProviderConfig needs for login.
func TestHandleEnableAdminProviderConfigSucceedsWithRedirectURLPresent(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{Found: true, Changed: true, Status: "active"}}
	tester := &fakeProviderConfigConnectionTester{result: AdminProviderConfigConnectionTestResult{OK: true, Detail: "ok", RevisionID: "rev_tested_1"}}
	readStore := &fakeAdminProviderConfigReadStore{details: map[string]AdminProviderConfigDetail{
		"pc_1": {
			ProviderConfigID: "pc_1", ProviderKind: "external_oidc", Status: "draft",
			Configuration: map[string]any{
				"issuer": "https://idp.example.test", "client_id": "client-1",
				"redirect_url": "https://admin.example.test/auth/oidc/callback",
			},
		},
	}}
	mux := newProviderConfigMutationMux(store, tester, &recordingAuditAppender{}, readStore)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/enable", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("enable with redirect_url present status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.gotEnableID != "pc_1" {
		t.Fatalf("store.EnableProviderConfig id = %q, want pc_1 (enable should have proceeded)", store.gotEnableID)
	}
}

// TestHandleEnableAdminProviderConfigUnknownKindUnaffected proves the guard
// only applies to the login-capable kinds it knows about (external_oidc,
// external_saml, external_github). A provider config of any other kind —
// e.g. a future bearer-only provider that never resolves for browser login —
// is not subject to this check and enables normally.
func TestHandleEnableAdminProviderConfigUnknownKindUnaffected(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{Found: true, Changed: true, Status: "active"}}
	tester := &fakeProviderConfigConnectionTester{result: AdminProviderConfigConnectionTestResult{OK: true, Detail: "ok", RevisionID: "rev_tested_1"}}
	readStore := &fakeAdminProviderConfigReadStore{details: map[string]AdminProviderConfigDetail{
		"pc_1": {ProviderConfigID: "pc_1", ProviderKind: "external_bearer_token", Status: "draft", Configuration: map[string]any{}},
	}}
	mux := newProviderConfigMutationMux(store, tester, &recordingAuditAppender{}, readStore)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/enable", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("enable of a non-login-capable provider kind status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.gotEnableID != "pc_1" {
		t.Fatalf("store.EnableProviderConfig id = %q, want pc_1", store.gotEnableID)
	}
}

// TestHandleEnableAdminProviderConfigReadinessGapFailsOpen proves the
// documented fail-open behavior of the guard (see
// AdminProviderConfigMutationHandler.ReadStore's doc comment): when ReadStore
// is nil, or its lookup errors, or it finds nothing for the provider config
// id, the enable path proceeds exactly as it did before issue #5604 — the
// mandatory test-connection gate still runs, but this secondary guard never
// turns into a new hard dependency or a new source of enable failures.
func TestHandleEnableAdminProviderConfigReadinessGapFailsOpen(t *testing.T) {
	t.Parallel()
	cases := []struct {
		label     string
		readStore AdminProviderConfigReadStore
	}{
		{"nil ReadStore", nil},
		{"lookup error", &fakeAdminProviderConfigReadStore{forceErr: errors.New("read store unavailable")}},
		{"provider config not found in read store", &fakeAdminProviderConfigReadStore{details: map[string]AdminProviderConfigDetail{}}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()
			store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{Found: true, Changed: true, Status: "active"}}
			tester := &fakeProviderConfigConnectionTester{result: AdminProviderConfigConnectionTestResult{OK: true, Detail: "ok", RevisionID: "rev_tested_1"}}
			handler := &AdminProviderConfigMutationHandler{Store: store, Tester: tester, Audit: &recordingAuditAppender{}, ReadStore: tc.readStore}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/enable", nil)
			req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("enable status = %d, want 200 (guard must fail open): %s", rec.Code, rec.Body.String())
			}
			if store.gotEnableID != "pc_1" {
				t.Fatalf("store.EnableProviderConfig id = %q, want pc_1 (enable should still have proceeded)", store.gotEnableID)
			}
		})
	}
}

// TestProviderConfigMissingLoginField unit-tests the field-presence check
// directly against each login-capable kind, mirroring exactly the fields
// githublogin/oidclogin/samlauth's ResolveSealedProviderConfig functions
// require but buildProviderConfigWrite (create/update) does not.
func TestProviderConfigMissingLoginField(t *testing.T) {
	t.Parallel()
	cases := []struct {
		label        string
		providerKind string
		configJSON   map[string]any
		want         string
	}{
		{"oidc missing redirect_url", "external_oidc", map[string]any{"issuer": "i", "client_id": "c"}, "redirect_url"},
		{"oidc with redirect_url", "external_oidc", map[string]any{"redirect_url": "https://x/callback"}, ""},
		{"oidc blank redirect_url", "external_oidc", map[string]any{"redirect_url": "   "}, "redirect_url"},
		{"github missing redirect_url", "external_github", map[string]any{"client_id": "c"}, "redirect_url"},
		{"github with redirect_url", "external_github", map[string]any{"redirect_url": "https://x/callback"}, ""},
		{"saml missing sp_entity_id first", "external_saml", map[string]any{"metadata_xml": "<md/>", "service_provider_acs_url": "https://x/acs"}, "service_provider_entity_id"},
		{"saml missing acs_url", "external_saml", map[string]any{"metadata_xml": "<md/>", "service_provider_entity_id": "https://x/sp"}, "service_provider_acs_url"},
		{"saml missing metadata_xml", "external_saml", map[string]any{"service_provider_entity_id": "https://x/sp", "service_provider_acs_url": "https://x/acs"}, "metadata_xml"},
		{"saml complete", "external_saml", map[string]any{"metadata_xml": "<md/>", "service_provider_entity_id": "https://x/sp", "service_provider_acs_url": "https://x/acs"}, ""},
		{"unknown kind never flagged", "external_bearer_token", map[string]any{}, ""},
		{"nil configuration missing field", "external_oidc", nil, "redirect_url"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()
			got := providerConfigMissingLoginField(tc.providerKind, tc.configJSON)
			if got != tc.want {
				t.Errorf("providerConfigMissingLoginField(%q, %v) = %q, want %q", tc.providerKind, tc.configJSON, got, tc.want)
			}
		})
	}
}
