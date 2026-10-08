// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerProducerActivationInstruments registers the #7635 producer
// activation consumer's counters, gauges and histograms on inst. Labels are
// closed sets (status, outcome, reason, domain); no scope or generation id
// is ever a label, the consumer's per-obligation log line carries them.
func registerProducerActivationInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ProducerActivations, err = meter.Int64Gauge(
		"eshu_dp_producer_activations",
		metric.WithDescription("Producer activation obligation rows by status (pending, leased, completed, obsolete, inapplicable), sampled once per consumer cycle"),
	); err != nil {
		return fmt.Errorf("register ProducerActivations gauge: %w", err)
	}
	if inst.ProducerActivationOldestOpenAge, err = meter.Float64Gauge(
		"eshu_dp_producer_activation_oldest_open_age_seconds",
		metric.WithDescription("Age of the oldest pending or leased producer activation obligation, sampled once per consumer cycle"),
		metric.WithUnit("s"),
	); err != nil {
		return fmt.Errorf("register ProducerActivationOldestOpenAge gauge: %w", err)
	}
	if inst.ProducerActivationClaimAge, err = meter.Float64Histogram(
		"eshu_dp_producer_activation_claim_age_seconds",
		metric.WithDescription("Age of a producer activation obligation when a consumer claimed it"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(activationObligationAgeBuckets...),
	); err != nil {
		return fmt.Errorf("register ProducerActivationClaimAge histogram: %w", err)
	}
	if inst.ProducerActivationSettles, err = meter.Int64Counter(
		"eshu_dp_producer_activation_settle_total",
		metric.WithDescription("Producer activation settle attempts by outcome (completed, obsolete, inapplicable, not_owner, missing, lease_lost, error)"),
	); err != nil {
		return fmt.Errorf("register ProducerActivationSettles counter: %w", err)
	}
	if inst.ProducerActivationReopened, err = meter.Int64Counter(
		"eshu_dp_producer_activation_reopened_total",
		metric.WithDescription("Consumer rows reopened by committed producer activation settles, by consumer domain"),
	); err != nil {
		return fmt.Errorf("register ProducerActivationReopened counter: %w", err)
	}
	if inst.ProducerActivationPruned, err = meter.Int64Counter(
		"eshu_dp_producer_activation_pruned_total",
		metric.WithDescription("Finished producer activation obligations deleted by the bounded prune"),
	); err != nil {
		return fmt.Errorf("register ProducerActivationPruned counter: %w", err)
	}
	if inst.ProducerActivationFailures, err = meter.Int64Counter(
		"eshu_dp_producer_activation_failures_total",
		metric.WithDescription("Producer activation consumer step failures by reason (claim, settle, prune, stats); settle_lock_timeout is lock contention logged at Warn"),
	); err != nil {
		return fmt.Errorf("register ProducerActivationFailures counter: %w", err)
	}
	return nil
}
