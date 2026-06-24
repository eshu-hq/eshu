// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestSemanticExtractionDefaultsUnavailableWithoutAffectingHealth(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		AsOf: time.Date(2026, time.June, 8, 14, 0, 0, 0, time.UTC),
	}, status.DefaultOptions())

	if got, want := report.Health.State, "healthy"; got != want {
		t.Fatalf("Health.State = %q, want %q", got, want)
	}
	semantic := report.SemanticExtraction
	if got, want := semantic.State, status.SemanticExtractionUnavailable; got != want {
		t.Fatalf("SemanticExtraction.State = %q, want %q", got, want)
	}
	if got, want := semantic.Reason, status.SemanticExtractionReasonProviderNotConfigured; got != want {
		t.Fatalf("SemanticExtraction.Reason = %q, want %q", got, want)
	}
	if semantic.ProviderConfigured {
		t.Fatal("ProviderConfigured = true, want false for no-provider mode")
	}
	if semantic.DocumentationObservationsEnabled {
		t.Fatal("DocumentationObservationsEnabled = true, want false for no-provider mode")
	}
	if semantic.CodeHintsEnabled {
		t.Fatal("CodeHintsEnabled = true, want false for no-provider mode")
	}
	if semantic.DeterministicPathsAffected {
		t.Fatal("DeterministicPathsAffected = true, want false so no-provider mode cannot block indexing")
	}
}

func TestSemanticExtractionProviderConfiguredMatchesState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		snapshot               status.SemanticExtractionStatus
		wantProviderConfigured bool
	}{
		{
			name: "unavailable clears provider flag",
			snapshot: status.SemanticExtractionStatus{
				State:              status.SemanticExtractionUnavailable,
				ProviderConfigured: true,
			},
			wantProviderConfigured: false,
		},
		{
			name: "provider unhealthy implies configured provider",
			snapshot: status.SemanticExtractionStatus{
				State: status.SemanticExtractionProviderUnhealthy,
			},
			wantProviderConfigured: true,
		},
		{
			name: "policy disabled without provider stays unconfigured",
			snapshot: status.SemanticExtractionStatus{
				State: status.SemanticExtractionDisabledByPolicy,
			},
			wantProviderConfigured: false,
		},
		{
			name: "policy disabled with provider preserves configured flag",
			snapshot: status.SemanticExtractionStatus{
				State:              status.SemanticExtractionDisabledByPolicy,
				ProviderConfigured: true,
			},
			wantProviderConfigured: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			report := status.BuildReport(status.RawSnapshot{
				SemanticExtraction: tt.snapshot,
			}, status.DefaultOptions())
			if got := report.SemanticExtraction.ProviderConfigured; got != tt.wantProviderConfigured {
				t.Fatalf("ProviderConfigured = %t, want %t", got, tt.wantProviderConfigured)
			}
		})
	}
}

