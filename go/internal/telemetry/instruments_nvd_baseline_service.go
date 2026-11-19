// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Closed NVD baseline drain phases a chunk can wait in (eshu-hq/eshu#10746):
// every planner drain phase but drained. The not-done age gauge reports the
// oldest waiting chunk's age under its phase, so the operator sees how long
// the pipeline's head has waited and where it waits.
const (
	// NVDBaselineDrainPhaseClaimOpen: the chunk's collector claim is not
	// closed.
	NVDBaselineDrainPhaseClaimOpen = "claim_open"
	// NVDBaselineDrainPhaseProjecting: the chunk committed but its
	// generation is not active yet.
	NVDBaselineDrainPhaseProjecting = "projecting"
	// NVDBaselineDrainPhaseReducing: the generation is active but its
	// supply-chain impact reducer item has not succeeded.
	NVDBaselineDrainPhaseReducing = "reducing"
	// NVDBaselineDrainPhaseFailed: the chunk's projector or reducer item
	// failed terminally, or its scope has no active or pending generation
	// and its newest generation failed.
	NVDBaselineDrainPhaseFailed = "failed"
)

// nvdBaselineDrainNotDonePhases is the ordered, closed set of phases the
// not-done age gauge can report. A parity test in the coordinator keeps it
// equal to the planner's non-drained phases.
var nvdBaselineDrainNotDonePhases = [...]string{
	NVDBaselineDrainPhaseClaimOpen,
	NVDBaselineDrainPhaseProjecting,
	NVDBaselineDrainPhaseReducing,
	NVDBaselineDrainPhaseFailed,
}

// NVDBaselineDrainNotDonePhases returns the ordered, closed set of drain
// phases the not-done age gauge reports.
func NVDBaselineDrainNotDonePhases() []string {
	return slices.Clone(nvdBaselineDrainNotDonePhases[:])
}

// RecordNVDBaselineETA records the estimated seconds until the baseline run
// commits its remaining records at the sampler's trailing six-hour rate
// (eshu-hq/eshu#10746): remaining records over that rate, started years
// only. A non-positive value records nothing, so a run with no computable
// rate keeps its last estimate instead of a zero no history produced. Nil
// instruments or a nil gauge skip the signal.
func (inst *Instruments) RecordNVDBaselineETA(ctx context.Context, seconds float64) {
	if inst == nil || inst.NVDBaselineETA == nil || seconds <= 0 {
		return
	}
	inst.NVDBaselineETA.Record(ctx, seconds)
}

// RecordNVDBaselineOldestNotDoneAge records the age in seconds of the run's
// oldest not-done chunk next to the phase it waits in (eshu-hq/eshu#10746).
// A phase outside the closed set counts as other, so a renamed planner
// phase never becomes an unbounded label. A negative age records as zero.
// Nil instruments or a nil gauge skip the signal.
func (inst *Instruments) RecordNVDBaselineOldestNotDoneAge(ctx context.Context, phase string, ageSeconds float64) {
	if inst == nil || inst.NVDBaselineOldestNotDoneAge == nil {
		return
	}
	if !slices.Contains(nvdBaselineDrainNotDonePhases[:], phase) {
		phase = NVDBaselineOutcomeOther
	}
	if ageSeconds < 0 {
		ageSeconds = 0
	}
	inst.NVDBaselineOldestNotDoneAge.Record(ctx, ageSeconds, metric.WithAttributes(attribute.String("phase", phase)))
}

// RecordNVDBaselineDrainProbeTimeouts counts the drain probe reads that hit
// their statement timeout or context deadline on one plan tick
// (eshu-hq/eshu#10746): the years the service could not prove drained. A
// non-positive count records nothing. Nil instruments or a nil counter skip
// the signal.
func (inst *Instruments) RecordNVDBaselineDrainProbeTimeouts(ctx context.Context, count int) {
	if inst == nil || inst.NVDBaselineDrainProbeTimeouts == nil || count <= 0 {
		return
	}
	inst.NVDBaselineDrainProbeTimeouts.Add(ctx, int64(count))
}

// registerNVDBaselineService registers the #10746 service-side baseline
// signals: the run-completion ETA gauge, the oldest not-done chunk age gauge
// by phase, and the drain probe timeout counter. Labels stay closed and
// bounded: no scope, stamp, year, or run value ever becomes a label.
func registerNVDBaselineService(meter metric.Meter, inst *Instruments) error {
	var err error
	inst.NVDBaselineETA, err = meter.Float64Gauge(
		"eshu_dp_nvd_baseline_eta_seconds",
		metric.WithDescription("Estimated seconds until the NVD baseline run commits its remaining records at the sampler's trailing six-hour rate (remaining records over that rate, started years only; #10746)"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineETA gauge: %w", err)
	}
	inst.NVDBaselineOldestNotDoneAge, err = meter.Float64Gauge(
		"eshu_dp_nvd_baseline_oldest_not_done_age_seconds",
		metric.WithDescription("Age in seconds of the NVD baseline run's oldest not-done chunk by drain phase (the commit time of the generation the chunk waits on; #10746)"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineOldestNotDoneAge gauge: %w", err)
	}
	inst.NVDBaselineDrainProbeTimeouts, err = meter.Int64Counter(
		"eshu_dp_nvd_baseline_drain_probe_timeouts_total",
		metric.WithDescription("Total NVD baseline drain probe reads that hit their statement timeout or context deadline, by plan tick (#10746): the years the service could not prove drained and the planner holds as baseline_drain_unloaded"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineDrainProbeTimeouts counter: %w", err)
	}
	return nil
}
