// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

// providerConfigSecretCanary is the shared hosted-governance redaction
// registry's SensitiveSecretValue canary, used across this file's and
// admin_provider_config_mutations_test.go's negative-leakage tests.
const providerConfigSecretCanary = "correct-horse-redaction-canary"

// assertNoCanaryInResponseAndAudit is the shared negative-leakage assertion
// for a handler call: neither the HTTP response body nor any recorded
// governance-audit event (JSON-marshaled) may contain the secret canary, on
// EITHER the SurfaceAPIMCPBodies or SurfaceAuditEvents policy.
func assertNoCanaryInResponseAndAudit(t *testing.T, label string, rec *httptest.ResponseRecorder, audit *recordingAuditAppender) {
	t.Helper()
	registry := redact.HostedGovernanceRegistry()
	if err := registry.AssertNoForbiddenCanary(redact.SurfaceAPIMCPBodies, rec.Body.Bytes()); err != nil {
		t.Errorf("%s: response body failed canary check: %v", label, err)
	}
	if strings.Contains(rec.Body.String(), providerConfigSecretCanary) {
		t.Errorf("%s: response body contains the raw canary directly: %s", label, rec.Body.String())
	}
	eventsJSON, err := json.Marshal(audit.events)
	if err != nil {
		t.Fatalf("%s: json.Marshal(audit.events): %v", label, err)
	}
	if err := registry.AssertNoForbiddenCanary(redact.SurfaceAuditEvents, eventsJSON); err != nil {
		t.Errorf("%s: audit events failed canary check: %v\nevents: %s", label, err, eventsJSON)
	}
	if strings.Contains(string(eventsJSON), providerConfigSecretCanary) {
		t.Errorf("%s: audit events contain the raw canary directly: %s", label, eventsJSON)
	}
}

// TestProviderConfigMutationResponsesAndAuditNeverLeakCanary extends the
// negative-leakage canary check (previously only the create response body)
// to update/revert/enable/disable/test-connection response bodies AND every
// recorded governance-audit event, on both the SurfaceAPIMCPBodies and
// SurfaceAuditEvents policies. The update request embeds the canary as the
// (write-only) client_secret, proving the handler never echoes a request
// secret back into its response or an audit event.
func TestProviderConfigMutationResponsesAndAuditNeverLeakCanary(t *testing.T) {
	t.Parallel()

	canaryUpdateBody := `{"provider_kind":"oidc","issuer":"https://idp.example.test","client_id":"client-1","client_secret":"` + providerConfigSecretCanary + `","scopes":["openid"]}`

	cases := []struct {
		label  string
		method string
		target string
		body   string
	}{
		{"update", http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1", canaryUpdateBody},
		{"revert", http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/revert", `{"revision_id":"rev_1"}`},
		{"enable", http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/enable", ""},
		{"disable", http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/disable", ""},
		{"test-connection", http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/test-connection", ""},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()
			store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{
				ProviderConfigID: "pc_1", RevisionID: "rev_1", Status: "active", Found: true, Changed: true,
			}}
			tester := &fakeProviderConfigConnectionTester{result: AdminProviderConfigConnectionTestResult{
				OK: true, Detail: "discovery and jwks reachable", RevisionID: "rev_1",
			}}
			audit := &recordingAuditAppender{}
			mux := newProviderConfigMutationMux(store, tester, audit)

			var req *http.Request
			if tc.body == "" {
				req = httptest.NewRequest(tc.method, tc.target, nil)
			} else {
				req = httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			}
			req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertNoCanaryInResponseAndAudit(t, tc.label, rec, audit)
		})
	}
}

// TestProviderConfigReadResponsesNeverLeakCanary extends the negative
// -leakage canary check to the get-detail and list-revisions response
// bodies (list was already covered), using a fake store whose
// SecretFingerprint/SecretKeyID are derived-looking values that must never
// coincidentally equal or contain the canary — and confirms directly that
// neither route's JSON encoding path can carry it.
func TestProviderConfigReadResponsesNeverLeakCanary(t *testing.T) {
	t.Parallel()
	registry := redact.HostedGovernanceRegistry()

	detailStore := &fakeAdminProviderConfigReadStore{
		details: map[string]AdminProviderConfigDetail{
			"pc_1": {
				ProviderConfigID: "pc_1", ProviderKind: "external_oidc", Status: "active",
				HasSecret: true, SecretFingerprint: "abc12345", SecretKeyID: "k1", ManagedBy: "database",
			},
		},
	}
	mux := newProviderConfigReadMux(detailStore)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminReadRequest(http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_1", providerConfigAdminAuth()))
	if err := registry.AssertNoForbiddenCanary(redact.SurfaceAPIMCPBodies, rec.Body.Bytes()); err != nil {
		t.Errorf("get-detail response failed canary check: %v", err)
	}

	revisionsStore := &fakeAdminProviderConfigReadStore{
		revisions: map[string][]AdminProviderConfigRevisionItem{
			"pc_1": {{RevisionID: "rev_1", Status: "active", HasSecret: true}},
		},
	}
	mux = newProviderConfigReadMux(revisionsStore)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, adminReadRequest(http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_1/revisions", providerConfigAdminAuth()))
	if err := registry.AssertNoForbiddenCanary(redact.SurfaceAPIMCPBodies, rec.Body.Bytes()); err != nil {
		t.Errorf("list-revisions response failed canary check: %v", err)
	}
}
