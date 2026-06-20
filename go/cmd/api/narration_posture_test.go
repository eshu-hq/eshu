package main

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// TestBuildNarrationPostureDefaultClosed verifies that with no env vars set
// (all defaults) the posture source returns Unavailable — never Available.
// This is the mandatory default-closed invariant.
func TestBuildNarrationPostureDefaultClosed(t *testing.T) {
	t.Parallel()

	postureFunc := buildNarrationPosture(func(string) string { return "" }, false)
	got := postureFunc()

	if got.State == status.AnswerNarrationAvailable {
		t.Fatalf("default-closed violated: State = %q, must NOT be Available when no config is set", got.State)
	}
	if !got.DeterministicFallbackAvailable {
		t.Fatal("DeterministicFallbackAvailable must always be true")
	}
}

// TestBuildNarrationPostureAllGatesOpen verifies that when every gate env var
// is set to its enabling value and the adapter was actually built (adapterReady
// = true) the posture source returns Available.
func TestBuildNarrationPostureAllGatesOpen(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"ESHU_ASK_ENABLED":           "true",
		"ESHU_ASK_NARRATION_ENABLED": "true",
	}
	getenv := func(key string) string { return env[key] }

	// adapterReady=true simulates successful provider.NewAdapter construction.
	postureFunc := buildNarrationPosture(getenv, true)
	got := postureFunc()

	if got.State != status.AnswerNarrationAvailable {
		t.Fatalf("all-gates-open: State = %q (Reason=%q), want Available; check that every gate is properly derived",
			got.State, got.Reason)
	}
	if !got.ProviderConfigured {
		t.Fatal("all-gates-open: ProviderConfigured should be true when adapterReady=true")
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

	env := map[string]string{
		"ESHU_ASK_ENABLED": "true",
		// ESHU_ASK_NARRATION_ENABLED deliberately not set (defaults false)
	}
	getenv := func(key string) string { return env[key] }

	postureFunc := buildNarrationPosture(getenv, true)
	got := postureFunc()

	if got.State == status.AnswerNarrationAvailable {
		t.Fatalf("narration-disabled: State = %q, must NOT be Available when ESHU_ASK_NARRATION_ENABLED is not true", got.State)
	}
}

// TestBuildNarrationPostureAdapterNotReady verifies that when adapterReady is
// false (adapter construction failed — e.g. credential env var unset) the
// posture returns ProviderUnavailable even when the enable flags are set and a
// profile entry exists. This is the regression test for the consistency bug
// where the status endpoint could report Available while POST /api/v0/ask
// returned 503.
func TestBuildNarrationPostureAdapterNotReady(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"ESHU_ASK_ENABLED":           "true",
		"ESHU_ASK_NARRATION_ENABLED": "true",
		// adapterReady=false below: simulates provider.NewAdapter failure due to
		// unset credential env var (the credential handle is set in the profile
		// but the referenced env var is absent).
	}
	getenv := func(key string) string { return env[key] }

	postureFunc := buildNarrationPosture(getenv, false /* adapterReady */)
	got := postureFunc()

	if got.State == status.AnswerNarrationAvailable {
		t.Fatalf("unbuildable-adapter: State = %q, must NOT be Available when the adapter could not be constructed; "+
			"status endpoint must match the unavailability of POST /api/v0/ask", got.State)
	}
	if got.ProviderConfigured {
		t.Fatal("unbuildable-adapter: ProviderConfigured must be false when adapterReady=false")
	}
}

