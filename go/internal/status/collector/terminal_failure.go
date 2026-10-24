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

// TerminalFailure is a terminal work-item failure for one collector instance.
// The reader supplies up to two per instance, only for enabled, non-deactivated
// instances: the newest unresolved failure no operator acknowledgement covers
// (Acknowledged false), and the newest unresolved failure an acknowledgement
// covers (Acknowledged true). Each reached failed_terminal inside the instance's
// window (3 x scan_interval, 30 minute floor) and has no later completed work
// item of the same instance for the same scope, so a terminal failure followed
// by a later success never appears here, and an orphaned one ages out after the
// window.
//
// Every TerminalFailure is terminal by construction: the reader selects rows by
// the failed_terminal status, not by the last_failure_terminal flag, which the
// scanner worker leaves false when it dead-letters a claim. There is no per-row
// terminal flag to consult.
//
// FailureClass and FailureOperation are the persisted tokens;
// BoundedFailureClassForKind and BoundedFailureOperation bound them before any
// status surface. The failure message and provider status code are deliberately
// absent: they can carry provider response text and must not reach a status
// payload. ScopeID and WorkItemID name the failed work item for audit and are
// never projected onto a status surface. AcknowledgedAt is set only with
// Acknowledged. The operator's acknowledgement reason is never read here.
type TerminalFailure struct {
	CollectorKind    string
	InstanceID       string
	ScopeID          string
	WorkItemID       string
	FailureClass     string
	FailureOperation string
	ObservedAt       time.Time
	Acknowledged     bool
	AcknowledgedAt   time.Time
}

// AcknowledgedFailure is the distinct sub-record an operator-acknowledged
// terminal failure leaves on a RuntimeStatus and a PromotionProof (#8830). It is
// not part of LastFailure*, whose contract is "unresolved and reported": the
// failure is real and still happening, an operator said it is expected, and no
// surface may read it as healthy. FailureClass and FailureOperation are the same
// bounded tokens as LastFailureClass and LastFailureOperation, FailedAt is when
// the failure became terminal, and AcknowledgedAt is when the operator
// acknowledged it. The operator's reason text is not carried: it is admin-only.
type AcknowledgedFailure struct {
	FailureClass     string
	FailureOperation string
	FailedAt         time.Time
	AcknowledgedAt   time.Time
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

// applyTerminalFailures stamps terminal failures onto the coordinator-registered
// runtime row they belong to. A failure without a matching registered row is
// ignored: failure evidence never creates a collector identity. The reader never
// returns a failure for a disabled or deactivated instance, so the disabled guard
// below is defense in depth for other callers: it runs before any write, so a
// disabled row keeps its health and detail and carries no failure fields.
//
// An unacknowledged failure degrades the row and fills LastFailure*. An
// acknowledged failure (#8830) fills only the AcknowledgedFailure sub-record and
// raises health to partial, never healthy: the failure is still happening. The
// higher health rank wins in combineRuntimeHealth, so a real degraded failure on
// another scope of the same instance keeps the row degraded. The row keeps the
// newest failure of each kind when a caller supplies several.
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
		class := BoundedFailureClassForKind(failure.CollectorKind, failure.FailureClass)
		operation := BoundedFailureOperation(failure.FailureOperation)
		if failure.Acknowledged {
			if row.AcknowledgedFailure != nil && !failure.ObservedAt.After(row.AcknowledgedFailure.FailedAt) {
				continue
			}
			row.AcknowledgedFailure = &AcknowledgedFailure{
				FailureClass: class, FailureOperation: operation,
				FailedAt: failure.ObservedAt, AcknowledgedAt: failure.AcknowledgedAt,
			}
			row.Health = combineRuntimeHealth(row.Health, "partial")
			if row.Health != "degraded" {
				row.Detail = acknowledgedFailureDetail(class, operation)
			}
			continue
		}
		if row.LastFailureTerminal && !failure.ObservedAt.After(row.LastFailureAt) {
			continue
		}
		row.LastFailureClass = class
		row.LastFailureOperation = operation
		row.LastFailureTerminal = true
		row.LastFailureAt = failure.ObservedAt
		row.Health = combineRuntimeHealth(row.Health, "degraded")
		row.Detail = terminalFailureDetail(class, operation)
	}
}

// acknowledgedFailureDetail is the operator wording for an acknowledged terminal
// failure, shared by the runtime detail and the promotion blocker.
func acknowledgedFailureDetail(class string, operation string) string {
	return "terminal fetch failure acknowledged: " + class + " (" + operation + ")"
}
