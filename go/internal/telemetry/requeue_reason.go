// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

// RequeueReason names why a reducer-stage fact_work_items row returned to an
// outstanding state (#11615). The vocabulary is closed: every reducer-stage
// requeue writer must pass one of these values, and RequeueReason.Validate
// rejects anything else so a new writer cannot silently invent a label. The
// set is intentionally writer-class shaped (which code path requeued the row),
// not cause shaped: the fine detail stays in failure_class and the payload,
// while the reason answers "which part of the system keeps re-pending work".
//
// Lifecycle: the reason is set on every transition that returns a row to an
// outstanding, claimable state from a non-outstanding one — into pending or
// retrying from a finished or in-flight state, on replay-debt scheduling for
// claimed or running rows (counted once, at schedule time), and on a waiting
// wake, where a parked retrying row leaves the waiting set-aside (wait row
// deleted, visible_at advanced) without a status change. A touch on a row
// already outstanding and already scheduled to run — a retouch or schedule
// attempt on a pending or retrying row, or on a claimed/running row already
// carrying replay debt — is not a requeue: it creates no execution, changes
// neither claimability nor queue position, and coalesces into the scheduled
// run, so the row keeps that run's reason and the touch is not counted, even
// where it advances reopened_at to move that run's cycle bound. Claim,
// heartbeat, ack-success, dead_letter, and supersede never clear it, so
// dead-letter triage can read why the row last ran. A row that was never
// requeued keeps NULL, which readers display as
// RequeueReasonLegacyUnclassified. That display label and "unknown" are
// forbidden writer values: an unknown bucket written by design would recreate
// the unattributed remainder this issue exists to eliminate.
type RequeueReason string

const (
	// RequeueReasonRetryBackoff marks a counting Fail to retrying: the handler
	// failed on a retryable cause and consumed one attempt-budget retry.
	RequeueReasonRetryBackoff RequeueReason = "retry_backoff"
	// RequeueReasonReadinessWait marks a non-counting Fail to retrying that
	// parks or waits (a *_not_ready or other non-counting failure class). The
	// row retries without consuming attempt budget; the detail stays in
	// failure_class.
	RequeueReasonReadinessWait RequeueReason = "readiness_wait"
	// RequeueReasonWaitingWake marks a waiting set-aside wake: a parked row
	// whose wait condition resolved (narrow wakes and the winners-watermark
	// wake alike) returned to retrying.
	RequeueReasonWaitingWake RequeueReason = "waiting_wake"
	// RequeueReasonMaintenanceReopen marks the deferred-maintenance reopen
	// pass, including breaker-surviving reopens and the targeted/floor
	// backfill paths. The bootstrap reopen of succeeded rows uses the same
	// value: both are operator-schedule-driven replays, not new evidence.
	RequeueReasonMaintenanceReopen RequeueReason = "maintenance_reopen"
	// RequeueReasonProducerActivationSettle marks a producer-activation settle
	// reopen: a row re-pended because its producer domain (re)activated.
	RequeueReasonProducerActivationSettle RequeueReason = "producer_activation_settle"
	// RequeueReasonCompletionWake marks a cross-scope completion fanout wake:
	// a producer domain completed and fanned waiting consumers back to work.
	RequeueReasonCompletionWake RequeueReason = "completion_wake"
	// RequeueReasonCrossScopeReplay marks replay-debt scheduling (workload
	// materialization, Crossplane redrives, claimed/running rows flagged for
	// one rerun) and the database ACK-trigger conversion it later causes.
	// The conversion itself is counted at schedule time, never double-counted
	// at conversion.
	RequeueReasonCrossScopeReplay RequeueReason = "cross_scope_replay"
	// RequeueReasonFanoutHolder marks the supply-chain holder fanout reopen:
	// new impact evidence re-pended a holder's supply_chain_impact item.
	RequeueReasonFanoutHolder RequeueReason = "fanout_holder"
	// RequeueReasonRetouchNewEvidence marks a retouch and the service-catalog
	// re-trigger: a same-generation touch reopened a finished row or newly
	// flagged an in-flight row for one rerun. Touches on already-outstanding
	// rows coalesce into the scheduled run without stamping.
	RequeueReasonRetouchNewEvidence RequeueReason = "retouch_new_evidence"
	// RequeueReasonAdminReplay marks an operator replay of a dead-lettered or
	// failed row back to pending.
	RequeueReasonAdminReplay RequeueReason = "admin_replay"
	// RequeueReasonAdminReopen marks an operator reopen of a succeeded row
	// back to pending.
	RequeueReasonAdminReopen RequeueReason = "admin_reopen"
	// RequeueReasonRecoveryRedrive marks a generation-liveness re-drive,
	// zombie heal, reconcile re-projection, or refinalize re-enqueue: the
	// recovery plane re-drove an outstanding or finished row.
	RequeueReasonRecoveryRedrive RequeueReason = "recovery_redrive"
)

