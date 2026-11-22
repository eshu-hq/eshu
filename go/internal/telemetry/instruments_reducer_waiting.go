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
// in-tx provider saves, the audit, the #11104 exact-phase wake inside
// and after the phase publish, and the #11256 park rendezvous.
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
	// WaitingWakeSiteParkRendezvous is a generation-activation wait that
	// found an activation of its provider scope in flight while parking
	// (#11256). The park stood behind that activation's commit before its
	// satisfaction check, so the activation's wake could not miss it. Outcomes
	// are WaitingWakeOutcomeWaited and WaitingWakeOutcomeTimeout; an
	// uncontended park records nothing.
	WaitingWakeSiteParkRendezvous = "park_rendezvous"
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
	// WaitingWakeOutcomeWaited means a parking wait stood behind an in-flight
	// activation of its provider scope and proceeded after it committed.
	WaitingWakeOutcomeWaited = "waited"
	// WaitingWakeOutcomeTimeout means the park stopped waiting for the
	// activation at its bound and parked as before; the audit and the backstop
	// bound the wake.
	WaitingWakeOutcomeTimeout = "timeout"
)

// AttrWaitingWakeSite returns a wake_site attribute. v must be one of the
// WaitingWakeSite* constants.
func AttrWaitingWakeSite(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionWaitingWakeSite, v)
}

// registerReducerWaitingInstruments registers the #10876 waiting set-aside
// signals on inst: one counter per wake statement by site and outcome, one
// counter for audit reopens that an earlier pass should have caught
// (missed wakes), and two gauges the audit records per reason
// (failure_class): parked-wait count and oldest-wait age in seconds. The
// gauges have no callback: the audit is the only writer and it already
// holds the database handle the counts come from.
func registerReducerWaitingInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ReducerWaitingWake, err = meter.Int64Counter(
		"eshu_dp_reducer_waiting_wake_total",
		metric.WithDescription("Reducer waiting-set-aside wake statements by site (in_tx: inside a providing save's transaction; audit: the 5-minute slow audit; phase_exact_in_tx and phase_exact_post_commit: the #11104 exact-phase wake inside and after the phase publish; park_rendezvous: a generation-activation wait that stood behind an in-flight activation of its provider while parking, #11256) and outcome (reopened, noop, error; waited, timeout for park_rendezvous) (#10876)"),
	); err != nil {
		return fmt.Errorf("register ReducerWaitingWake counter: %w", err)
	}
	if inst.ReducerWaitingMissedWake, err = meter.Int64Counter(
		"eshu_dp_reducer_waiting_missed_wake_total",
		metric.WithDescription("Reducer waits the audit reopened a full audit interval after they parked, meaning an earlier pass should have caught them (#10876)"),
	); err != nil {
		return fmt.Errorf("register ReducerWaitingMissedWake counter: %w", err)
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
