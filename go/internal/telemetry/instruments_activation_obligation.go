// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// activationObligationMaintenanceBuckets span a no-op or targeted callback
// (milliseconds) to a whole-corpus control-arm pass (minutes).
var activationObligationMaintenanceBuckets = []float64{0.001, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60, 300}

// activationObligationAgeBuckets span an obligation claimed right after its
// Ack to one that waited hours for a consumer.
var activationObligationAgeBuckets = []float64{0.1, 1, 5, 15, 30, 60, 300, 900, 3600, 14400, 86400}

// registerActivationObligationInstruments registers the #7584 activation
// obligation consumer's counters, gauges and histograms on inst. Labels are
// closed sets (status, outcome, reason); no scope or generation id is ever a
// label, the consumer's per-obligation log line carries them.
func registerActivationObligationInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ActivationObligations, err = meter.Int64Gauge(
		"eshu_dp_activation_obligations",
		metric.WithDescription("Activation obligation rows by status (pending, leased, completed, obsolete, inapplicable), sampled once per consumer cycle"),
	); err != nil {
		return fmt.Errorf("register ActivationObligations gauge: %w", err)
	}
	if inst.ActivationObligationOldestOpenAge, err = meter.Float64Gauge(
		"eshu_dp_activation_obligation_oldest_open_age_seconds",
		metric.WithDescription("Age of the oldest pending or leased activation obligation, sampled once per consumer cycle"),
		metric.WithUnit("s"),
	); err != nil {
		return fmt.Errorf("register ActivationObligationOldestOpenAge gauge: %w", err)
	}
	if inst.ActivationObligationClaimAge, err = meter.Float64Histogram(
		"eshu_dp_activation_obligation_claim_age_seconds",
		metric.WithDescription("Age of an activation obligation when a consumer claimed it"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(activationObligationAgeBuckets...),
	); err != nil {
		return fmt.Errorf("register ActivationObligationClaimAge histogram: %w", err)
	}
	if inst.ActivationObligationFinalizes, err = meter.Int64Counter(
		"eshu_dp_activation_obligation_finalize_total",
		metric.WithDescription("Activation obligation finalize attempts by outcome (completed, phase_not_ready, work_pending, obsolete, inapplicable, not_owner, missing, lease_lost, error)"),
	); err != nil {
		return fmt.Errorf("register ActivationObligationFinalizes counter: %w", err)
	}
	if inst.ActivationObligationWoken, err = meter.Int64Counter(
		"eshu_dp_activation_obligation_woken_total",
		metric.WithDescription("deployment_mapping rows made visible by committed activation obligation wakes"),
	); err != nil {
		return fmt.Errorf("register ActivationObligationWoken counter: %w", err)
	}
	if inst.ActivationObligationMaintenanceDuration, err = meter.Float64Histogram(
		"eshu_dp_activation_obligation_maintenance_duration_seconds",
		metric.WithDescription("Activation maintenance callback duration by outcome (success, error); the count is the callback count"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(activationObligationMaintenanceBuckets...),
	); err != nil {
		return fmt.Errorf("register ActivationObligationMaintenanceDuration histogram: %w", err)
	}
	if inst.ActivationObligationCatchUpInserted, err = meter.Int64Counter(
		"eshu_dp_activation_obligation_catch_up_inserted_total",
		metric.WithDescription("Activation obligations owed by catch-up to already-active generations missing their phase"),
	); err != nil {
		return fmt.Errorf("register ActivationObligationCatchUpInserted counter: %w", err)
	}
	if inst.ActivationObligationPruned, err = meter.Int64Counter(
		"eshu_dp_activation_obligation_pruned_total",
		metric.WithDescription("Finished activation obligations deleted by the bounded prune"),
	); err != nil {
		return fmt.Errorf("register ActivationObligationPruned counter: %w", err)
	}
	if inst.ActivationObligationFailures, err = meter.Int64Counter(
		"eshu_dp_activation_obligation_failures_total",
		metric.WithDescription("Activation obligation consumer step failures by reason (claim, finalize, maintenance, maintenance_timeout, catalog_changed, no_memo_baseline, closure_too_deep, catch_up, prune, stats); the three hold reasons are held refusals, not errors"),
	); err != nil {
		return fmt.Errorf("register ActivationObligationFailures counter: %w", err)
	}
	return nil
}