// RequeueReasonLegacyUnclassified is the read-side display label for a NULL
// last_requeue_reason: a row that was never requeued (or that predates the
// #11615 column). It is not a valid writer value; passing it to Validate
// fails so no writer can stamp the unattributed remainder by design.
const RequeueReasonLegacyUnclassified = "legacy_unclassified"

// allRequeueReasons is the closed writer vocabulary, in stable declaration
// order. Freeze discipline (#11615 decision 7): a value is added here only
// with its censused writer wired in the same PR.
var allRequeueReasons = []RequeueReason{
	RequeueReasonRetryBackoff,
	RequeueReasonReadinessWait,
	RequeueReasonWaitingWake,
	RequeueReasonMaintenanceReopen,
	RequeueReasonProducerActivationSettle,
	RequeueReasonCompletionWake,
	RequeueReasonCrossScopeReplay,
	RequeueReasonFanoutHolder,
	RequeueReasonRetouchNewEvidence,
	RequeueReasonAdminReplay,
	RequeueReasonAdminReopen,
	RequeueReasonRecoveryRedrive,
}

// AllRequeueReasons returns the closed writer vocabulary in stable order.
func AllRequeueReasons() []RequeueReason {
	out := make([]RequeueReason, len(allRequeueReasons))
	copy(out, allRequeueReasons)
	return out
}

// Validate reports whether r is a legal writer value: one of the closed
// vocabulary. The zero value, "unknown", and the legacy_unclassified display
// label are all rejected.
func (r RequeueReason) Validate() error {
	for _, known := range allRequeueReasons {
		if r == known {
			return nil
		}
	}
	return fmt.Errorf("invalid requeue reason %q: want one of the closed #11615 vocabulary", string(r))
}

// SQLLiteral returns r as a quoted SQL string literal for embedding in a
// requeue UPDATE's SET list. The literal is safe to embed because Validate
// restricts values to lowercase [a-z_]; callers must validate first.
// SQLLiteral panics on an invalid reason so a bad call site fails loudly in
// tests rather than writing a corrupt label.
func (r RequeueReason) SQLLiteral() string {
	if err := r.Validate(); err != nil {
		panic(err)
	}
	if strings.TrimSpace(string(r)) != string(r) {
		panic(fmt.Sprintf("requeue reason %q carries whitespace", string(r)))
	}
	return "'" + string(r) + "'"
}

// AttrRequeueReason returns a reason attribute for the per-reason requeue
// counter and gauges. r must be a valid writer value.
func AttrRequeueReason(r RequeueReason) attribute.KeyValue {
	return attribute.String(MetricDimensionReason, string(r))
}

// DisplayRequeueReason maps a nullable stored reason to its operator-facing
// label: the reason itself, or legacy_unclassified for NULL (a row that was
// never requeued or that predates the column).
func DisplayRequeueReason(stored *string) string {
	if stored == nil || *stored == "" {
		return RequeueReasonLegacyUnclassified
	}
	return *stored
}
