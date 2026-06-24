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

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestStatusHandlerGovernanceLocalNoPolicyReturnsEnvelope(t *testing.T) {
	t.Parallel()

	envelope, body := governanceStatusEnvelope(t, &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 6, 9, 16, 0, 0, 0, time.UTC),
			},
		},
		Profile: ProfileLocalLightweight,
	})

	assertNoGovernanceStatusLeak(t, body)
	data := envelope.Data.(map[string]any)
	if got, want := data["mode"], "local_no_policy"; got != want {
		t.Fatalf("data.mode = %#v, want %#v", got, want)
	}
	if got, want := data["state"], "disabled"; got != want {
		t.Fatalf("data.state = %#v, want %#v", got, want)
	}
	requireStringSliceContains(t, data["reasons"], "policy_not_configured")
	if got, want := data["source_kind"], "unknown"; got != want {
		t.Fatalf("data.source_kind = %#v, want %#v", got, want)
	}
	if _, ok := data["policy_revision"]; ok {
		t.Fatalf("data exposed raw policy_revision: %#v", data)
	}
	if got, want := envelope.Truth.Capability, hostedGovernanceStatusCapability; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Freshness.State, FreshnessUnavailable; got != want {
		t.Fatalf("truth.freshness.state = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Level, TruthLevelFallback; got != want {
		t.Fatalf("truth.level = %q, want %q", got, want)
	}
}

func TestStatusHandlerGovernanceEnforcingReportsSafeAggregates(t *testing.T) {
	t.Parallel()

	const (
		rawPolicyBody     = `{"tenant":"redacted-team","endpoint":"https://redacted.invalid"}`
		privateSourceID   = "repo:redacted-team/redacted-service"
		credentialHandle  = "UNSAFE_CREDENTIAL_MARKER"
		rawProviderDetail = "raw prompt and provider response must stay private"
	)
	envelope, body := governanceStatusEnvelope(t, &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 6, 9, 16, 5, 0, 0, time.UTC),
				SemanticExtraction: statuspkg.SemanticExtractionStatus{
					State:              statuspkg.SemanticExtractionAvailable,
					ProviderConfigured: true,
					ProviderProfiles: []statuspkg.SemanticProviderProfileStatus{
						{
							ProfileID:              "semantic-docs-default",
							ProviderKind:           "internal_gateway",
							CredentialSourceKind:   "cloud_workload_identity",
							CredentialConfigured:   true,
							SourceClasses:          []string{"documentation"},
							SourcePolicyConfigured: true,
							State:                  statuspkg.SemanticProviderProfileConfigured,
							Detail:                 credentialHandle,
						},
					},
					Queue: statuspkg.SemanticExtractionQueueSnapshot{
						Total:        4,
						PolicyDenied: 1,
					},
					Audit: statuspkg.SemanticExtractionAuditSnapshot{
						ActorClassCounts: []statuspkg.NamedCount{{Name: "hosted_worker", Count: 4}},
						ACLStateCounts:   []statuspkg.NamedCount{{Name: "acl_allowed", Count: 3}},
					},
					Detail: rawProviderDetail,
				},
			},
		},
		Profile: ProfileProduction,
		Governance: GovernanceStatusConfig{
			Mode:               "hosted_single_tenant",
			State:              "enforcing",
			SourceKind:         "kubernetes_secret",
			PolicyRevisionHash: "sha256:abcdef1234567890",
			AuthMode:           "shared_token",
			TokenConfigured:    true,
			TenantMode:         "single_tenant",
			WorkspaceMode:      "single_workspace",
			EgressMode:         "restricted",
			RetentionMode:      "metadata_only",
			RedactionState:     "configured",
			AuditState:         "observed",
			ExtensionMode:      "strict",
			DeniedDecisions:    2,
			PolicySectionCount: 8,
			Reasons:            []string{"shared_token_mode", rawPolicyBody, privateSourceID},
		},
	})

	for _, forbidden := range []string{
		rawPolicyBody,
		privateSourceID,
		credentialHandle,
		rawProviderDetail,
		"redacted-team",
		"redacted.invalid",
		"prompt",
		"provider response",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("governance status leaked %q: %s", forbidden, body)
		}
	}

	data := envelope.Data.(map[string]any)
	if got, want := data["mode"], "hosted_single_tenant"; got != want {
		t.Fatalf("data.mode = %#v, want %#v", got, want)
	}
	if got, want := data["state"], "enforcing"; got != want {
		t.Fatalf("data.state = %#v, want %#v", got, want)
	}
	if got, want := data["policy_revision_hash"], "sha256:abcdef1234567890"; got != want {
		t.Fatalf("data.policy_revision_hash = %#v, want %#v", got, want)
	}
	readiness := data["readiness"].(map[string]any)
	if got, want := readiness["identity"], true; got != want {
		t.Fatalf("readiness.identity = %#v, want %#v", got, want)
	}
	if got, want := readiness["egress"], true; got != want {
		t.Fatalf("readiness.egress = %#v, want %#v", got, want)
	}
	identity := data["identity"].(map[string]any)
	if got, want := identity["auth_mode"], "shared_token"; got != want {
		t.Fatalf("identity.auth_mode = %#v, want %#v", got, want)
	}
	if got, want := identity["shared_token_limitation"], true; got != want {
		t.Fatalf("identity.shared_token_limitation = %#v, want %#v", got, want)
	}
	egress := data["egress"].(map[string]any)
	if got, want := egress["mode"], "restricted"; got != want {
		t.Fatalf("egress.mode = %#v, want %#v", got, want)
	}
	aggregates := data["aggregates"].(map[string]any)
	if got, want := aggregates["denied_decision_count"], float64(2); got != want {
		t.Fatalf("aggregates.denied_decision_count = %#v, want %#v", got, want)
	}
	semantic := data["semantic"].(map[string]any)
	if got, want := semantic["state"], "available"; got != want {
		t.Fatalf("semantic.state = %#v, want %#v", got, want)
	}
}

