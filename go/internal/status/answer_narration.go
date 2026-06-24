// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"slices"
	"strings"
	"time"
)

const (
	// AnswerNarrationUnavailable means optional answer narration cannot run.
	AnswerNarrationUnavailable = "unavailable"
	// AnswerNarrationDisabled means governance has disabled answer narration.
	AnswerNarrationDisabled = "disabled"
	// AnswerNarrationAvailable means governed answer narration may run.
	AnswerNarrationAvailable = "available"
	// AnswerNarrationProviderUnavailable means the configured provider path is unavailable.
	AnswerNarrationProviderUnavailable = "provider_unavailable"
)

const (
	// AnswerNarrationReasonDisabledByDefault is the zero-key default reason.
	AnswerNarrationReasonDisabledByDefault = "disabled_by_default"
	// AnswerNarrationReasonPolicyDenied marks a governance policy denial.
	AnswerNarrationReasonPolicyDenied = "policy_denied"
	// AnswerNarrationReasonBudgetExhausted marks a narration budget denial.
	AnswerNarrationReasonBudgetExhausted = "budget_exhausted"
	// AnswerNarrationReasonUnsafeOutput marks publish-safety rejection.
	AnswerNarrationReasonUnsafeOutput = "unsafe_output"
	// AnswerNarrationReasonTimeout marks narration timeout fallback.
	AnswerNarrationReasonTimeout = "timeout"
	// AnswerNarrationReasonProviderUnavailable marks provider or assistant unavailability.
	AnswerNarrationReasonProviderUnavailable = "provider_unavailable"
	// AnswerNarrationReasonAvailable marks fully allowed narration.
	AnswerNarrationReasonAvailable = "available"
	// AnswerNarrationReasonInvalidState marks malformed status input.
	AnswerNarrationReasonInvalidState = "invalid_answer_narration_state"
)

const (
	// AnswerNarrationRetentionMetadataOnly means no prompt or response bodies are retained.
	AnswerNarrationRetentionMetadataOnly = "metadata_only"
)

var (
	answerNarrationStates = []string{
		AnswerNarrationUnavailable,
		AnswerNarrationDisabled,
		AnswerNarrationAvailable,
		AnswerNarrationProviderUnavailable,
	}
	answerNarrationReasons = []string{
		AnswerNarrationReasonDisabledByDefault,
		AnswerNarrationReasonPolicyDenied,
		AnswerNarrationReasonBudgetExhausted,
		AnswerNarrationReasonUnsafeOutput,
		AnswerNarrationReasonTimeout,
		AnswerNarrationReasonProviderUnavailable,
		AnswerNarrationReasonAvailable,
		AnswerNarrationReasonInvalidState,
	}
	answerNarrationValidatorReasons = []string{
		"uncited_factual_sentence",
		"unsupported_confidence_drift",
		"partial_signal_hidden",
		"truth_class_promotion",
		"unsafe_output",
		"unknown_provenance",
		"over_limit",
	}
)

// AnswerNarrationStatus captures optional governed answer narration posture.
type AnswerNarrationStatus struct {
	State                          string
	Reason                         string
	Detail                         string
	ProviderConfigured             bool
	ProviderTrafficEnabled         bool
	PolicyAllowed                  bool
	BudgetAvailable                bool
	PublishSafetyEnabled           bool
	DeterministicFallbackAvailable bool
	CanonicalTruthAffected         bool
	RetentionPosture               string
	PolicyHash                     string
	UpdatedAt                      time.Time
}

// AnswerNarrationSupportedStates returns the stable narration status enum.
func AnswerNarrationSupportedStates() []string {
	return slices.Clone(answerNarrationStates)
}

// AnswerNarrationSupportedReasons returns the stable narration reason enum.
func AnswerNarrationSupportedReasons() []string {
	return slices.Clone(answerNarrationReasons)
}

// AnswerNarrationValidatorReasonCodes returns validator failure reason codes.
func AnswerNarrationValidatorReasonCodes() []string {
	return slices.Clone(answerNarrationValidatorReasons)
}

// DefaultAnswerNarrationStatus returns the zero-key narration status.
func DefaultAnswerNarrationStatus() AnswerNarrationStatus {
	return AnswerNarrationStatus{
		State:                          AnswerNarrationUnavailable,
		Reason:                         AnswerNarrationReasonDisabledByDefault,
		Detail:                         "answer narration is disabled by default; deterministic answer packets remain the canonical fallback",
		DeterministicFallbackAvailable: true,
		RetentionPosture:               AnswerNarrationRetentionMetadataOnly,
	}
}

func normalizeAnswerNarrationStatus(snapshot AnswerNarrationStatus) AnswerNarrationStatus {
	out := snapshot
	out.State = strings.TrimSpace(out.State)
	out.Reason = strings.TrimSpace(out.Reason)
	out.Detail = safeStatusDetail(out.Detail)
	out.PolicyHash = safeStatusDetail(out.PolicyHash)
	if out.State == "" {
		out = DefaultAnswerNarrationStatus()
		out.UpdatedAt = snapshot.UpdatedAt
		return out
	}
	if !isAnswerNarrationState(out.State) {
		out = DefaultAnswerNarrationStatus()
		out.Reason = AnswerNarrationReasonInvalidState
		out.Detail = "answer narration status is unsupported; treating narration as unavailable"
		out.UpdatedAt = snapshot.UpdatedAt
		return out
	}
	if out.Reason == "" {
		out.Reason = defaultAnswerNarrationReason(out.State)
	}
	if out.Detail == "" {
		out.Detail = defaultAnswerNarrationDetail(out.State)
	}
	if out.RetentionPosture == "" {
		out.RetentionPosture = AnswerNarrationRetentionMetadataOnly
	}
	out.DeterministicFallbackAvailable = true
	out.CanonicalTruthAffected = false
	if out.State != AnswerNarrationAvailable {
		out.ProviderTrafficEnabled = false
		out.PolicyAllowed = false
	}
	return out
}

func isAnswerNarrationState(state string) bool {
	return slices.Contains(answerNarrationStates, state)
}

func defaultAnswerNarrationReason(state string) string {
	switch state {
	case AnswerNarrationAvailable:
		return AnswerNarrationReasonAvailable
	case AnswerNarrationProviderUnavailable:
		return AnswerNarrationReasonProviderUnavailable
	case AnswerNarrationDisabled:
		return AnswerNarrationReasonPolicyDenied
	default:
		return AnswerNarrationReasonDisabledByDefault
	}
}

func defaultAnswerNarrationDetail(state string) string {
	switch state {
	case AnswerNarrationAvailable:
		return "answer narration is policy allowed; deterministic answer packets remain canonical"
	case AnswerNarrationProviderUnavailable:
		return "answer narration provider path is unavailable; deterministic answer packets remain canonical"
	default:
		return "answer narration is disabled; deterministic answer packets remain canonical"
	}
}

func safeStatusDetail(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, forbidden := range []string{"://", "prompt", "response", "credential", "/Users/", "/home/", ".internal"} {
		if strings.Contains(strings.ToLower(value), strings.ToLower(forbidden)) {
			return ""
		}
	}
	return value
}