func TestSemanticExtractionProviderProfilesEnableScopedCapabilities(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		SemanticExtraction: status.SemanticExtractionStatus{
			ProviderProfiles: []status.SemanticProviderProfileStatus{
				{
					ProfileID:              "semantic-code-hints",
					DisplayName:            "Code hints",
					ProviderKind:           "deepseek",
					CredentialSourceKind:   "environment_variable",
					CredentialConfigured:   true,
					ModelID:                "deepseek-chat",
					EmbeddingDimensions:    3,
					EndpointProfileID:      "deepseek-public-api",
					SourceClasses:          []string{"code_hints"},
					SourcePolicyConfigured: true,
					State:                  status.SemanticProviderProfileConfigured,
				},
				{
					ProfileID:              "semantic-docs-default",
					DisplayName:            "Documentation default",
					ProviderKind:           "deepseek",
					CredentialSourceKind:   "environment_variable",
					CredentialConfigured:   true,
					ModelID:                "deepseek-chat",
					EmbeddingDimensions:    3,
					EndpointProfileID:      "deepseek-public-api",
					SourceClasses:          []string{"documentation"},
					SourcePolicyConfigured: true,
					State:                  status.SemanticProviderProfileConfigured,
				},
			},
		},
	}, status.DefaultOptions())

	semantic := report.SemanticExtraction
	if got, want := semantic.State, status.SemanticExtractionAvailable; got != want {
		t.Fatalf("SemanticExtraction.State = %q, want %q", got, want)
	}
	if !semantic.ProviderConfigured {
		t.Fatal("ProviderConfigured = false, want true")
	}
	if !semantic.DocumentationObservationsEnabled {
		t.Fatal("DocumentationObservationsEnabled = false, want true")
	}
	if !semantic.CodeHintsEnabled {
		t.Fatal("CodeHintsEnabled = false, want true")
	}
	if len(semantic.ProviderProfiles) != 2 {
		t.Fatalf("len(ProviderProfiles) = %d, want 2", len(semantic.ProviderProfiles))
	}
	if got, want := semantic.ProviderProfiles[0].ProfileID, "semantic-code-hints"; got != want {
		t.Fatalf("ProviderProfiles[0].ProfileID = %q, want sorted %q", got, want)
	}
	if got, want := semantic.ProviderProfiles[1].ProfileID, "semantic-docs-default"; got != want {
		t.Fatalf("ProviderProfiles[1].ProfileID = %q, want sorted %q", got, want)
	}
}

func TestSemanticExtractionProviderProfilesRemainPolicyGated(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		SemanticExtraction: status.SemanticExtractionStatus{
			ProviderProfiles: []status.SemanticProviderProfileStatus{
				{
					ProfileID:              "semantic-docs-default",
					ProviderKind:           "deepseek",
					CredentialSourceKind:   "environment_variable",
					CredentialConfigured:   true,
					ModelID:                "deepseek-chat",
					SourceClasses:          []string{"documentation"},
					SourcePolicyConfigured: false,
					State:                  status.SemanticProviderProfileConfigured,
				},
			},
		},
	}, status.DefaultOptions())

	semantic := report.SemanticExtraction
	if got, want := semantic.State, status.SemanticExtractionDisabledByPolicy; got != want {
		t.Fatalf("SemanticExtraction.State = %q, want %q", got, want)
	}
	if !semantic.ProviderConfigured {
		t.Fatal("ProviderConfigured = false, want true because profile metadata is configured")
	}
	if semantic.DocumentationObservationsEnabled {
		t.Fatal("DocumentationObservationsEnabled = true, want false while source policy is absent")
	}
	if semantic.CodeHintsEnabled {
		t.Fatal("CodeHintsEnabled = true, want false while source policy is absent")
	}
	if got, want := semantic.Reason, status.SemanticExtractionReasonPolicyDisabled; got != want {
		t.Fatalf("SemanticExtraction.Reason = %q, want %q", got, want)
	}
}

func TestSemanticExtractionUnhealthyProviderProfileRemainsConfigured(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		SemanticExtraction: status.SemanticExtractionStatus{
			ProviderProfiles: []status.SemanticProviderProfileStatus{
				{
					ProfileID:              "semantic-docs-default",
					ProviderKind:           "deepseek",
					CredentialSourceKind:   "environment_variable",
					CredentialConfigured:   true,
					ModelID:                "deepseek-chat",
					SourceClasses:          []string{"documentation"},
					SourcePolicyConfigured: true,
					State:                  status.SemanticProviderProfileUnhealthy,
				},
			},
		},
	}, status.DefaultOptions())

	semantic := report.SemanticExtraction
	if got, want := semantic.State, status.SemanticExtractionProviderUnhealthy; got != want {
		t.Fatalf("SemanticExtraction.State = %q, want %q", got, want)
	}
	if !semantic.ProviderConfigured {
		t.Fatal("ProviderConfigured = false, want true for unhealthy configured profile")
	}
	if semantic.DocumentationObservationsEnabled {
		t.Fatal("DocumentationObservationsEnabled = true, want false while provider is unhealthy")
	}
	if semantic.CodeHintsEnabled {
		t.Fatal("CodeHintsEnabled = true, want false while provider is unhealthy")
	}
}

