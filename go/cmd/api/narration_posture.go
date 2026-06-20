package main

import (
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/ask/governance"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// envAskNarrationEnabled is the environment variable that enables governed
// answer narration. Both ESHU_ASK_ENABLED and ESHU_ASK_NARRATION_ENABLED must
// be "true" for narration to be permitted; either alone is insufficient.
//
// Default: false (narration is default-closed).
//
// v1 conservative wiring: PolicyAllowed, BudgetAvailable, and
// PublishSafetyEnabled are all derived from ProviderTrafficEnabled (i.e. they
// are true only when both ask flags are true and a provider is configured).
// Fine-grained per-gate env vars can be added in a follow-up when the
// corresponding governance stores are wired.
const envAskNarrationEnabled = "ESHU_ASK_NARRATION_ENABLED"

// buildNarrationPosture constructs a func that resolves the current governed
// answer-narration posture from runtime configuration. The returned func is
// safe to call concurrently and reads only from the closed-over getenv, so it
// can be shared between the engine and the status endpoint.
//
// Gate derivation (v1):
//   - ProviderConfigured     = an agent_reasoning provider profile is registered.
//   - ProviderTrafficEnabled = ESHU_ASK_ENABLED=true AND ESHU_ASK_NARRATION_ENABLED=true.
//   - PolicyAllowed          = same as ProviderTrafficEnabled (v1 conservative default).
//   - BudgetAvailable        = same as ProviderTrafficEnabled (v1 conservative default).
//   - PublishSafetyEnabled   = same as ProviderTrafficEnabled (v1 conservative default).
//
// The posture is default-CLOSED: if any gate is false, ResolvePosture returns
// a non-Available state and the engine will not narrate.
func buildNarrationPosture(
	getenv func(string) string,
	logger *slog.Logger,
) func() status.AnswerNarrationStatus {
	providerConfigured := hasAgentReasoningProfile(getenv, logger)
	trafficEnabled := isAskEnabled(getenv) && isNarrationEnabled(getenv)

	return func() status.AnswerNarrationStatus {
		in := governance.PostureInputs{
			ProviderConfigured:     providerConfigured,
			ProviderTrafficEnabled: trafficEnabled,
			// v1 conservative wiring: policy, budget, and publish safety are
			// gated to the same bool as traffic. They are documented as
			// defaulting to false unless traffic is open, which satisfies the
			// default-closed requirement while giving operators a single pair
			// of flags (ESHU_ASK_ENABLED + ESHU_ASK_NARRATION_ENABLED) to
			// open narration in v1 deployments.
			PolicyAllowed:        trafficEnabled,
			BudgetAvailable:      trafficEnabled,
			PublishSafetyEnabled: trafficEnabled,
		}
		return governance.ResolvePosture(in, time.Now().UTC())
	}
}

// isNarrationEnabled reports whether ESHU_ASK_NARRATION_ENABLED is "true".
func isNarrationEnabled(getenv func(string) string) bool {
	return strings.EqualFold(strings.TrimSpace(getenv(envAskNarrationEnabled)), "true")
}

// hasAgentReasoningProfile reports whether getenv contains a valid
// ESHU_SEMANTIC_PROVIDER_PROFILES_JSON entry with source class agent_reasoning.
// Parsing errors are treated as absent (false), not fatal, so a misconfigured
// profile JSON does not break the status endpoint.
func hasAgentReasoningProfile(getenv func(string) string, logger *slog.Logger) bool {
	raw := strings.TrimSpace(getenv(semanticprofile.EnvProviderProfilesJSON))
	if raw == "" {
		return false
	}
	profiles, err := semanticprofile.ParseProfilesJSON(raw)
	if err != nil {
		if logger != nil {
			logger.Warn("narration posture: cannot parse provider profiles; treating provider as absent",
				"err_type", "profile_parse")
		}
		return false
	}
	for _, p := range profiles {
		if slices.Contains(p.SourceClasses, semanticprofile.SourceAgentReasoning) {
			return true
		}
	}
	return false
}
