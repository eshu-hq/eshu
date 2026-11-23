// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricDimensionWaitingWakeSite labels eshu_dp_reducer_waiting_wake_total
// with where the #10876 waiting wake ran. The value set is closed:
// in-tx provider saves, the audit, the #11104 exact-phase wake inside and
// after the phase publish, and the #11228 projector Ack re-wake after its
// commit. On eshu_dp_reducer_waiting_fallback_resolved_total it carries the
// reason's registered wake site from the waiting registry instead (in_tx,
// completion_event, audit, or unregistered), also a closed set (#11228).
const MetricDimensionWaitingWakeSite = "wake_site"

// Waiting wake sites (#10876).
const (
	// WaitingWakeSiteInTx is a wake inside a providing save's transaction.
	WaitingWakeSiteInTx = "in_tx"
	// WaitingWakeSiteAudit is the 5-minute slow audit reopening a wait whose
	// provider already committed.
	WaitingWakeSiteAudit = "audit"
	// WaitingWakeSitePhaseExactInTx is the exact-key phase_committed wake
	// inside the phase publish's transaction (#11104): it reopens waits on
	// one generation's own (keyspace, phase) row, today the cross-repo
	// backward-evidence deferral.
	WaitingWakeSitePhaseExactInTx = "phase_exact_in_tx"
	// WaitingWakeSitePhaseExactPostCommit is the exact-key re-wake after the
	// phase publish committed (#11104), covering a waiter that parked inside
	// the publisher's open window.
	WaitingWakeSitePhaseExactPostCommit = "phase_exact_post_commit"
	// WaitingWakeSiteAckPostCommit is the projector Ack's re-wake after its
	// transaction committed (#11228), covering a waiter that parked inside
	// the Ack's open window: the generation_active, producer-terminal, and
	// terminal-generation wakes run again on the pool connection.
	WaitingWakeSiteAckPostCommit = "ack_post_commit"
)

// Waiting wake outcomes (#10876), recorded on the existing outcome dimension.
const (
	// WaitingWakeOutcomeReopened means the wake statement reopened at least
	// one parked queue row.
	WaitingWakeOutcomeReopened = "reopened"
	// WaitingWakeOutcomeNoop means the wake statement ran and reopened nobody.
	WaitingWakeOutcomeNoop = "noop"
	// WaitingWakeOutcomeError means the wake statement failed; the providing
	// save rolls back (in-tx), the post-commit caller logs and keeps the
	// committed outcome (#11016), or the audit logs and retries next cycle.
	WaitingWakeOutcomeError = "error"
)

// Outcomes of eshu_dp_reducer_waiting_fallback_resolved_total (#11228), the
// count of parked waits a fallback resolved instead of a wake. The
// audit-reopened outcome reuses WaitingWakeOutcomeReopened.
const (
	// WaitingAuditOutcomeReconciled labels a wait whose queue row had
	// already left the parked state through another path (a completion
	// fanout, a replay, a supersede sweep, a wake on a sibling key, or the
	// backstop claim counted earlier as WaitingResolvedOutcomeBackstop), so
	// the audit only dropped its wait row.
	WaitingAuditOutcomeReconciled = "reconciled"
	// WaitingResolvedOutcomeBackstop labels a wait the reducer claimed at
	// its 10-minute backstop with its wait row still present: no wake and
	// no audit reopened it in time.
	WaitingResolvedOutcomeBackstop = "backstop"
)

// AttrWaitingWakeSite returns a wake_site attribute. v must be one of the
// WaitingWakeSite* constants.
func AttrWaitingWakeSite(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionWaitingWakeSite, v)
}

// registerReducerWaitingInstruments registers the #10876 waiting set-aside
// signals on inst: one counter per wake statement by site and outcome, one
// counter for audit reopens that an earlier pass should have caught
// (missed wakes), one counter of waits a fallback (the audit or the
// backstop) resolved instead of a wake, by reason, the reason's registered
// wake site, and outcome (#11228), and two gauges the audit records per reason
// (failure_class): parked-wait count and oldest-wait age in seconds. The
// gauges have no callback: the audit is the only writer and it already
// holds the database handle the counts come from.
func registerReducerWaitingInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ReducerWaitingWake, err = meter.Int64Counter(
		"eshu_dp_reducer_waiting_wake_total",
		metric.WithDescription("Reducer waiting-set-aside wake statements by site (in_tx: inside a providing save's transaction; audit: the 5-minute slow audit; phase_exact_in_tx and phase_exact_post_commit: the #11104 exact-phase wake inside and after the phase publish; ack_post_commit: the #11228 projector Ack re-wake after its commit) and outcome (reopened, noop, error) (#10876)"),
	); err != nil {
		return fmt.Errorf("register ReducerWaitingWake counter: %w", err)
	}
	if inst.ReducerWaitingMissedWake, err = meter.Int64Counter(
		"eshu_dp_reducer_waiting_missed_wake_total",
		metric.WithDescription("Reducer waits the audit reopened a full audit interval after they parked, meaning an earlier pass should have caught them (#10876)"),
	); err != nil {
		return fmt.Errorf("register ReducerWaitingMissedWake counter: %w", err)
	}
	if inst.ReducerWaitingKeyBounded, err = meter.Int64Counter(
		"eshu_dp_reducer_waiting_key_bounded_total",
		metric.WithDescription("Narrowed corpus waits parked with their blocking or read scope set cut to the bounded key budget, so the waits_for_key fits the reducer_waiting_items primary-key btree row limit. A bounded wait still parks and wakes on the retained scopes; the 10-minute backstop and the audit cover the rest (#11297)"),
	); err != nil {
		return fmt.Errorf("register ReducerWaitingKeyBounded counter: %w", err)
	}
	if inst.ReducerWaitingFallbackResolved, err = meter.Int64Counter(
		"eshu_dp_reducer_waiting_fallback_resolved_total",
		metric.WithDescription("Parked reducer waits a fallback resolved instead of a wake, by reason (failure_class), the reason's registered wake site (wake_site: in_tx, completion_event, audit, or unregistered) and outcome (reopened: the slow audit reopened a satisfied wait; backstop: the reducer claimed the row at its 10-minute backstop with its wait row still present; reconciled: the queue row had already left the parked state and the audit dropped its wait row). A reopened or backstop count on a reason whose wake_site is not audit is a lost wake (#11228)"),
	); err != nil {
		return fmt.Errorf("register ReducerWaitingFallbackResolved counter: %w", err)
	}
	if inst.ReducerWaitingCount, err = meter.Int64Gauge(
		"eshu_dp_reducer_waiting_items",
		metric.WithDescription("Parked reducer waits by reason (failure_class), recorded by the waiting audit each cycle (#10876)"),
	); err != nil {
		return fmt.Errorf("register ReducerWaitingCount gauge: %w", err)
	}
	if inst.ReducerWaitingOldestAge, err = meter.Int64Gauge(
		"eshu_dp_reducer_waiting_oldest_age_seconds",
		metric.WithDescription("Age in seconds of the oldest parked reducer wait by reason (failure_class), recorded by the waiting audit each cycle (#10876)"),
	); err != nil {
		return fmt.Errorf("register ReducerWaitingOldestAge gauge: %w", err)
	}
	return nil
}

// registerReducerWaiting appends the waiting wake-site dimension (#10876).
// reason is already in the root registry.
func registerReducerWaiting() {
	metricDimensionKeys = append(metricDimensionKeys, MetricDimensionWaitingWakeSite)
}
