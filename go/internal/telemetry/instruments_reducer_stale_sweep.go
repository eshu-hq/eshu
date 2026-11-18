// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerReducerStaleSweepInstruments registers the #10863 periodic
// stale-generation sweep signals on inst: rows terminalized per cycle
// (the cleanup volume an operator watches after claim-time supersession
// moved out of the claim), one pass count per cycle, and cycle duration,
// both by the closed outcome set (ok, failed) on the existing outcome
// dimension. No scope, domain, or work-item label: the sweep is a
// whole-queue maintenance statement.
func registerReducerStaleSweepInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ReducerStaleSweptRows, err = meter.Int64Counter(
		"eshu_dp_reducer_stale_generation_swept_rows_total",
		metric.WithDescription("Reducer fact_work_items rows terminalized as superseded by the periodic stale-generation sweep (#10863)"),
	); err != nil {
		return fmt.Errorf("register ReducerStaleSweptRows counter: %w", err)
	}
	if inst.ReducerStaleSweepPasses, err = meter.Int64Counter(
		"eshu_dp_reducer_stale_generation_sweep_passes_total",
		metric.WithDescription("Periodic reducer stale-generation sweep cycles by outcome (ok, failed) (#10863)"),
	); err != nil {
		return fmt.Errorf("register ReducerStaleSweepPasses counter: %w", err)
	}
	if inst.ReducerStaleSweepDuration, err = meter.Float64Histogram(
		"eshu_dp_reducer_stale_generation_sweep_duration_seconds",
		metric.WithDescription("Periodic reducer stale-generation sweep cycle duration in seconds by outcome (ok, failed) (#10863)"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
	); err != nil {
		return fmt.Errorf("register ReducerStaleSweepDuration histogram: %w", err)
	}
	return nil
}
