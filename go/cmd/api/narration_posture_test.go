package main

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// TestBuildNarrationPostureDefaultClosed verifies that with no env vars set
// (all defaults) the posture source returns Unavailable — never Available.
// This is the mandatory default-closed invariant.
func TestBuildNarrationPostureDefaultClosed(t *testing.T) {
	t.Parallel()

	postureFunc := buildNarrationPosture(func(string) string { return "" }, nil)
	got := postureFunc()

	if got.State == status.AnswerNarrationAvailable {
		t.Fatalf("default-closed violated: State = %q, must NOT be Available when no config is set", got.State)
	}
	if !got.DeterministicFallbackAvailable {
		t.Fatal("DeterministicFallbackAvailable must always be true")
	}
}

// TestBuildNarrationPostureAllGatesOpen verifies that when every gate env var
// is set to its enabling value (and a valid agent_reasoning profile is present)
// the posture source returns Available.
func TestBuildNarrationPostureAllGatesOpen(t *testing.T) {
	t.Parallel()

	// Minimal valid agent_reasoning profile JSON.
	profileJSON := `[{"profile_id":"ask-agent","provider_kind":"anthropic","credential_source":{"kind":"environment_variable","handle":"ANTHROPIC_API_KEY"},"model_id":"claude-3-5-haiku-20241022","source_classes":["agent_reasoning"],"source_policy_configured":true}]`
	env := map[string]string{
		"ESHU_SEMANTIC_PROVIDER_PROFILES_JSON": profileJSON,
		"ESHU_ASK_ENABLED":                     "true",
		"ESHU_ASK_NARRATION_ENABLED":           "true",
	}
	getenv := func(key string) string { return env[key] }

	postureFunc := buildNarrationPosture(getenv, nil)
	got := postureFunc()

	if got.State != status.AnswerNarrationAvailable {
		t.Fatalf("all-gates-open: State = %q (Reason=%q), want Available; check that every gate is properly derived",
			got.State, got.Reason)
	}
	if !got.ProviderConfigured {
		t.Fatal("all-gates-open: ProviderConfigured should be true when agent_reasoning profile present")
	}
	if !got.ProviderTrafficEnabled {
		t.Fatal("all-gates-open: ProviderTrafficEnabled should be true when ask + narration enabled")
	}
	if !got.PolicyAllowed {
		t.Fatal("all-gates-open: PolicyAllowed should be true (v1 conservative wiring: true when traffic enabled)")
	}
	if !got.BudgetAvailable {
		t.Fatal("all-gates-open: BudgetAvailable should be true (v1 conservative wiring: true when traffic enabled)")
	}
	if !got.PublishSafetyEnabled {
		t.Fatal("all-gates-open: PublishSafetyEnabled should be true (v1 conservative wiring: true when traffic enabled)")
	}
}

// TestBuildNarrationPostureAskEnabledButNarrationNotEnabled verifies that
// ESHU_ASK_ENABLED=true without ESHU_ASK_NARRATION_ENABLED=true yields
// Unavailable, not Available.
func TestBuildNarrationPostureAskEnabledButNarrationNotEnabled(t *testing.T) {
	t.Parallel()

	profileJSON := `[{"profile_id":"ask-agent","provider_kind":"anthropic","credential_source":{"kind":"environment_variable","handle":"ANTHROPIC_API_KEY"},"model_id":"claude-3-5-haiku-20241022","source_classes":["agent_reasoning"],"source_policy_configured":true}]`
	env := map[string]string{
		"ESHU_SEMANTIC_PROVIDER_PROFILES_JSON": profileJSON,
		"ESHU_ASK_ENABLED":                     "true",
		// ESHU_ASK_NARRATION_ENABLED deliberately not set (defaults false)
	}
	getenv := func(key string) string { return env[key] }

	postureFunc := buildNarrationPosture(getenv, nil)
	got := postureFunc()

	if got.State == status.AnswerNarrationAvailable {
		t.Fatalf("narration-disabled: State = %q, must NOT be Available when ESHU_ASK_NARRATION_ENABLED is not true", got.State)
	}
}

// TestBuildNarrationPostureNoProviderProfile verifies that when there is no
// agent_reasoning provider profile the posture returns Unavailable even if
// other flags are set.
func TestBuildNarrationPostureNoProviderProfile(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"ESHU_ASK_ENABLED":           "true",
		"ESHU_ASK_NARRATION_ENABLED": "true",
		// No ESHU_SEMANTIC_PROVIDER_PROFILES_JSON
	}
	getenv := func(key string) string { return env[key] }

	postureFunc := buildNarrationPosture(getenv, nil)
	got := postureFunc()

	if got.State == status.AnswerNarrationAvailable {
		t.Fatalf("no-provider: State = %q, must NOT be Available when no agent_reasoning profile is configured", got.State)
	}
	if got.ProviderConfigured {
		t.Fatal("no-provider: ProviderConfigured should be false when no agent_reasoning profile present")
	}
}

// TestBuildNarrationPostureReturnsCurrentTimeInUpdatedAt verifies that the
// posture func stamps UpdatedAt with a plausible wall-clock time on each call.
func TestBuildNarrationPostureReturnsCurrentTimeInUpdatedAt(t *testing.T) {
	t.Parallel()

	before := time.Now().UTC().Add(-time.Second)
	postureFunc := buildNarrationPosture(func(string) string { return "" }, nil)
	got := postureFunc()
	after := time.Now().UTC().Add(time.Second)

	if got.UpdatedAt.Before(before) || got.UpdatedAt.After(after) {
		t.Fatalf("UpdatedAt = %v, want between %v and %v", got.UpdatedAt, before, after)
	}
}
