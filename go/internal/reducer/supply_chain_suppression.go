// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychaincore"
)

// SupplyChainSuppressionState aliases
// [supplychaincore.SupplyChainSuppressionState]; the suppression vocabulary
// and the SupplyChainSuppressionDecision it labels moved to the shared
// supply-chain leaf (#6061) so the suppression evaluator here and the
// impact-finding writer can split into siblings without importing each other.
// The state helpers and the evaluator below stay in this package.
type SupplyChainSuppressionState = supplychaincore.SupplyChainSuppressionState

const (
	// SupplyChainSuppressionStateActive aliases [supplychaincore.SupplyChainSuppressionStateActive].
	SupplyChainSuppressionStateActive = supplychaincore.SupplyChainSuppressionStateActive
	// SupplyChainSuppressionStateNotAffected aliases [supplychaincore.SupplyChainSuppressionStateNotAffected].
	SupplyChainSuppressionStateNotAffected = supplychaincore.SupplyChainSuppressionStateNotAffected
	// SupplyChainSuppressionStateAcceptedRisk aliases [supplychaincore.SupplyChainSuppressionStateAcceptedRisk].
	SupplyChainSuppressionStateAcceptedRisk = supplychaincore.SupplyChainSuppressionStateAcceptedRisk
	// SupplyChainSuppressionStateFalsePositive aliases [supplychaincore.SupplyChainSuppressionStateFalsePositive].
	SupplyChainSuppressionStateFalsePositive = supplychaincore.SupplyChainSuppressionStateFalsePositive
	// SupplyChainSuppressionStateIgnored aliases [supplychaincore.SupplyChainSuppressionStateIgnored].
	SupplyChainSuppressionStateIgnored = supplychaincore.SupplyChainSuppressionStateIgnored
	// SupplyChainSuppressionStateExpired aliases [supplychaincore.SupplyChainSuppressionStateExpired].
	SupplyChainSuppressionStateExpired = supplychaincore.SupplyChainSuppressionStateExpired
	// SupplyChainSuppressionStateProviderDismissed aliases [supplychaincore.SupplyChainSuppressionStateProviderDismissed].
	SupplyChainSuppressionStateProviderDismissed = supplychaincore.SupplyChainSuppressionStateProviderDismissed
	// SupplyChainSuppressionStateScopeMismatch aliases [supplychaincore.SupplyChainSuppressionStateScopeMismatch].
	SupplyChainSuppressionStateScopeMismatch = supplychaincore.SupplyChainSuppressionStateScopeMismatch
)

// SupplyChainSuppressionStates returns every state the reducer can emit.
func SupplyChainSuppressionStates() []SupplyChainSuppressionState {
	return []SupplyChainSuppressionState{
		SupplyChainSuppressionStateActive,
		SupplyChainSuppressionStateNotAffected,
		SupplyChainSuppressionStateAcceptedRisk,
		SupplyChainSuppressionStateFalsePositive,
		SupplyChainSuppressionStateIgnored,
		SupplyChainSuppressionStateExpired,
		SupplyChainSuppressionStateProviderDismissed,
		SupplyChainSuppressionStateScopeMismatch,
	}
}

// SupplyChainSuppressionHiddenStates returns the states whose findings are
// hidden from the default API/MCP view (operator-asserted local
// suppressions). Provider dismissals, expired suppressions, and scope
// mismatches remain visible.
func SupplyChainSuppressionHiddenStates() []SupplyChainSuppressionState {
	return []SupplyChainSuppressionState{
		SupplyChainSuppressionStateNotAffected,
		SupplyChainSuppressionStateAcceptedRisk,
		SupplyChainSuppressionStateFalsePositive,
		SupplyChainSuppressionStateIgnored,
	}
}

// SupplyChainSuppressionStateIsHidden reports whether the state hides the
// finding from the default view. Callers can still opt in via
// include_suppressed.
func SupplyChainSuppressionStateIsHidden(state SupplyChainSuppressionState) bool {
	for _, hidden := range SupplyChainSuppressionHiddenStates() {
		if hidden == state {
			return true
		}
	}
	return false
}

