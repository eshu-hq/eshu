// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychaincore

import "time"

// SupplyChainSuppressionState names the reducer decision for one finding
// after VEX, operator-policy, and provider-dismissal suppression facts have
// been evaluated against the finding's identity and evidence path.
type SupplyChainSuppressionState string

const (
	// SupplyChainSuppressionStateActive means no suppression matched the
	// finding; the finding is visible by default.
	SupplyChainSuppressionStateActive SupplyChainSuppressionState = "active"
	// SupplyChainSuppressionStateNotAffected means a VEX or operator-policy
	// suppression asserts the finding is not exploitable in this context.
	// Hidden from the default API view but available with include_suppressed.
	SupplyChainSuppressionStateNotAffected SupplyChainSuppressionState = "not_affected"
	// SupplyChainSuppressionStateAcceptedRisk means an operator has accepted
	// the residual risk. Hidden from the default view; explanation preserved.
	SupplyChainSuppressionStateAcceptedRisk SupplyChainSuppressionState = "accepted_risk"
	// SupplyChainSuppressionStateFalsePositive means an operator asserts the
	// finding is a false positive. Hidden from the default view.
	SupplyChainSuppressionStateFalsePositive SupplyChainSuppressionState = "false_positive"
	// SupplyChainSuppressionStateIgnored means a temporary operator ignore is
	// in effect. Hidden from the default view until expiration.
	SupplyChainSuppressionStateIgnored SupplyChainSuppressionState = "ignored"
	// SupplyChainSuppressionStateExpired means the matched suppression has an
	// expires_at that has already passed. The finding stays visible and the
	// expired suppression is preserved on the decision for audit.
	SupplyChainSuppressionStateExpired SupplyChainSuppressionState = "expired"
	// SupplyChainSuppressionStateProviderDismissed means a provider-dismissal
	// suppression points at provider-side evidence (for example a GitHub
	// Dependabot dismissal). Provider dismissals are evidence, not automatic
	// Eshu suppressions: the finding stays visible by default and the
	// provider link is preserved.
	SupplyChainSuppressionStateProviderDismissed SupplyChainSuppressionState = "provider_dismissed"
	// SupplyChainSuppressionStateScopeMismatch means a suppression existed for
	// adjacent identity but did not match the finding's identity or evidence
	// path. Preserved so operators can audit drift between the suppression's
	// intent and the actual finding shape.
	SupplyChainSuppressionStateScopeMismatch SupplyChainSuppressionState = "scope_mismatch"
)

// SupplyChainSuppressionDecision is the reducer's per-finding suppression
// outcome. It is always populated (state=active when no suppression matched)
// so the writer can persist a deterministic block and the API can explain
// suppression context regardless of whether the finding is hidden.
type SupplyChainSuppressionDecision struct {
	State          SupplyChainSuppressionState
	SuppressionID  string
	Source         string
	Justification  string
	Author         string
	AuthoredAt     time.Time
	ExpiresAt      time.Time
	Reason         string
	EvidenceRef    string
	VEXDocumentID  string
	VEXStatementID string
}
