// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// SpanReducerStatusSummaryPass wraps one pass of the reducer's periodic
// status summary writer (#7009): the advisory try-lock, the active-work
// statement at the database clock, and the guarded single-row upsert.
const SpanReducerStatusSummaryPass = "reducer.status_summary.pass"

// MetricDimensionModelKey labels the status summary writer metrics with the
// read model the writer maintains. The value set is closed: one key per
// model (active_work_summary today).
const MetricDimensionModelKey = "model_key"

// statusSummaryWriterPassBuckets span a lock skip (sub-millisecond) to a pass
// at its two-interval deadline. The #7009 fixture measured 300-440 ms alone
// and 600-640 ms beside the claim loop, and the ops-qa read replica about
// 1.0 s; a pass above the writer interval (10 s by default) is an overrun.
var statusSummaryWriterPassBuckets = []float64{0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30}

// AttrModelKey returns a model_key attribute naming a status summary model.
func AttrModelKey(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionModelKey, v)
}

// registerStatusSummaryWriterInstruments registers the status summary
// writer's pass counter and duration histogram, overrun counter, and up gauge
// on inst.
func registerStatusSummaryWriterInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.StatusSummaryWriterPasses, err = meter.Int64Counter(
		"eshu_dp_status_summary_writer_passes_total",
		metric.WithDescription("Status summary writer passes by model_key and outcome (ok, skipped_lock, skipped_missing_table, rejected_guard, error)"),
	); err != nil {
		return fmt.Errorf("register StatusSummaryWriterPasses counter: %w", err)
	}
	if inst.StatusSummaryWriterPassDuration, err = meter.Float64Histogram(
		"eshu_dp_status_summary_writer_pass_duration_seconds",
		metric.WithDescription("Duration of one status summary writer pass transaction by model_key and outcome"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(statusSummaryWriterPassBuckets...),
	); err != nil {
		return fmt.Errorf("register StatusSummaryWriterPassDuration histogram: %w", err)
	}
	if inst.StatusSummaryWriterOverruns, err = meter.Int64Counter(
		"eshu_dp_status_summary_writer_overrun_total",
		metric.WithDescription("Status summary writer passes that took longer than the writer interval, by model_key"),
	); err != nil {
		return fmt.Errorf("register StatusSummaryWriterOverruns counter: %w", err)
	}
	if inst.StatusSummaryWriterUp, err = meter.Int64Gauge(
		"eshu_dp_status_summary_writer_up",
		metric.WithDescription("1 while this reducer's status summary writer loop runs, 0 after it stops, by model_key"),
	); err != nil {
		return fmt.Errorf("register StatusSummaryWriterUp gauge: %w", err)
	}
	return nil
}