func TestRenderStatusIncludesRedactedSemanticProviderProfiles(t *testing.T) {
	t.Parallel()

	const credentialHandle = "DEEPSEEK_API_KEY"
	report := status.BuildReport(status.RawSnapshot{
		SemanticExtraction: status.SemanticExtractionStatus{
			ProviderProfiles: []status.SemanticProviderProfileStatus{
				{
					ProfileID:              "semantic-docs-default",
					DisplayName:            "Documentation default",
					ProviderKind:           "deepseek",
					CredentialSourceKind:   "environment_variable",
					CredentialConfigured:   true,
					ModelID:                "deepseek-chat",
					EmbeddingDimensions:    3,
					EndpointProfileID:      "deepseek-public-api",
					SourceClasses:          []string{"documentation"},
					SourcePolicyConfigured: true,
					State:                  status.SemanticProviderProfileConfigured,
				},
			},
		},
	}, status.DefaultOptions())

	text := status.RenderText(report)
	for _, want := range []string{
		"provider_profiles=1",
		"profile=semantic-docs-default",
		"provider=deepseek",
		"credential_source=environment_variable",
		"credential_configured=true",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("RenderText() missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, credentialHandle) {
		t.Fatalf("RenderText() leaked credential handle %q:\n%s", credentialHandle, text)
	}

	encoded, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	if strings.Contains(string(encoded), credentialHandle) {
		t.Fatalf("RenderJSON() leaked credential handle %q:\n%s", credentialHandle, encoded)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	semantic, ok := payload["semantic_extraction"].(map[string]any)
	if !ok {
		t.Fatalf("semantic_extraction missing or wrong type: %s", encoded)
	}
	profiles, ok := semantic["provider_profiles"].([]any)
	if !ok {
		t.Fatalf("provider_profiles missing or wrong type: %#v", semantic["provider_profiles"])
	}
	if len(profiles) != 1 {
		t.Fatalf("len(provider_profiles) = %d, want 1", len(profiles))
	}
	profile := profiles[0].(map[string]any)
	if _, ok := profile["credential_handle"]; ok {
		t.Fatalf("provider profile exposed credential_handle: %#v", profile)
	}
	if got, want := profile["credential_source_kind"], "environment_variable"; got != want {
		t.Fatalf("credential_source_kind = %#v, want %#v", got, want)
	}
	if got, want := profile["embedding_dimensions"], float64(3); got != want {
		t.Fatalf("embedding_dimensions = %#v, want %#v", got, want)
	}
}

func TestRenderStatusIncludesSemanticExtractionNoProvider(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		AsOf: time.Date(2026, time.June, 8, 14, 0, 0, 0, time.UTC),
	}, status.DefaultOptions())

	text := status.RenderText(report)
	for _, want := range []string{
		"Semantic extraction:",
		"state=unavailable",
		"reason=provider_not_configured",
		"code_hints=disabled",
		"documentation_observations=disabled",
		"deterministic_paths=unaffected",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("RenderText() missing %q:\n%s", want, text)
		}
	}

	encoded, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	semantic, ok := payload["semantic_extraction"].(map[string]any)
	if !ok {
		t.Fatalf("semantic_extraction missing or wrong type: %s", encoded)
	}
	if got, want := semantic["state"], "unavailable"; got != want {
		t.Fatalf("semantic_extraction.state = %#v, want %#v", got, want)
	}
	if got, want := semantic["code_hints_enabled"], false; got != want {
		t.Fatalf("semantic_extraction.code_hints_enabled = %#v, want %#v", got, want)
	}
	if got, want := semantic["deterministic_paths_affected"], false; got != want {
		t.Fatalf("semantic_extraction.deterministic_paths_affected = %#v, want %#v", got, want)
	}
}