// vulnerabilitySuppressionScope is the bounded scope a suppression applies to.
// Empty fields are wildcards.
type vulnerabilitySuppressionScope struct {
	CVEID         string
	AdvisoryID    string
	PackageID     string
	PURL          string
	RepositoryID  string
	SubjectDigest string
	EvidencePath  []string
	// Environment, WorkloadID, and ServiceID are optional conjuncts on a
	// discoverable vulnerability identity. They never identify a vulnerability
	// alone. Environment is canonicalized through the shared alias contract.
	//
	// Findings currently store one suppression decision over independent
	// flattened deployment lists. Every referenced dimension must therefore
	// have at most one observed value; otherwise one matching value would hide
	// sibling contexts. Ambiguous aggregates fail closed to scope_mismatch.
	Environment string
	WorkloadID  string
	ServiceID   string
}

// vulnerabilitySuppression is a decoded VEX or operator-policy suppression
// fact ready for reducer evaluation.
//
// ExpiresAtRaw, ExpiresAtPresent, and ExpiresAtParseFailed together let the
// evaluator distinguish three cases that must NOT collapse into one:
//
//   - missing expiration: ExpiresAtPresent=false → suppression is timeless
//   - valid expiration:   ExpiresAtPresent=true, ExpiresAtParseFailed=false →
//     compare ExpiresAt against the evaluation clock
//   - invalid expiration: ExpiresAtPresent=true, ExpiresAtParseFailed=true →
//     treat as already-expired so a malformed timestamp can never silently
//     extend the suppression's life. The raw value is preserved for audit.
type vulnerabilitySuppression struct {
	SuppressionID        string
	Source               string
	Justification        string
	Author               string
	AuthoredAt           time.Time
	ExpiresAt            time.Time
	ExpiresAtRaw         string
	ExpiresAtPresent     bool
	ExpiresAtParseFailed bool
	Reason               string
	Scope                vulnerabilitySuppressionScope
	EvidenceRef          string
	VEXDocumentID        string
	VEXStatementID       string
}

// SupplyChainSuppressionDecision aliases
// [supplychaincore.SupplyChainSuppressionDecision]; the value type moved to
// the shared supply-chain leaf (#6061). The evaluator that builds it stays
// in this package.
type SupplyChainSuppressionDecision = supplychaincore.SupplyChainSuppressionDecision

// EvaluateSupplyChainSuppression returns the suppression decision for one
// finding. Selection is deterministic:
//
//  1. Active operator/VEX suppression (unexpired, scope matches) wins; ties
//     broken by latest AuthoredAt, then lexicographic SuppressionID.
//  2. Provider-dismissal evidence wins when no operator suppression matched.
//  3. Expired suppression preserved when no active or provider match exists.
//  4. Scope-mismatch preserved when only mismatched suppressions exist.
//  5. Otherwise active.
//
// The decision retains suppression provenance for every non-active state so
// callers can explain why a finding is hidden or why a related suppression
// did not apply.
func EvaluateSupplyChainSuppression(
	finding SupplyChainImpactFinding,
	suppressions []vulnerabilitySuppression,
	now time.Time,
) SupplyChainSuppressionDecision {
	if len(suppressions) == 0 {
		return SupplyChainSuppressionDecision{State: SupplyChainSuppressionStateActive}
	}
	var (
		activeMatch   *vulnerabilitySuppression
		providerMatch *vulnerabilitySuppression
		expiredMatch  *vulnerabilitySuppression
		scopeMismatch *vulnerabilitySuppression
	)
	for i := range suppressions {
		suppression := &suppressions[i]
		if !suppressionAdjacent(finding, *suppression) {
			continue
		}
		if !suppressionScopeMatchesFinding(finding, *suppression) {
			scopeMismatch = preferredSuppression(scopeMismatch, suppression)
			continue
		}
		if suppressionIsExpired(*suppression, now) {
			expiredMatch = preferredSuppression(expiredMatch, suppression)
			continue
		}
		if suppression.Source == facts.VulnerabilitySuppressionSourceProviderDismissal {
			providerMatch = preferredSuppression(providerMatch, suppression)
			continue
		}
		activeMatch = preferredSuppression(activeMatch, suppression)
	}

	if activeMatch != nil {
		return decisionFromActiveOperatorSuppression(*activeMatch)
	}
	if providerMatch != nil {
		return decisionFromProviderSuppression(*providerMatch)
	}
	if expiredMatch != nil {
		return decisionFromExpiredSuppression(*expiredMatch)
	}
	if scopeMismatch != nil {
		return decisionFromScopeMismatch(finding, *scopeMismatch)
	}
	return SupplyChainSuppressionDecision{State: SupplyChainSuppressionStateActive}
}

