// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticpolicy_test

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestEvaluateRequiresExplicitPolicyAllowlist(t *testing.T) {
	t.Parallel()

	decision := semanticpolicy.Evaluate(semanticpolicy.Policy{}, semanticpolicy.Request{
		ProviderProfileID: "semantic-docs-default",
		SourceClass:       semanticprofile.SourceDocumentation,
		Scope: semanticpolicy.Scope{
			Kind: semanticpolicy.ScopeRepository,
			ID:   "repo-1",
		},
		SourcePath: "docs/runbook.md",
		ACLState:   semanticpolicy.ACLAllowed,
	}, providerStatuses())

	if decision.Allowed {
		t.Fatal("Evaluate() Allowed = true, want false without policy")
	}
	if got, want := decision.State, status.SemanticExtractionDisabledByPolicy; got != want {
		t.Fatalf("Evaluate() State = %q, want %q", got, want)
	}
	if got, want := decision.Reason, semanticpolicy.ReasonPolicyDisabled; got != want {
		t.Fatalf("Evaluate() Reason = %q, want %q", got, want)
	}
}

func TestEvaluateAllowsDocumentationWithConfiguredPolicy(t *testing.T) {
	t.Parallel()

	policy := docsOnlyPolicyWithEgress()
	decision := semanticpolicy.Evaluate(policy, semanticpolicy.Request{
		ProviderProfileID: "semantic-docs-default",
		SourceClass:       semanticprofile.SourceDocumentation,
		Scope: semanticpolicy.Scope{
			Kind: semanticpolicy.ScopeRepository,
			ID:   "repo-1",
		},
		SourcePath: "docs/runbook.md",
		ACLState:   semanticpolicy.ACLAllowed,
	}, providerStatuses())

	if !decision.Allowed {
		t.Fatalf("Evaluate() Allowed = false, want true: %#v", decision)
	}
	if got, want := decision.State, status.SemanticExtractionAvailable; got != want {
		t.Fatalf("Evaluate() State = %q, want %q", got, want)
	}
	if got, want := decision.PolicyID, "semantic-hosted-policy"; got != want {
		t.Fatalf("Evaluate() PolicyID = %q, want %q", got, want)
	}
	if got, want := decision.RuleID, "docs-repo-1"; got != want {
		t.Fatalf("Evaluate() RuleID = %q, want %q", got, want)
	}
	if got, want := decision.Settings.Limits.MaxChunkBytes, int64(8192); got != want {
		t.Fatalf("MaxChunkBytes = %d, want %d", got, want)
	}
	if got, want := decision.Settings.Limits.MaxTokensPerChunk, int64(2048); got != want {
		t.Fatalf("MaxTokensPerChunk = %d, want %d", got, want)
	}
	if got, want := decision.Settings.Redaction.Mode, semanticpolicy.RedactionStrict; got != want {
		t.Fatalf("Redaction.Mode = %q, want %q", got, want)
	}
	if got, want := decision.Settings.Retention.Posture, semanticpolicy.RetentionMetadataOnly; got != want {
		t.Fatalf("Retention.Posture = %q, want %q", got, want)
	}
}

func TestEvaluateAllowsSearchDocumentsByDocumentID(t *testing.T) {
	t.Parallel()

	policy := searchDocumentsPolicyWithEgress()
	decision := semanticpolicy.Evaluate(policy, semanticpolicy.Request{
		ProviderProfileID: "semantic-search-default",
		SourceClass:       semanticprofile.SourceSearchDocuments,
		Scope: semanticpolicy.Scope{
			Kind: semanticpolicy.ScopeRepository,
			ID:   "repo-1",
		},
		DocumentID: "searchdoc:code:handler",
		ACLState:   semanticpolicy.ACLAllowed,
	}, searchProviderStatuses())

	if !decision.Allowed {
		t.Fatalf("Evaluate() Allowed = false, want true: %#v", decision)
	}
	if got, want := decision.RuleID, "search-docs-repo-1"; got != want {
		t.Fatalf("RuleID = %q, want %q", got, want)
	}
	if got, want := decision.Settings.Retention.Posture, semanticpolicy.RetentionHashOnly; got != want {
		t.Fatalf("Retention.Posture = %q, want %q", got, want)
	}
}

