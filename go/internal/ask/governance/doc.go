// Package governance implements the governed-narration posture resolver and
// audit-safe observability vocabulary for Ask Eshu.
//
// # Default-closed posture
//
// Ask Eshu answer narration is OFF by default. [ResolvePosture] returns a
// status.AnswerNarrationStatus whose State is [status.AnswerNarrationAvailable]
// only when ALL five gates in [PostureInputs] are true:
//
//   - ProviderConfigured
//   - ProviderTrafficEnabled
//   - PolicyAllowed
//   - BudgetAvailable
//   - PublishSafetyEnabled
//
// Any closed gate produces a specific Reason so operators can diagnose denials
// without exposing secret or tenant data. The deterministic answer packet path
// is always available (DeterministicFallbackAvailable is always true).
//
// # Deterministic-canonical invariant
//
// Narrated prose is an optional validated layer. AnswerPackets produced by the
// Eshu query surface are the canonical truth regardless of narration posture.
// CanonicalTruthAffected is always false in every return value of ResolvePosture.
//
// # Bounded, leak-safe telemetry
//
// [AskOutcome] and [AskStage] are bounded low-cardinality enums for ask-session
// observability. They MUST NOT encode question text, provider response bodies,
// or tenant identifiers. See the cardinality contract on each type.
//
// # Security and governance references
//
//   - ADR #2462: governed narration design — this package is the enable-gate
//     implementation referenced by that ADR.
//   - Issue #1755 / #1900 / #1902: Tier-2 security review. This package is
//     where the narration enable-gates are computed; any change to PostureInputs
//     or the ResolvePosture logic MUST be included in the Tier-2 review scope.
//
// # Wiring
//
// The API and engine layers wire this package as the narration posture source:
//
//	posture := func() status.AnswerNarrationStatus {
//	    return governance.ResolvePosture(buildInputs(), time.Now())
//	}
//	engine.SetNarrationPosture(posture)
package governance
