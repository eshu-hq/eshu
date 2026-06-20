package governance

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// PostureInputs carries the bounded set of gate signals that govern whether
// Ask Eshu answer narration is permitted. It contains only boolean flags
// derived from external configuration; it never holds secrets, provider
// credentials, question text, or tenant identifiers.
//
// All fields default to false. The governance posture is default-CLOSED: every
// gate must be explicitly opened for narration to be permitted.
type PostureInputs struct {
	// ProviderConfigured is true when a valid narration provider has been
	// registered and its credentials are present.
	ProviderConfigured bool
	// ProviderTrafficEnabled is true when the narration provider traffic
	// circuit is open (no kill-switch or maintenance-mode flag is active).
	ProviderTrafficEnabled bool
	// PolicyAllowed is true when the active governance policy permits
	// narration for the current scope.
	PolicyAllowed bool
	// BudgetAvailable is true when the narration budget (token or cost
	// quota) has not been exhausted for the current period.
	BudgetAvailable bool
	// PublishSafetyEnabled is true when the publish-safety gate is active.
	// Narration is only allowed when publish safety is running; disabling
	// the safety gate closes narration rather than opening it.
	PublishSafetyEnabled bool
}

// ResolvePosture computes the governed narration posture from in and the wall
// clock instant now. It returns a status.AnswerNarrationStatus whose State is
// AnswerNarrationAvailable only when every gate in in is true; otherwise the
// most-specific Reason for the denial is encoded.
//
// Precedence order (first failing gate wins):
//
//  1. !ProviderConfigured or !ProviderTrafficEnabled → ProviderUnavailable
//  2. !PolicyAllowed                                 → PolicyDenied
//  3. !BudgetAvailable                               → BudgetExhausted
//  4. !PublishSafetyEnabled                          → DisabledByDefault (catch-all)
//
// Invariants that hold for every return value:
//   - DeterministicFallbackAvailable is always true.
//   - CanonicalTruthAffected is always false.
//   - RetentionPosture is always AnswerNarrationRetentionMetadataOnly.
//   - UpdatedAt is set to now.
//
// now is accepted as a parameter so callers can inject a deterministic clock in
// tests without patching a global.
func ResolvePosture(in PostureInputs, now time.Time) status.AnswerNarrationStatus {
	base := status.AnswerNarrationStatus{
		DeterministicFallbackAvailable: true,
		CanonicalTruthAffected:         false,
		RetentionPosture:               status.AnswerNarrationRetentionMetadataOnly,
		UpdatedAt:                      now,
		// Mirror input gate booleans unconditionally so that every return path
		// (Available or any denied branch) reports the actual operator-visible
		// diagnostic state rather than defaulting to false on denied paths.
		ProviderConfigured:     in.ProviderConfigured,
		ProviderTrafficEnabled: in.ProviderTrafficEnabled,
		PolicyAllowed:          in.PolicyAllowed,
		BudgetAvailable:        in.BudgetAvailable,
		PublishSafetyEnabled:   in.PublishSafetyEnabled,
	}

	if !in.ProviderConfigured || !in.ProviderTrafficEnabled {
		base.State = status.AnswerNarrationProviderUnavailable
		base.Reason = status.AnswerNarrationReasonProviderUnavailable
		base.Detail = "narration provider is not configured or provider traffic is disabled"
		return base
	}

	if !in.PolicyAllowed {
		base.State = status.AnswerNarrationDisabled
		base.Reason = status.AnswerNarrationReasonPolicyDenied
		base.Detail = "governance policy denies narration for the current scope"
		return base
	}

	if !in.BudgetAvailable {
		base.State = status.AnswerNarrationUnavailable
		base.Reason = status.AnswerNarrationReasonBudgetExhausted
		base.Detail = "narration budget is exhausted for the current period"
		return base
	}

	if !in.PublishSafetyEnabled {
		base.State = status.AnswerNarrationUnavailable
		base.Reason = status.AnswerNarrationReasonDisabledByDefault
		base.Detail = "publish-safety gate is not active; narration requires publish safety to be running"
		return base
	}

	base.State = status.AnswerNarrationAvailable
	base.Reason = status.AnswerNarrationReasonAvailable
	base.Detail = "all governance gates are open; narration is permitted"
	return base
}