func TestEvaluateDeniesCodeHintsWhenOnlyDocumentationIsAllowlisted(t *testing.T) {
	t.Parallel()

	decision := semanticpolicy.Evaluate(docsOnlyPolicyWithEgress(), semanticpolicy.Request{
		ProviderProfileID: "semantic-docs-default",
		SourceClass:       semanticprofile.SourceCodeHints,
		Scope: semanticpolicy.Scope{
			Kind: semanticpolicy.ScopeRepository,
			ID:   "repo-1",
		},
		SourcePath: "go/internal/query/status.go",
		ACLState:   semanticpolicy.ACLAllowed,
	}, providerStatuses())

	if decision.Allowed {
		t.Fatal("Evaluate() Allowed = true, want false for unallowlisted code hints")
	}
	if got, want := decision.State, status.SemanticExtractionAvailableButDisabledForScope; got != want {
		t.Fatalf("Evaluate() State = %q, want %q", got, want)
	}
	if got, want := decision.Reason, semanticpolicy.ReasonScopeDisabled; got != want {
		t.Fatalf("Evaluate() Reason = %q, want %q", got, want)
	}

	applied := semanticpolicy.ApplyToProviderStatuses(providerStatuses(), docsOnlyPolicyWithEgress())
	report := status.BuildReport(status.RawSnapshot{
		SemanticExtraction: status.SemanticExtractionStatus{
			ProviderProfiles: applied,
		},
	}, status.DefaultOptions())
	if got, want := report.SemanticExtraction.State, status.SemanticExtractionAvailable; got != want {
		t.Fatalf("SemanticExtraction.State = %q, want %q", got, want)
	}
	if !report.SemanticExtraction.DocumentationObservationsEnabled {
		t.Fatal("DocumentationObservationsEnabled = false, want true")
	}
	if report.SemanticExtraction.CodeHintsEnabled {
		t.Fatal("CodeHintsEnabled = true, want false")
	}
	if len(applied) != 1 {
		t.Fatalf("len(applied) = %d, want 1", len(applied))
	}
	if !slices.Equal(applied[0].SourceClasses, []string{semanticprofile.SourceDocumentation}) {
		t.Fatalf("SourceClasses = %#v, want documentation only", applied[0].SourceClasses)
	}
	if !applied[0].SourcePolicyConfigured {
		t.Fatal("SourcePolicyConfigured = false, want true for docs allowlist")
	}
}

func TestEvaluateReportsDisabledByPolicyWhenAllExtractionDisabled(t *testing.T) {
	t.Parallel()

	policy := docsOnlyPolicy()
	policy.Enabled = false

	decision := semanticpolicy.Evaluate(policy, semanticpolicy.Request{
		ProviderProfileID: "semantic-docs-default",
		SourceClass:       semanticprofile.SourceDocumentation,
		Scope: semanticpolicy.Scope{
			Kind: semanticpolicy.ScopeRepository,
			ID:   "repo-1",
		},
		SourcePath: "docs/runbook.md",
		ACLState:   semanticpolicy.ACLAllowed,
	}, providerStatuses())

	if decision.Allowed {
		t.Fatal("Evaluate() Allowed = true, want false when policy is disabled")
	}
	if got, want := decision.State, status.SemanticExtractionDisabledByPolicy; got != want {
		t.Fatalf("Evaluate() State = %q, want %q", got, want)
	}

	applied := semanticpolicy.ApplyToProviderStatuses(providerStatuses(), policy)
	report := status.BuildReport(status.RawSnapshot{
		SemanticExtraction: status.SemanticExtractionStatus{
			ProviderProfiles: applied,
		},
	}, status.DefaultOptions())
	if got, want := report.SemanticExtraction.State, status.SemanticExtractionDisabledByPolicy; got != want {
		t.Fatalf("SemanticExtraction.State = %q, want %q", got, want)
	}
}

func TestEvaluateFailsClosedForUnsafeInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request semanticpolicy.Request
		want    string
	}{
		{
			name: "unknown source class",
			request: semanticpolicy.Request{
				ProviderProfileID: "semantic-docs-default",
				SourceClass:       "raw_logs",
				Scope:             semanticpolicy.Scope{Kind: semanticpolicy.ScopeRepository, ID: "repo-1"},
				SourcePath:        "logs/app.log",
				ACLState:          semanticpolicy.ACLAllowed,
			},
			want: semanticpolicy.ReasonUnsupportedSourceClass,
		},
		{
			name: "missing provider profile",
			request: semanticpolicy.Request{
				ProviderProfileID: "missing-profile",
				SourceClass:       semanticprofile.SourceDocumentation,
				Scope:             semanticpolicy.Scope{Kind: semanticpolicy.ScopeRepository, ID: "repo-1"},
				SourcePath:        "docs/runbook.md",
				ACLState:          semanticpolicy.ACLAllowed,
			},
			want: semanticpolicy.ReasonProviderProfileNotAllowed,
		},
		{
			name: "stale ACL",
			request: semanticpolicy.Request{
				ProviderProfileID: "semantic-docs-default",
				SourceClass:       semanticprofile.SourceDocumentation,
				Scope:             semanticpolicy.Scope{Kind: semanticpolicy.ScopeRepository, ID: "repo-1"},
				SourcePath:        "docs/runbook.md",
				ACLState:          semanticpolicy.ACLStale,
			},
			want: semanticpolicy.ReasonACLNotAllowed,
		},
		{
			name: "explicitly denied source class",
			request: semanticpolicy.Request{
				ProviderProfileID: "semantic-docs-default",
				SourceClass:       semanticprofile.SourceTicketsChat,
				Scope:             semanticpolicy.Scope{Kind: semanticpolicy.ScopeRepository, ID: "repo-1"},
				SourcePath:        "incidents/p1.md",
				ACLState:          semanticpolicy.ACLAllowed,
			},
			want: semanticpolicy.ReasonSourceClassDenied,
		},
	}

	policy := docsOnlyPolicy()
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decision := semanticpolicy.Evaluate(policy, tt.request, providerStatuses())
			if decision.Allowed {
				t.Fatalf("Evaluate() Allowed = true, want false: %#v", decision)
			}
			if got, want := decision.Reason, tt.want; got != want {
				t.Fatalf("Evaluate() Reason = %q, want %q", got, want)
			}
		})
	}
}

func TestLoadFromEnvParsesPolicySettingsBeforeDatastoreConnections(t *testing.T) {
	t.Parallel()

	raw := `{
		"policy_id": "semantic-hosted-policy",
		"enabled": true,
		"egress": {
			"mode": "restricted",
			"semantic_providers": [
				{
					"provider_profile_id": "semantic-docs-default",
					"source_classes": ["documentation"],
					"decision": "allow"
				}
			]
		},
		"rules": [
			{
				"rule_id": "docs-repo-1",
				"provider_profile_id": "semantic-docs-default",
				"source_classes": ["documentation"],
				"scopes": [{"kind": "repository", "id": "repo-1"}],
				"source_allowlist": [{"kind": "path_prefix", "value": "docs/"}],
				"settings": {
					"limits": {
						"max_chunk_bytes": 8192,
						"max_tokens_per_chunk": 2048,
						"max_daily_tokens": 100000,
						"max_daily_cost_micros": 2500000
					},
					"redaction": {"mode": "strict", "policy_ref": "semantic-redaction-v1"},
					"retention": {
						"posture": "metadata_only",
						"prompt": "none",
						"response": "hash_only"
					}
				}
			}
		],
		"denied_source_classes": ["tickets_chat"]
	}`

	policy, err := semanticpolicy.LoadFromEnv(func(key string) string {
		if key == semanticpolicy.EnvPolicyJSON {
			return raw
		}
		return ""
	})
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v, want nil", err)
	}
	decision := semanticpolicy.Evaluate(policy, semanticpolicy.Request{
		ProviderProfileID: "semantic-docs-default",
		SourceClass:       semanticprofile.SourceDocumentation,
		Scope:             semanticpolicy.Scope{Kind: semanticpolicy.ScopeRepository, ID: "repo-1"},
		SourcePath:        "docs/runbook.md",
		ACLState:          semanticpolicy.ACLAllowed,
	}, providerStatuses())
	if !decision.Allowed {
		t.Fatalf("Evaluate() Allowed = false, want true: %#v", decision)
	}
}

func TestParsePolicyJSONRejectsEnabledPolicyWithoutRules(t *testing.T) {
	t.Parallel()

	_, err := semanticpolicy.ParsePolicyJSON(`{
		"policy_id": "semantic-hosted-policy",
		"enabled": true,
		"rules": []
	}`)
	if err == nil {
		t.Fatal("ParsePolicyJSON() error = nil, want enabled policy without rules error")
	}
}

func TestApplyToProviderStatusesClearsProfilePolicyWithoutExplicitAllowlist(t *testing.T) {
	t.Parallel()

	profiles := providerStatuses()
	profiles[0].SourcePolicyConfigured = true
	applied := semanticpolicy.ApplyToProviderStatuses(profiles, semanticpolicy.Policy{})
	if len(applied) != 1 {
		t.Fatalf("len(applied) = %d, want 1", len(applied))
	}
	if applied[0].SourcePolicyConfigured {
		t.Fatal("SourcePolicyConfigured = true, want false without explicit policy")
	}
	if len(applied[0].SourceClasses) != 0 {
		t.Fatalf("SourceClasses = %#v, want empty without explicit policy", applied[0].SourceClasses)
	}
}