func TestGovernanceStatusConfigNormalizesPolicyStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		config     GovernanceStatusConfig
		wantState  string
		wantReason string
	}{
		{
			name:       "missing policy",
			config:     GovernanceStatusConfig{Mode: "hosted_single_tenant", State: "disabled"},
			wantState:  "disabled",
			wantReason: "policy_not_configured",
		},
		{
			name:       "invalid policy",
			config:     GovernanceStatusConfig{Mode: "hosted_single_tenant", State: "invalid"},
			wantState:  "invalid",
			wantReason: "policy_invalid",
		},
		{
			name:       "stale policy",
			config:     GovernanceStatusConfig{Mode: "hosted_single_tenant", State: "stale"},
			wantState:  "stale",
			wantReason: "policy_stale",
		},
		{
			name:       "partial policy",
			config:     GovernanceStatusConfig{Mode: "hosted_single_tenant", State: "partial"},
			wantState:  "partial",
			wantReason: "policy_partial",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload := buildGovernanceStatus(tc.config, statuspkg.SemanticExtractionStatus{})
			if got := payload["state"]; got != tc.wantState {
				t.Fatalf("state = %#v, want %#v", got, tc.wantState)
			}
			requireStringSliceContains(t, payload["reasons"], tc.wantReason)
		})
	}
}

func TestGovernanceStatusConfigDropsUnsafeStatusValues(t *testing.T) {
	t.Parallel()

	payload := buildGovernanceStatus(GovernanceStatusConfig{
		Mode:               "/Users/operator/policy.json",
		State:              "https://redacted.invalid/state",
		SourceKind:         "tenant-redacted",
		PolicyRevisionHash: "sha256:/Users/operator/private",
		AuthMode:           "Bearer redacted",
		TenantMode:         "tenant-redacted",
		WorkspaceMode:      "workspace-redacted",
		EgressMode:         "https://redacted.invalid",
		RetentionMode:      "repo:redacted-team/redacted-service",
		RedactionState:     "credential-handle",
		AuditState:         "prompt text",
		ExtensionMode:      "provider response",
		Reasons: []string{
			"policy_invalid",
			"/Users/operator/policy.json",
			"https://redacted.invalid",
		},
	}, statuspkg.SemanticExtractionStatus{})

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	for _, forbidden := range []string{
		"/Users/operator",
		"redacted.invalid",
		"tenant-redacted",
		"workspace-redacted",
		"redacted-service",
		"credential-handle",
		"prompt text",
		"provider response",
		"Bearer redacted",
	} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("governance status leaked unsafe config value %q: %s", forbidden, string(body))
		}
	}
	if got, want := payload["mode"], "local_no_policy"; got != want {
		t.Fatalf("mode = %#v, want %#v", got, want)
	}
	if got, want := payload["state"], "disabled"; got != want {
		t.Fatalf("state = %#v, want %#v", got, want)
	}
	if got, want := payload["policy_revision_hash"], ""; got != want {
		t.Fatalf("policy_revision_hash = %#v, want %#v", got, want)
	}
	requireStringSliceContains(t, payload["reasons"], "policy_invalid")
}