func TestSemanticExtractionQueueBudgetAndAuditDoNotAffectDeterministicHealth(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		AsOf: time.Date(2026, time.June, 9, 6, 0, 0, 0, time.UTC),
		SemanticExtraction: status.SemanticExtractionStatus{
			State:                      status.SemanticExtractionAvailable,
			ProviderConfigured:         true,
			DeterministicPathsAffected: true,
			Queue: status.SemanticExtractionQueueSnapshot{
				Total:           5,
				Pending:         2,
				Retrying:        1,
				DeadLetter:      1,
				BudgetExhausted: 1,
				SourceClassCounts: []status.NamedCount{
					{Name: "documentation", Count: 4},
				},
				StatusCounts: []status.NamedCount{
					{Name: "pending", Count: 2},
					{Name: "dead_letter", Count: 1},
				},
			},
			Budget: status.SemanticExtractionBudgetSnapshot{
				EstimatedInputTokens: 500,
				ActualCostMicros:     250,
				Exhausted:            1,
			},
			Audit: status.SemanticExtractionAuditSnapshot{
				ActorClassCounts: []status.NamedCount{{Name: "hosted_worker", Count: 5}},
				ACLStateCounts:   []status.NamedCount{{Name: "acl_allowed", Count: 5}},
			},
		},
	}, status.DefaultOptions())

	if got, want := report.Health.State, "healthy"; got != want {
		t.Fatalf("Health.State = %q, want %q", got, want)
	}
	semantic := report.SemanticExtraction
	if semantic.DeterministicPathsAffected {
		t.Fatal("DeterministicPathsAffected = true, want semantic queue/budget to stay advisory")
	}
	if got, want := semantic.Queue.BudgetExhausted, 1; got != want {
		t.Fatalf("Queue.BudgetExhausted = %d, want %d", got, want)
	}
	if got, want := semantic.Budget.EstimatedInputTokens, int64(500); got != want {
		t.Fatalf("Budget.EstimatedInputTokens = %d, want %d", got, want)
	}
}

func TestRenderStatusIncludesSemanticExtractionObservability(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		SemanticExtraction: status.SemanticExtractionStatus{
			State:              status.SemanticExtractionAvailable,
			ProviderConfigured: true,
			Queue: status.SemanticExtractionQueueSnapshot{
				Total:           3,
				Pending:         1,
				Succeeded:       1,
				BudgetExhausted: 1,
				SourceClassCounts: []status.NamedCount{
					{Name: "documentation", Count: 2},
				},
			},
			Budget: status.SemanticExtractionBudgetSnapshot{
				EstimatedInputTokens: 220,
				EstimatedCostMicros:  330,
				ActualInputTokens:    90,
				ActualCostMicros:     120,
				Exhausted:            1,
			},
			Audit: status.SemanticExtractionAuditSnapshot{
				ActorClassCounts: []status.NamedCount{{Name: "hosted_worker", Count: 3}},
			},
		},
	}, status.DefaultOptions())

	text := status.RenderText(report)
	for _, want := range []string{
		"semantic_queue_total=3",
		"semantic_budget_exhausted=1",
		"semantic_estimated_input_tokens=220",
		"semantic_actual_cost_micros=120",
		"semantic_audit_actor_classes=hosted_worker=3",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("RenderText() missing %q:\n%s", want, text)
		}
	}

	encoded, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	semantic := payload["semantic_extraction"].(map[string]any)
	queue := semantic["queue"].(map[string]any)
	if got, want := queue["budget_exhausted"], float64(1); got != want {
		t.Fatalf("queue.budget_exhausted = %#v, want %#v", got, want)
	}
	budget := semantic["budget"].(map[string]any)
	if got, want := budget["estimated_input_tokens"], float64(220); got != want {
		t.Fatalf("budget.estimated_input_tokens = %#v, want %#v", got, want)
	}
	audit := semantic["audit"].(map[string]any)
	if _, ok := audit["actor_class_counts"]; !ok {
		t.Fatalf("audit.actor_class_counts missing: %#v", audit)
	}
}
