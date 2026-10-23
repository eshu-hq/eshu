// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
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

// boundedFailureClasses is the closed set of failure classes the claim runner
// persists (alertruntime failure classes plus the runner defaults). It is
// duplicated here, not imported, so the status leaf stays free of collector
// runtime packages. A class outside the set reads as UnknownFailureToken.
var boundedFailureClasses = map[string]struct{}{
	"auth_denied":  {},
	"not_found":    {},
	"rate_limited": {},
	"terminal":     {},
	"retryable":    {},
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

// terminalFailureDetail is the shared operator wording for a terminal provider
// failure, used by the runtime detail and the promotion blocker.
func terminalFailureDetail(class string, operation string) string {
	return "terminal provider failure: " + class + " (" + operation + ")"
}

// applyTerminalFailures stamps the newest unresolved terminal failure onto the
// coordinator-registered runtime row it belongs to. A failure without a
// matching registered row is ignored: failure evidence never creates a
// collector identity. The reader never returns a failure for a disabled or
// deactivated instance, so the disabled guard below is defense in depth for
// other callers: it keeps a disabled row's health and detail unchanged.
func (b *runtimeStatusBuilder) applyTerminalFailures(failures []TerminalFailure) {
	for _, failure := range failures {
		key := runtimeStatusKey(strings.TrimSpace(failure.CollectorKind), strings.TrimSpace(failure.InstanceID))
		index, ok := b.byKey[key]
		if !ok || !b.statuses[index].CoordinatorRegistered {
			continue
		}
		row := &b.statuses[index]
		if row.LastFailureTerminal && !failure.ObservedAt.After(row.LastFailureAt) {
			continue
		}
		row.LastFailureClass = BoundedFailureClass(failure.FailureClass)
		row.LastFailureOperation = BoundedFailureOperation(failure.FailureOperation)
		row.LastFailureTerminal = true
		row.LastFailureAt = failure.ObservedAt
		if row.Health == "disabled" {
			continue
		}
		row.Health = combineRuntimeHealth(row.Health, "degraded")
		row.Detail = terminalFailureDetail(row.LastFailureClass, row.LastFailureOperation)
	}
}