func providerStatuses() []status.SemanticProviderProfileStatus {
	return []status.SemanticProviderProfileStatus{
		{
			ProfileID:              "semantic-docs-default",
			ProviderKind:           semanticprofile.ProviderDeepSeek,
			CredentialSourceKind:   semanticprofile.CredentialSourceEnvironmentVariable,
			CredentialConfigured:   true,
			ModelID:                "deepseek-chat",
			SourceClasses:          []string{semanticprofile.SourceCodeHints, semanticprofile.SourceDocumentation},
			SourcePolicyConfigured: false,
			State:                  status.SemanticProviderProfileConfigured,
		},
	}
}

func searchProviderStatuses() []status.SemanticProviderProfileStatus {
	return []status.SemanticProviderProfileStatus{
		{
			ProfileID:              "semantic-search-default",
			ProviderKind:           semanticprofile.ProviderInternalGateway,
			CredentialSourceKind:   semanticprofile.CredentialSourceCloudWorkloadIdentity,
			CredentialConfigured:   true,
			ModelID:                "search-embed-v1",
			EndpointProfileID:      "semantic-search-gateway",
			SourceClasses:          []string{semanticprofile.SourceSearchDocuments},
			SourcePolicyConfigured: false,
			State:                  status.SemanticProviderProfileConfigured,
		},
	}
}

func docsOnlyPolicy() semanticpolicy.Policy {
	return semanticpolicy.Policy{
		PolicyID:            "semantic-hosted-policy",
		Enabled:             true,
		DeniedSourceClasses: []string{semanticprofile.SourceTicketsChat},
		Rules: []semanticpolicy.Rule{
			{
				RuleID:            "docs-repo-1",
				ProviderProfileID: "semantic-docs-default",
				SourceClasses:     []string{semanticprofile.SourceDocumentation},
				Scopes: []semanticpolicy.Scope{
					{Kind: semanticpolicy.ScopeRepository, ID: "repo-1"},
				},
				SourceAllowlist: []semanticpolicy.SourceSelector{
					{Kind: semanticpolicy.SourceSelectorPathPrefix, Value: "docs/"},
				},
				Settings: semanticpolicy.Settings{
					Limits: semanticpolicy.Limits{
						MaxChunkBytes:      8192,
						MaxTokensPerChunk:  2048,
						MaxDailyTokens:     100000,
						MaxDailyCostMicros: 2500000,
					},
					Redaction: semanticpolicy.Redaction{
						Mode:      semanticpolicy.RedactionStrict,
						PolicyRef: "semantic-redaction-v1",
					},
					Retention: semanticpolicy.Retention{
						Posture:  semanticpolicy.RetentionMetadataOnly,
						Prompt:   semanticpolicy.RetentionNone,
						Response: semanticpolicy.RetentionHashOnly,
					},
				},
			},
		},
	}
}

func searchDocumentsPolicyWithEgress() semanticpolicy.Policy {
	return semanticpolicy.Policy{
		PolicyID: "semantic-search-policy",
		Enabled:  true,
		Egress: semanticpolicy.EgressPolicy{
			Mode: semanticpolicy.EgressModeRestricted,
			SemanticProviders: []semanticpolicy.EgressProviderRule{
				{
					ProviderProfileID: "semantic-search-default",
					SourceClasses:     []string{semanticprofile.SourceSearchDocuments},
					Decision:          semanticpolicy.EgressDecisionAllow,
				},
			},
		},
		Rules: []semanticpolicy.Rule{
			{
				RuleID:            "search-docs-repo-1",
				ProviderProfileID: "semantic-search-default",
				SourceClasses:     []string{semanticprofile.SourceSearchDocuments},
				Scopes: []semanticpolicy.Scope{
					{Kind: semanticpolicy.ScopeRepository, ID: "repo-1"},
				},
				SourceAllowlist: []semanticpolicy.SourceSelector{
					{Kind: semanticpolicy.SourceSelectorDocumentID, Value: "searchdoc:code:handler"},
				},
				Settings: semanticpolicy.Settings{
					Limits: semanticpolicy.Limits{
						MaxChunkBytes:      4096,
						MaxTokensPerChunk:  1024,
						MaxDailyTokens:     50000,
						MaxDailyCostMicros: 1000000,
					},
					Redaction: semanticpolicy.Redaction{
						Mode:      semanticpolicy.RedactionStrict,
						PolicyRef: "semantic-search-redaction-v1",
					},
					Retention: semanticpolicy.Retention{
						Posture:  semanticpolicy.RetentionHashOnly,
						Prompt:   semanticpolicy.RetentionNone,
						Response: semanticpolicy.RetentionHashOnly,
					},
				},
			},
		},
	}
}