func TestGovernanceStatusReportsAuditAggregates(t *testing.T) {
	t.Parallel()

	envelope, body := governanceStatusEnvelope(t, &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				SemanticExtraction: statuspkg.SemanticExtractionStatus{
					Audit: statuspkg.SemanticExtractionAuditSnapshot{
						ActorClassCounts: []statuspkg.NamedCount{
							{Name: string(governanceaudit.ActorClassScopedToken), Count: 3},
							{Name: "hosted_worker", Count: 1},
						},
					},
				},
			},
		},
		Profile: ProfileProduction,
		Governance: GovernanceStatusConfig{
			Mode:       "hosted_single_tenant",
			State:      "enforcing",
			AuthMode:   "scoped_token",
			TenantMode: "single_tenant",
			AuditState: "observed",
			AuditSummary: governanceaudit.Summary{
				Total:       4,
				Denied:      2,
				Unavailable: 1,
				EventTypeCounts: []governanceaudit.Count{
					{Name: string(governanceaudit.EventTypeReadAuthorization), Count: 2},
				},
				ActorClassCounts: []governanceaudit.Count{
					{Name: string(governanceaudit.ActorClassScopedToken), Count: 2},
				},
				ScopeClassCounts: []governanceaudit.Count{
					{Name: string(governanceaudit.ScopeClassRepository), Count: 2},
				},
				ReasonCounts: []governanceaudit.Count{
					{Name: "subject_scope_missing", Count: 2},
				},
			},
		},
	})

	for _, forbidden := range []string{
		"operator.person@example.invalid",
		"repo://private/source",
		"Bearer unsafe-token",
		"credential_handle",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("governance audit status leaked %q: %s", forbidden, body)
		}
	}

	data := envelope.Data.(map[string]any)
	audit := data["audit"].(map[string]any)
	if got, want := audit["event_count"], float64(4); got != want {
		t.Fatalf("audit.event_count = %#v, want %#v", got, want)
	}
	if got, want := audit["denied_decision_count"], float64(2); got != want {
		t.Fatalf("audit.denied_decision_count = %#v, want %#v", got, want)
	}
	if got, want := audit["unavailable_decision_count"], float64(1); got != want {
		t.Fatalf("audit.unavailable_decision_count = %#v, want %#v", got, want)
	}
	if got, want := audit["event_type_count"], float64(1); got != want {
		t.Fatalf("audit.event_type_count = %#v, want %#v", got, want)
	}
	if got, want := audit["actor_class_count"], float64(2); got != want {
		t.Fatalf("audit.actor_class_count = %#v, want %#v", got, want)
	}
	if got, want := audit["reason_count"], float64(1); got != want {
		t.Fatalf("audit.reason_count = %#v, want %#v", got, want)
	}
	aggregates := data["aggregates"].(map[string]any)
	if got, want := aggregates["denied_decision_count"], float64(2); got != want {
		t.Fatalf("aggregates.denied_decision_count = %#v, want %#v", got, want)
	}
}

func TestGovernanceStatusConfigFromEnvParsesAuditCounts(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"ESHU_GOVERNANCE_AUDIT_EVENT_COUNT":                "7",
		"ESHU_GOVERNANCE_AUDIT_DENIED_DECISION_COUNT":      "3",
		"ESHU_GOVERNANCE_AUDIT_UNAVAILABLE_DECISION_COUNT": "2",
	}
	config := GovernanceStatusConfigFromEnv(func(key string) string {
		return values[key]
	}, true)

	if got, want := config.AuditSummary.Total, 7; got != want {
		t.Fatalf("AuditSummary.Total = %d, want %d", got, want)
	}
	if got, want := config.AuditSummary.Denied, 3; got != want {
		t.Fatalf("AuditSummary.Denied = %d, want %d", got, want)
	}
	if got, want := config.AuditSummary.Unavailable, 2; got != want {
		t.Fatalf("AuditSummary.Unavailable = %d, want %d", got, want)
	}
}

func governanceStatusEnvelope(t *testing.T, handler *StatusHandler) (ResponseEnvelope, string) {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/governance", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/status/governance status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Truth == nil {
		t.Fatal("truth = nil, want governance status envelope")
	}
	return envelope, rec.Body.String()
}

func assertNoGovernanceStatusLeak(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{
		"raw_policy",
		"policy_body",
		"tenant_id",
		"workspace_id",
		"repository_id",
		"credential_handle",
		"provider_endpoint",
		"token_value",
		"/Users/",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("governance status leaked forbidden marker %q: %s", forbidden, body)
		}
	}
}

func requireStringSliceContains(t *testing.T, value any, want string) {
	t.Helper()
	rows, ok := value.([]any)
	if !ok {
		t.Fatalf("value = %#v, want []any containing %q", value, want)
	}
	for _, row := range rows {
		if row == want {
			return
		}
	}
	t.Fatalf("value = %#v, want %q", rows, want)
}
