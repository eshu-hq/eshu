// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestAuthMiddlewareWithScopedTokensAllowsGovernanceStatusRoute(t *testing.T) {
	t.Parallel()

	const (
		rawPolicyBody    = `{"tenant":"private-team","endpoint":"https://private.invalid"}`
		privateSourceID  = "repo:private-team/private-service"
		credentialHandle = "UNSAFE_CREDENTIAL_MARKER"
	)
	statusHandler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 6, 10, 4, 0, 0, 0, time.UTC),
			},
		},
		Profile: ProfileProduction,
		Governance: GovernanceStatusConfig{
			Mode:               "hosted_multi_tenant",
			State:              "enforcing",
			SourceKind:         "kubernetes_secret",
			PolicyRevisionHash: "sha256:abcdef1234567890",
			AuthMode:           "scoped_token",
			TenantMode:         "multi_tenant",
			WorkspaceMode:      "multi_workspace",
			EgressMode:         "restricted",
			RedactionState:     "configured",
			AuditState:         "observed",
			ExtensionMode:      "strict",
			Reasons:            []string{rawPolicyBody, privateSourceID, credentialHandle},
		},
	}
	mux := http.NewServeMux()
	statusHandler.Mount(mux)
	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:        AuthModeScoped,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/governance", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{rawPolicyBody, privateSourceID, credentialHandle, "private-team", "private.invalid"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("governance status leaked %q: %s", forbidden, body)
		}
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != hostedGovernanceStatusCapability {
		t.Fatalf("truth = %#v, want governance status truth", envelope.Truth)
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsSemanticExtractionStatusRoute(t *testing.T) {
	t.Parallel()

	const (
		rawProviderDetail = "provider response prompt for https://private.invalid with UNSAFE_SEMANTIC_CREDENTIAL_MARKER"
	)
	statusHandler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 6, 10, 4, 30, 0, 0, time.UTC),
				SemanticExtraction: statuspkg.SemanticExtractionStatus{
					State:              statuspkg.SemanticExtractionAvailable,
					ProviderConfigured: true,
					Detail:             rawProviderDetail,
					ProviderProfiles: []statuspkg.SemanticProviderProfileStatus{
						{
							ProfileID:              "semantic-docs-default",
							DisplayName:            "Documentation default",
							ProviderKind:           "deepseek",
							CredentialSourceKind:   "environment_variable",
							CredentialConfigured:   true,
							ModelID:                "deepseek-chat",
							EndpointProfileID:      "hosted-semantic-default",
							SourceClasses:          []string{"documentation"},
							SourcePolicyConfigured: true,
							State:                  statuspkg.SemanticProviderProfileConfigured,
							Detail:                 rawProviderDetail,
						},
					},
					Queue: statuspkg.SemanticExtractionQueueSnapshot{
						Total:             1,
						SourceClassCounts: []statuspkg.NamedCount{{Name: "documentation", Count: 1}},
					},
				},
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	statusHandler.Mount(mux)
	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:        AuthModeScoped,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/semantic-extraction", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{
		rawProviderDetail,
		"private.invalid",
		"UNSAFE_SEMANTIC_CREDENTIAL_MARKER",
		"prompt",
		"response",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("semantic extraction status leaked %q: %s", forbidden, body)
		}
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != semanticExtractionStatusCapability {
		t.Fatalf("truth = %#v, want semantic extraction status truth", envelope.Truth)
	}
}
