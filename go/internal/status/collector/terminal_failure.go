// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"slices"
	"strings"
	"time"
)

// UnknownFailureToken is the bounded value a failure class or operation takes
// when the persisted token is empty, outside the allowlist, or not shaped like
// a bounded operation token. Status surfaces never echo the raw persisted text.
const UnknownFailureToken = "unknown"

// maxFailureOperationLength caps a failure operation token. The collector
// runner persists short snake_case stage names; anything longer is not a token.
const maxFailureOperationLength = 48

// TerminalFailure is the newest unresolved terminal work-item failure for one
// collector instance. The reader supplies only rows for enabled,
// non-deactivated instances whose work item reached failed_terminal inside the
// instance's window (3 x scan_interval, 30 minute floor) and has no later
// completed work item of the same instance for the same scope, so a terminal
// failure followed by a later success never appears here, and an orphaned one
// ages out after the window.
//
// Every TerminalFailure is terminal by construction: the reader selects rows
// by the failed_terminal status, not by the last_failure_terminal flag, which
// the scanner worker leaves false when it dead-letters a claim. There is no
// per-row terminal flag to consult.
//
// FailureClass and FailureOperation are the bounded tokens the claim runner
// persisted. The failure message and provider status code are deliberately
// absent: they can carry provider response text and must not reach a status
// payload.
type TerminalFailure struct {
	CollectorKind    string
	InstanceID       string
	FailureClass     string
	FailureOperation string
	ObservedAt       time.Time
}

// boundedFailureClasses is every failure class the security-alert provider and
// the claim runner itself persist on a terminal work item. Other collector
// kinds persist their own bounded classes through the same
// classifiedFailureClass path (for example registry_auth_denied,
// permission_denied, stale_fence, and any extension-host class); those read as
// UnknownFailureToken here until a per-kind set lands (#8896). A class outside
// the set reads as UnknownFailureToken, so free text never reaches a status
// payload or metric label. The literals are pinned here, not imported, so the
// status leaf stays free of collector runtime packages. Sources:
//
//   - auth_denied, not_found, rate_limited, terminal, retryable: the provider
//     classes (go/internal/collector/sdk/failure.go, mapped by
//     collector/securityalerts/alertruntime/failure.go). failTerminal prefers
//     the class from the error chain (classifiedFailureClass in
//     collector/claimed_service.go), and budgetExhaustedError wraps the cause,
//     so a budget exhausted on a transient provider failure persists
//     retryable on a terminal row.
//   - attempt_budget_exhausted: the budget class when the cause carries no
//     class (FailureClassAttemptBudgetExhausted, collector/claimed_service.go).
//   - identity_mismatch: a claim whose collected identity does not match (the
//     FailClaimTerminal calls in collector/claimed_service.go).
//   - collect_failure, commit_failure: the claim-stage fallbacks for a
//     non-retryable collect or commit error (collector/claimed_service.go).
//
// Keep the set aligned with securityAlertTerminalFailureClasses in the
// security-alert read (#8803), which pins the same nine classes.
var boundedFailureClasses = map[string]struct{}{
	"auth_denied":              {},
	"not_found":                {},
	"rate_limited":             {},
	"terminal":                 {},
	"retryable":                {},
	"attempt_budget_exhausted": {},
	"identity_mismatch":        {},
	"collect_failure":          {},
	"commit_failure":           {},
}

// BoundedFailureClasses returns the bounded failure class set, sorted. It
// excludes UnknownFailureToken, which a status surface reports for any class
// outside the set. OpenAPI enum tests compare the published enums to it.
func BoundedFailureClasses() []string {
	out := make([]string, 0, len(boundedFailureClasses))
	for class := range boundedFailureClasses {
		out = append(out, class)
	}
	slices.Sort(out)
	return out
}

// BoundedFailureClass returns class when it is in the bounded failure class
// set and UnknownFailureToken otherwise.
func BoundedFailureClass(class string) string {
	class = strings.TrimSpace(class)
	if _, ok := boundedFailureClasses[class]; ok {
		return class
	}
	return UnknownFailureToken
}

// BoundedFailureOperation returns operation when it is a short lowercase
// snake_case token and UnknownFailureToken otherwise. It rejects URLs,
// headers, sentences, and anything else that could carry provider text.
func BoundedFailureOperation(operation string) string {
	operation = strings.TrimSpace(operation)
	if operation == "" || len(operation) > maxFailureOperationLength {
		return UnknownFailureToken
	}
	for _, r := range operation {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
		default:
			return UnknownFailureToken
		}
	}
	return operation
}

// terminalFailureDetail is the shared operator wording for a terminal fetch
// failure, used by the runtime detail and the promotion blocker. The wording
// covers provider classes and the runner's own classes (attempt budget,
// identity mismatch, collect or commit fallback) alike, matching the
// terminal_fetch_failure reason the security-alert read reports.
func terminalFailureDetail(class string, operation string) string {
	return "terminal fetch failure: " + class + " (" + operation + ")"
}

// applyTerminalFailures stamps the newest unresolved terminal failure onto the
// coordinator-registered runtime row it belongs to. A failure without a
// matching registered row is ignored: failure evidence never creates a
// collector identity. The reader never returns a failure for a disabled or
// deactivated instance, so the disabled guard below is defense in depth for
// other callers: it runs before any write, so a disabled row keeps its health
// and detail and carries no failure fields.
func (b *runtimeStatusBuilder) applyTerminalFailures(failures []TerminalFailure) {
	for _, failure := range failures {
		key := runtimeStatusKey(strings.TrimSpace(failure.CollectorKind), strings.TrimSpace(failure.InstanceID))
		index, ok := b.byKey[key]
		if !ok || !b.statuses[index].CoordinatorRegistered {
			continue
		}
		row := &b.statuses[index]
		if row.Health == "disabled" {
			continue
		}
		if row.LastFailureTerminal && !failure.ObservedAt.After(row.LastFailureAt) {
			continue
		}
		row.LastFailureClass = BoundedFailureClass(failure.FailureClass)
		row.LastFailureOperation = BoundedFailureOperation(failure.FailureOperation)
		row.LastFailureTerminal = true
		row.LastFailureAt = failure.ObservedAt
		row.Health = combineRuntimeHealth(row.Health, "degraded")
		row.Detail = terminalFailureDetail(row.LastFailureClass, row.LastFailureOperation)
	}
}
