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
