// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

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
		activeMatches   []vulnerabilitySuppression
		providerMatches []vulnerabilitySuppression
		expiredMatches  []vulnerabilitySuppression
		scopeMismatched []vulnerabilitySuppression
	)
	for _, s := range suppressions {
		if !suppressionAdjacent(finding, s) {
			continue
		}
		if !suppressionScopeMatchesFinding(finding, s) {
			scopeMismatched = append(scopeMismatched, s)
			continue
		}
		if suppressionIsExpired(s, now) {
			expiredMatches = append(expiredMatches, s)
			continue
		}
		if s.Source == facts.VulnerabilitySuppressionSourceProviderDismissal {
			providerMatches = append(providerMatches, s)
			continue
		}
		activeMatches = append(activeMatches, s)
	}

	if pick := pickPreferredSuppression(activeMatches); pick != nil {
		return decisionFromActiveOperatorSuppression(*pick)
	}
	if pick := pickPreferredSuppression(providerMatches); pick != nil {
		return decisionFromProviderSuppression(*pick)
	}
	if pick := pickPreferredSuppression(expiredMatches); pick != nil {
		return decisionFromExpiredSuppression(*pick)
	}
	if pick := pickPreferredSuppression(scopeMismatched); pick != nil {
		return decisionFromScopeMismatch(finding, *pick)
	}
	return SupplyChainSuppressionDecision{State: SupplyChainSuppressionStateActive}
}

// suppressionAdjacent reports whether a suppression names at least one anchor
// the finding also has, so we can tell "could this suppression apply to this
// finding's identity at all?" from "applies but scope did not line up." An
// empty scope is still treated as adjacent so the suppression is preserved on
// every finding decision for audit, but suppressionScopeMatchesFinding
// rejects empty scope so it never silently hides a finding.
func suppressionAdjacent(finding SupplyChainImpactFinding, s vulnerabilitySuppression) bool {
	if suppressionScopeIsEmpty(s.Scope) {
		return true
	}
	if s.Scope.CVEID != "" && strings.EqualFold(s.Scope.CVEID, finding.CVEID) {
		return true
	}
	if s.Scope.AdvisoryID != "" && strings.EqualFold(s.Scope.AdvisoryID, finding.AdvisoryID) {
		return true
	}
	if s.Scope.PackageID != "" && strings.EqualFold(s.Scope.PackageID, finding.PackageID) {
		return true
	}
	if s.Scope.PURL != "" && strings.EqualFold(s.Scope.PURL, finding.PURL) {
		return true
	}
	if s.Scope.RepositoryID != "" && strings.EqualFold(s.Scope.RepositoryID, finding.RepositoryID) {
		return true
	}
	if s.Scope.SubjectDigest != "" && strings.EqualFold(s.Scope.SubjectDigest, finding.SubjectDigest) {
		return true
	}
	return false
}

// suppressionScopeMatchesFinding returns true only when every populated scope
// key matches the finding. Empty scope keys act as wildcards within an
// otherwise-bounded scope, but a scope that names nothing at all is treated
// as a mismatch so a malformed or missing scope payload can never silently
// hide every finding (the suppression still surfaces as scope_mismatch for
// audit). Evidence path entries must all appear in the finding's evidence
// path.
func suppressionScopeMatchesFinding(finding SupplyChainImpactFinding, s vulnerabilitySuppression) bool {
	if suppressionScopeIsEmpty(s.Scope) {
		return false
	}
	if !scopeAnchorMatches(s.Scope.CVEID, finding.CVEID) {
		return false
	}
	if !scopeAnchorMatches(s.Scope.AdvisoryID, finding.AdvisoryID) {
		return false
	}
	if !scopeAnchorMatches(s.Scope.PackageID, finding.PackageID) {
		return false
	}
	if !scopeAnchorMatches(s.Scope.PURL, finding.PURL) {
		return false
	}
	if !scopeAnchorMatches(s.Scope.RepositoryID, finding.RepositoryID) {
		return false
	}
	if !scopeAnchorMatches(s.Scope.SubjectDigest, finding.SubjectDigest) {
		return false
	}
	if !evidencePathContainsAll(finding.EvidencePath, s.Scope.EvidencePath) {
		return false
	}
	return true
}

func scopeAnchorMatches(scoped, observed string) bool {
	scoped = strings.TrimSpace(scoped)
	if scoped == "" {
		return true
	}
	return strings.EqualFold(scoped, strings.TrimSpace(observed))
}

func evidencePathContainsAll(observed []string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	have := make(map[string]struct{}, len(observed))
	for _, step := range observed {
		step = strings.TrimSpace(step)
		if step == "" {
			continue
		}
		have[step] = struct{}{}
	}
	for _, step := range required {
		step = strings.TrimSpace(step)
		if step == "" {
			continue
		}
		if _, ok := have[step]; !ok {
			return false
		}
	}
	return true
}

func suppressionScopeIsEmpty(scope vulnerabilitySuppressionScope) bool {
	return strings.TrimSpace(scope.CVEID) == "" &&
		strings.TrimSpace(scope.AdvisoryID) == "" &&
		strings.TrimSpace(scope.PackageID) == "" &&
		strings.TrimSpace(scope.PURL) == "" &&
		strings.TrimSpace(scope.RepositoryID) == "" &&
		strings.TrimSpace(scope.SubjectDigest) == "" &&
		len(scope.EvidencePath) == 0
}

func pickPreferredSuppression(matches []vulnerabilitySuppression) *vulnerabilitySuppression {
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if !matches[i].AuthoredAt.Equal(matches[j].AuthoredAt) {
			return matches[i].AuthoredAt.After(matches[j].AuthoredAt)
		}
		return matches[i].SuppressionID < matches[j].SuppressionID
	})
	picked := matches[0]
	return &picked
}

func decisionFromActiveOperatorSuppression(s vulnerabilitySuppression) SupplyChainSuppressionDecision {
	state := suppressionStateForJustification(s.Justification)
	if state == SupplyChainSuppressionStateActive {
		// Defensive fallback: an unknown justification on an otherwise-active
		// operator suppression is still a suppression. Hide it as ignored so
		// operators see it and can correct the input rather than silently
		// shipping it as active.
		state = SupplyChainSuppressionStateIgnored
	}
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
