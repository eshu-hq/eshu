// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import "time"

type answerNarrationJSON struct {
	State                          string   `json:"state"`
	Reason                         string   `json:"reason"`
	Detail                         string   `json:"detail,omitempty"`
	ProviderConfigured             bool     `json:"provider_configured"`
	ProviderTrafficEnabled         bool     `json:"provider_traffic_enabled"`
	PolicyAllowed                  bool     `json:"policy_allowed"`
	BudgetAvailable                bool     `json:"budget_available"`
	PublishSafetyEnabled           bool     `json:"publish_safety_enabled"`
	DeterministicFallbackAvailable bool     `json:"deterministic_fallback_available"`
	CanonicalTruthAffected         bool     `json:"canonical_truth_affected"`
	RetentionPosture               string   `json:"retention_posture"`
	PolicyHash                     string   `json:"policy_hash,omitempty"`
	SupportedStates                []string `json:"supported_states"`
	SupportedReasons               []string `json:"supported_reasons"`
	ValidatorReasonCodes           []string `json:"validator_reason_codes"`
	UpdatedAt                      string   `json:"updated_at,omitempty"`
}

func answerNarrationStatusJSON(snapshot AnswerNarrationStatus) answerNarrationJSON {
	normalized := normalizeAnswerNarrationStatus(snapshot)
	out := answerNarrationJSON{
		State:                          normalized.State,
		Reason:                         normalized.Reason,
		Detail:                         normalized.Detail,
		ProviderConfigured:             normalized.ProviderConfigured,
		ProviderTrafficEnabled:         normalized.ProviderTrafficEnabled,
		PolicyAllowed:                  normalized.PolicyAllowed,
		BudgetAvailable:                normalized.BudgetAvailable,
		PublishSafetyEnabled:           normalized.PublishSafetyEnabled,
		DeterministicFallbackAvailable: normalized.DeterministicFallbackAvailable,
		CanonicalTruthAffected:         normalized.CanonicalTruthAffected,
		RetentionPosture:               normalized.RetentionPosture,
		PolicyHash:                     normalized.PolicyHash,
		SupportedStates:                AnswerNarrationSupportedStates(),
		SupportedReasons:               AnswerNarrationSupportedReasons(),
		ValidatorReasonCodes:           AnswerNarrationValidatorReasonCodes(),
	}
	if !normalized.UpdatedAt.IsZero() {
		out.UpdatedAt = normalized.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return out
}
