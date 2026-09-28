// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Bucket boundaries of the changed-since link histograms (#7127 PR-3a). A
// link of the largest repository takes about 13 s warm on ops-qa and is
// bounded by a 120 s statement timeout; delta rows and keys span a
// one-file change to a 1.3M-key scope.
var (
	changedSinceLinkDurationBuckets = []float64{0.01, 0.05, 0.1, 0.5, 1, 2.5, 5, 10, 20, 30, 60, 120}
	changedSinceLinkRowBuckets      = []float64{0, 1, 10, 100, 1000, 10000, 100000, 1000000}
	changedSinceLinkKeyBuckets      = []float64{10, 100, 1000, 10000, 100000, 500000, 1000000, 2000000}
)

// registerChangedSinceLinkInstruments registers the changed-since link
// writer's counters, gauges and histograms on inst.
func registerChangedSinceLinkInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.GenerationRetentionOverLimitBatches, err = meter.Int64Counter(
		"eshu_dp_generation_retention_over_limit_batches_total",
		metric.WithDescription("Generation retention batches of one generation admitted over BatchRowLimit by its changed-since ledger rows"),
	); err != nil {
		return fmt.Errorf("register GenerationRetentionOverLimitBatches counter: %w", err)
	}
	if inst.ChangedSinceLinks, err = meter.Int64Counter(
		"eshu_dp_changed_since_links_total",
		metric.WithDescription("Changed-since link attempts by link_kind and outcome"),
	); err != nil {
		return fmt.Errorf("register ChangedSinceLinks counter: %w", err)
	}
	if inst.ChangedSinceLinkRetries, err = meter.Int64Counter(
		"eshu_dp_changed_since_link_retries_total",
		metric.WithDescription("Non-counting changed-since link outcomes by reason"),
	); err != nil {
		return fmt.Errorf("register ChangedSinceLinkRetries counter: %w", err)
	}
	if inst.ChangedSinceLinkFailures, err = meter.Int64Counter(
		"eshu_dp_changed_since_link_failures_total",
		metric.WithDescription("Counting changed-since link failures by failure_class"),
	); err != nil {
		return fmt.Errorf("register ChangedSinceLinkFailures counter: %w", err)
	}
	if inst.ChangedSinceLinkRetryingScopes, err = meter.Int64Gauge(
		"eshu_dp_changed_since_link_retrying_scopes",
		metric.WithDescription("Scopes whose head changed-since activation has a counted failure pending"),
	); err != nil {
		return fmt.Errorf("register ChangedSinceLinkRetryingScopes gauge: %w", err)
	}
	if inst.ChangedSinceLinkPoisonedScopes, err = meter.Int64Gauge(
		"eshu_dp_changed_since_link_poisoned_scopes",
		metric.WithDescription("Scopes carrying the link_poisoned marker until their next full link"),
	); err != nil {
		return fmt.Errorf("register ChangedSinceLinkPoisonedScopes gauge: %w", err)
	}
	if inst.ChangedSinceChainBreaks, err = meter.Int64Counter(
		"eshu_dp_changed_since_chain_breaks_total",
		metric.WithDescription("Changed-since activations advanced without a link, by reason"),
	); err != nil {
		return fmt.Errorf("register ChangedSinceChainBreaks counter: %w", err)
	}
	if inst.ChangedSinceLinkBacklog, err = meter.Int64Gauge(
		"eshu_dp_changed_since_link_backlog",
		metric.WithDescription("Changed-since activation rows above their scope cursor"),
	); err != nil {
		return fmt.Errorf("register ChangedSinceLinkBacklog gauge: %w", err)
	}
	if inst.ChangedSinceLinkLag, err = meter.Float64Gauge(
		"eshu_dp_changed_since_link_lag_seconds",
		metric.WithDescription("Age of the oldest changed-since activation above its scope cursor"),
		metric.WithUnit("s"),
	); err != nil {
		return fmt.Errorf("register ChangedSinceLinkLag gauge: %w", err)
	}
	if inst.ChangedSinceStateBytes, err = meter.Int64Gauge(
		"eshu_dp_changed_since_state_bytes",
		metric.WithDescription("Total size of the changed-since key-state table"),
		metric.WithUnit("By"),
	); err != nil {
		return fmt.Errorf("register ChangedSinceStateBytes gauge: %w", err)
	}
	if inst.ChangedSinceStateRows, err = meter.Int64Gauge(
		"eshu_dp_changed_since_state_rows",
		metric.WithDescription("Planner row estimate of the changed-since key-state table"),
	); err != nil {
		return fmt.Errorf("register ChangedSinceStateRows gauge: %w", err)
	}
	if inst.ChangedSinceDeltasBytes, err = meter.Int64Gauge(
		"eshu_dp_changed_since_deltas_bytes",
		metric.WithDescription("Total size of the changed-since link-delta table"),
		metric.WithUnit("By"),
	); err != nil {
		return fmt.Errorf("register ChangedSinceDeltasBytes gauge: %w", err)
	}
	if inst.ChangedSinceDeltasRows, err = meter.Int64Gauge(
		"eshu_dp_changed_since_deltas_rows",
		metric.WithDescription("Planner row estimate of the changed-since link-delta table"),
	); err != nil {
		return fmt.Errorf("register ChangedSinceDeltasRows gauge: %w", err)
	}
	if inst.ChangedSinceLedgerOrphans, err = meter.Int64Gauge(
		"eshu_dp_changed_since_ledger_orphans",
		metric.WithDescription("Changed-since ledger rows naming a pruned generation, or bucket counts with no link, by kind"),
	); err != nil {
		return fmt.Errorf("register ChangedSinceLedgerOrphans gauge: %w", err)
	}
	if inst.ChangedSinceLinkDuration, err = meter.Float64Histogram(
		"eshu_dp_changed_since_link_duration_seconds",
		metric.WithDescription("Committed changed-since link transaction duration by link_kind"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(changedSinceLinkDurationBuckets...),
	); err != nil {
		return fmt.Errorf("register ChangedSinceLinkDuration histogram: %w", err)
	}
	if inst.ChangedSinceLinkDeltaRows, err = meter.Int64Histogram(
		"eshu_dp_changed_since_link_delta_rows",
		metric.WithDescription("Link delta rows written by one changed-since link, by link_kind"),
		metric.WithExplicitBucketBoundaries(changedSinceLinkRowBuckets...),
	); err != nil {
		return fmt.Errorf("register ChangedSinceLinkDeltaRows histogram: %w", err)
	}
	if inst.ChangedSinceLinkKeys, err = meter.Int64Histogram(
		"eshu_dp_changed_since_link_keys",
		metric.WithDescription("Effective keys of the generation one changed-since link reached, by link_kind"),
		metric.WithExplicitBucketBoundaries(changedSinceLinkKeyBuckets...),
	); err != nil {
		return fmt.Errorf("register ChangedSinceLinkKeys histogram: %w", err)
	}
	return nil
}