// preferredSuppression returns the candidate that the former stable sort
// would place first: newest authored time, then lexicographically smallest
// ID. Exact ties preserve the first input record.
func preferredSuppression(
	current *vulnerabilitySuppression,
	candidate *vulnerabilitySuppression,
) *vulnerabilitySuppression {
	if current == nil ||
		candidate.AuthoredAt.After(current.AuthoredAt) ||
		(candidate.AuthoredAt.Equal(current.AuthoredAt) && candidate.SuppressionID < current.SuppressionID) {
		return candidate
	}
	return current
}

func decisionFromActiveOperatorSuppression(s vulnerabilitySuppression) SupplyChainSuppressionDecision {
	state := suppressionStateForJustification(s.Justification)
	return SupplyChainSuppressionDecision{
		State:          state,
		SuppressionID:  s.SuppressionID,
		Source:         s.Source,
		Justification:  s.Justification,
		Author:         s.Author,
		AuthoredAt:     s.AuthoredAt,
		ExpiresAt:      s.ExpiresAt,
		Reason:         suppressionReasonOrDefault(s, state),
		EvidenceRef:    s.EvidenceRef,
		VEXDocumentID:  s.VEXDocumentID,
		VEXStatementID: s.VEXStatementID,
	}
}

func decisionFromProviderSuppression(s vulnerabilitySuppression) SupplyChainSuppressionDecision {
	return SupplyChainSuppressionDecision{
		State:          SupplyChainSuppressionStateProviderDismissed,
		SuppressionID:  s.SuppressionID,
		Source:         s.Source,
		Justification:  s.Justification,
		Author:         s.Author,
		AuthoredAt:     s.AuthoredAt,
		ExpiresAt:      s.ExpiresAt,
		Reason:         suppressionReasonOrDefault(s, SupplyChainSuppressionStateProviderDismissed),
		EvidenceRef:    s.EvidenceRef,
		VEXDocumentID:  s.VEXDocumentID,
		VEXStatementID: s.VEXStatementID,
	}
}

func decisionFromExpiredSuppression(s vulnerabilitySuppression) SupplyChainSuppressionDecision {
	return SupplyChainSuppressionDecision{
		State:          SupplyChainSuppressionStateExpired,
		SuppressionID:  s.SuppressionID,
		Source:         s.Source,
		Justification:  s.Justification,
		Author:         s.Author,
		AuthoredAt:     s.AuthoredAt,
		ExpiresAt:      s.ExpiresAt,
		Reason:         suppressionExpiredReason(s),
		EvidenceRef:    s.EvidenceRef,
		VEXDocumentID:  s.VEXDocumentID,
		VEXStatementID: s.VEXStatementID,
	}
}

// suppressionIsExpired reports whether a suppression should be treated as
// expired by the evaluator. An unparseable expires_at MUST be expired so a
// malformed timestamp cannot extend a suppression's life. A missing
// expires_at means the suppression is timeless and never expires.
func suppressionIsExpired(s vulnerabilitySuppression, now time.Time) bool {
	if !s.ExpiresAtPresent {
		return false
	}
	if s.ExpiresAtParseFailed {
		return true
	}
	if s.ExpiresAt.IsZero() {
		return false
	}
	return !now.Before(s.ExpiresAt)
}

func suppressionExpiredReason(s vulnerabilitySuppression) string {
	if s.ExpiresAtParseFailed {
		raw := s.ExpiresAtRaw
		if strings.TrimSpace(raw) == "" {
			raw = "<unparseable>"
		}
		return fmt.Sprintf("suppression %s has invalid expires_at %q; treated as expired so a bad timestamp cannot extend the suppression", s.SuppressionID, raw)
	}
	if s.Reason != "" {
		return s.Reason
	}
	return fmt.Sprintf("suppression %s expired at %s", s.SuppressionID, s.ExpiresAt.UTC().Format(time.RFC3339))
}

func decisionFromScopeMismatch(finding SupplyChainImpactFinding, s vulnerabilitySuppression) SupplyChainSuppressionDecision {
	reason := suppressionScopeMismatchReason(finding, s)
	return SupplyChainSuppressionDecision{
		State:          SupplyChainSuppressionStateScopeMismatch,
		SuppressionID:  s.SuppressionID,
		Source:         s.Source,
		Justification:  s.Justification,
		Author:         s.Author,
		AuthoredAt:     s.AuthoredAt,
		ExpiresAt:      s.ExpiresAt,
		Reason:         reason,
		EvidenceRef:    s.EvidenceRef,
		VEXDocumentID:  s.VEXDocumentID,
		VEXStatementID: s.VEXStatementID,
	}
}

func defaultIfBlank(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return "unknown"
}