// TestBuildNarrationPostureAdapterReadyDrivesProviderConfigured is an
// end-to-end regression test that exercises the real provider.NewAdapter path.
// It constructs two environments:
//   - profile present but credential env var UNSET → adapter fails → status must be non-Available.
//   - profile present and credential env var SET    → adapter builds → status must be Available.
//
// This proves that buildAskHandler.adapterReady() correctly feeds
// ProviderConfigured, eliminating the profile-presence vs adapter-readiness split.
func TestBuildNarrationPostureAdapterReadyDrivesProviderConfigured(t *testing.T) {
	t.Parallel()

	profileJSON := `[{"profile_id":"ask-agent","provider_kind":"anthropic","credential_source":{"kind":"environment_variable","handle":"ANTHROPIC_API_KEY"},"model_id":"claude-3-5-haiku-20241022","source_classes":["agent_reasoning"],"source_policy_configured":true}]`

	t.Run("credential env var unset → adapter fails → ProviderUnavailable", func(t *testing.T) {
		t.Parallel()

		// Env has the profile JSON but ANTHROPIC_API_KEY is absent.
		env := map[string]string{
			semanticprofile.EnvProviderProfilesJSON: profileJSON,
			"ESHU_ASK_ENABLED":                      "true",
			"ESHU_ASK_NARRATION_ENABLED":            "true",
			// ANTHROPIC_API_KEY intentionally absent
		}
		getenv := func(key string) string { return env[key] }

		// Exercise the real adapter path to confirm it fails.
		profiles, err := semanticprofile.ParseProfilesJSON(profileJSON)
		if err != nil || len(profiles) == 0 {
			t.Fatalf("test setup: cannot parse profile JSON: %v", err)
		}
		_, adapterErr := provider.NewAdapter(profiles[0], getenv)
		if adapterErr == nil {
			t.Fatal("test setup: expected provider.NewAdapter to fail when credential env var is unset, but it succeeded")
		}
		adapterReady := adapterErr == nil // false

		postureFunc := buildNarrationPosture(getenv, adapterReady)
		got := postureFunc()

		if got.State == status.AnswerNarrationAvailable {
			t.Fatalf("unset-cred: State = %q, want non-Available; "+
				"the status endpoint must not report Available when the adapter cannot be built", got.State)
		}
		if got.ProviderConfigured {
			t.Fatal("unset-cred: ProviderConfigured must be false when provider.NewAdapter failed")
		}
	})

	t.Run("credential env var set → adapter builds → Available", func(t *testing.T) {
		t.Parallel()

		// Env has the profile JSON AND the credential env var present.
		env := map[string]string{
			semanticprofile.EnvProviderProfilesJSON: profileJSON,
			"ESHU_ASK_ENABLED":                      "true",
			"ESHU_ASK_NARRATION_ENABLED":            "true",
			"ANTHROPIC_API_KEY":                     "sk-ant-test-key",
		}
		getenv := func(key string) string { return env[key] }

		// Exercise the real adapter path to confirm it succeeds.
		profiles, err := semanticprofile.ParseProfilesJSON(profileJSON)
		if err != nil || len(profiles) == 0 {
			t.Fatalf("test setup: cannot parse profile JSON: %v", err)
		}
		_, adapterErr := provider.NewAdapter(profiles[0], getenv)
		adapterReady := adapterErr == nil // true

		postureFunc := buildNarrationPosture(getenv, adapterReady)
		got := postureFunc()

		if got.State != status.AnswerNarrationAvailable {
			t.Fatalf("set-cred: State = %q (Reason=%q), want Available when adapter builds and flags are set",
				got.State, got.Reason)
		}
		if !got.ProviderConfigured {
			t.Fatal("set-cred: ProviderConfigured must be true when provider.NewAdapter succeeded")
		}
	})
}

// TestBuildNarrationPostureNoProviderProfile verifies that when adapterReady is
// false (no profile configured, so no adapter attempted) the posture returns
// Unavailable.
func TestBuildNarrationPostureNoProviderProfile(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"ESHU_ASK_ENABLED":           "true",
		"ESHU_ASK_NARRATION_ENABLED": "true",
		// No profile → buildAskHandler returns adapterReady()=false
	}
	getenv := func(key string) string { return env[key] }

	postureFunc := buildNarrationPosture(getenv, false /* adapterReady */)
	got := postureFunc()

	if got.State == status.AnswerNarrationAvailable {
		t.Fatalf("no-provider: State = %q, must NOT be Available when no agent_reasoning profile is configured", got.State)
	}
	if got.ProviderConfigured {
		t.Fatal("no-provider: ProviderConfigured should be false when adapterReady=false")
	}
}

// TestBuildNarrationPostureReturnsCurrentTimeInUpdatedAt verifies that the
// posture func stamps UpdatedAt with a plausible wall-clock time on each call.
func TestBuildNarrationPostureReturnsCurrentTimeInUpdatedAt(t *testing.T) {
	t.Parallel()

	before := time.Now().UTC().Add(-time.Second)
	postureFunc := buildNarrationPosture(func(string) string { return "" }, false)
	got := postureFunc()
	after := time.Now().UTC().Add(time.Second)

	if got.UpdatedAt.Before(before) || got.UpdatedAt.After(after) {
		t.Fatalf("UpdatedAt = %v, want between %v and %v", got.UpdatedAt, before, after)
	}
}
