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

// Closed NVD baseline outcomes (eshu-hq/eshu#10728). The advance set is
// the outcome label of eshu_dp_nvd_baseline_advance_total, recorded once
// per decided baseline generation by the host commit policy; the plan set
// is the outcome label of eshu_dp_nvd_baseline_plans_total, recorded once
// per plan call by the coordinator derivation; the trip set is the
// transition label of eshu_dp_nvd_baseline_trip_transitions_total.
const (
	// NVDBaselineOutcomeAdvanced: the chunk advanced the checkpoint in its
	// commit transaction.
	NVDBaselineOutcomeAdvanced = "advanced"
	// NVDBaselineOutcomeHeld: no verdict or a hold verdict — facts commit,
	// the checkpoint holds, the next plan replays the chunk.
	NVDBaselineOutcomeHeld = "held"
	// NVDBaselineOutcomeNotCovered: the stamped identity covers nothing new
	// (blank or malformed seed sha, an advance behind the chunk start, or
	// an incomplete advance that does not move past it) — facts commit,
	// the checkpoint holds.
	NVDBaselineOutcomeNotCovered = "not_covered"
	// NVDBaselineOutcomeRefused: the generation would have armed the window
	// cursor writer — the claim fails retryable and nothing commits.
	NVDBaselineOutcomeRefused = "refused"
	// NVDBaselineOutcomePlanned: the derivation minted a chunk this tick.
	NVDBaselineOutcomePlanned = "planned"
	// NVDBaselineOutcomeComplete: every seed year reads complete.
	NVDBaselineOutcomeComplete = "complete"
	// NVDBaselineOutcomeOther counts an outcome outside this set.
	NVDBaselineOutcomeOther = "other"
)

// NVDBaselineTripTransitions is the closed transition set of the baseline
// trip line (eshu-hq/eshu#10728, ruling decision 3).
const (
	// NVDBaselineTripOpened: the rate latch opened the trip.
	NVDBaselineTripOpened = "opened"
	// NVDBaselineTripPaged: the oldest-dirty age paged the operator.
	NVDBaselineTripPaged = "paged"
	// NVDBaselineTripEligible: six quiet hourly samples since the trip opened
	// marked the lane safe to re-enable.
	NVDBaselineTripEligible = "eligible"
	// NVDBaselineTripClosed: the operator closed the trip on re-enable.
	NVDBaselineTripClosed = "closed"
	// NVDBaselineTripCloseRefused: the operator tried to close the trip
	// before six quiet hourly samples had followed it opening
	// (eshu-hq/eshu#10760); the trip stays open.
	NVDBaselineTripCloseRefused = "close_refused"
)

var nvdBaselineOutcomes = [...]string{
	NVDBaselineOutcomeAdvanced,
	NVDBaselineOutcomeHeld,
	NVDBaselineOutcomeNotCovered,
	NVDBaselineOutcomeRefused,
	NVDBaselineOutcomePlanned,
	NVDBaselineOutcomeComplete,
	NVDBaselineOutcomeOther,
}

var nvdBaselineTripTransitions = [...]string{
	NVDBaselineTripOpened,
	NVDBaselineTripPaged,
	NVDBaselineTripEligible,
	NVDBaselineTripClosed,
	NVDBaselineTripCloseRefused,
}

// NVDBaselineOutcomes returns the closed, ordered baseline outcome set.
func NVDBaselineOutcomes() []string {
	return slices.Clone(nvdBaselineOutcomes[:])
}

// NVDBaselineOutcome maps outcome onto the closed set.
func NVDBaselineOutcome(outcome string) string {
	if slices.Contains(nvdBaselineOutcomes[:], outcome) {
		return outcome
	}
	return NVDBaselineOutcomeOther
}

// NVDBaselineTripTransitions returns the closed trip transition set.
func NVDBaselineTripTransitions() []string {
	return slices.Clone(nvdBaselineTripTransitions[:])
}

// RecordNVDBaselineAdvance counts one baseline commit-policy decision.
// Labels stay outcome-only: no run, year, scope, stamp, or sha ever
// becomes a label. Nil instruments or a nil meter skip the signal.
func (inst *Instruments) RecordNVDBaselineAdvance(ctx context.Context, outcome string) {
	if inst == nil || inst.NVDBaselineAdvances == nil {
		return
	}
	inst.NVDBaselineAdvances.Add(ctx, 1, metric.WithAttributes(AttrOutcome(NVDBaselineOutcome(outcome))))
}

// RecordNVDBaselinePlan counts one baseline derivation verdict per plan
// call: planned, held (any hold reason), or complete. Labels stay
// outcome-only; the hold reason rides the span, never a label. Nil
// instruments or a nil meter skip the signal.
func (inst *Instruments) RecordNVDBaselinePlan(ctx context.Context, outcome string) {
	if inst == nil || inst.NVDBaselinePlans == nil {
		return
	}
	inst.NVDBaselinePlans.Add(ctx, 1, metric.WithAttributes(AttrOutcome(NVDBaselineOutcome(outcome))))
}

// RecordNVDBaselineTripTransition counts one trip-line transition:
// opened, paged, eligible, closed, or close_refused (an early close the
// trip line refused). Labels stay transition-only; the
// rate and age ride the span, never a label. Nil instruments or a nil
// meter skip the signal.
func (inst *Instruments) RecordNVDBaselineTripTransition(ctx context.Context, transition string) {
	if inst == nil || inst.NVDBaselineTripTransitions == nil {
		return
	}
	if !slices.Contains(nvdBaselineTripTransitions[:], transition) {
		transition = NVDBaselineOutcomeOther
	}
	inst.NVDBaselineTripTransitions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("transition", transition),
	))
}

// RecordNVDBaselineDirtySample records the latest trip-line probe: the
// trailing-hour NVD-proxy dirty rate and the oldest pending dirty age in
// seconds. Values, never labels, carry the figures. Nil instruments or
// nil gauges skip the signal.
func (inst *Instruments) RecordNVDBaselineDirtySample(ctx context.Context, proxyRowsPerHour int64, oldestDirtyAgeSeconds float64) {
	if inst == nil {
		return
	}
	if inst.NVDBaselineDirtyProxyRate != nil {
		inst.NVDBaselineDirtyProxyRate.Record(ctx, float64(proxyRowsPerHour))
	}
	if inst.NVDBaselineOldestDirtyAge != nil {
		age := oldestDirtyAgeSeconds
		if age < 0 {
			age = 0
		}
		inst.NVDBaselineOldestDirtyAge.Record(ctx, age)
	}
}

// registerNVDBaseline registers the NVD baseline signals (#10728): the
// commit-policy advance counter, the plan verdict counter, the trip-line
// transition counter, and the dirty-probe gauges. Labels stay closed and
// bounded: no scope, stamp, sha, or run value ever becomes a label.
func registerNVDBaseline(meter metric.Meter, inst *Instruments) error {
	var err error
	inst.NVDBaselineAdvances, err = meter.Int64Counter(
		"eshu_dp_nvd_baseline_advance_total",
		metric.WithDescription("Total NVD baseline commit-policy outcomes (#10728): advanced (checkpoint armed to the committed chunk end in the generation commit transaction), held (no verdict or hold verdict; facts commit, checkpoint holds), not_covered (stamped identity covers nothing new; facts commit, checkpoint holds), refused (the generation would have armed the window cursor writer; claim failed retryable, nothing committed)"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineAdvances counter: %w", err)
	}
	inst.NVDBaselinePlans, err = meter.Int64Counter(
		"eshu_dp_nvd_baseline_plans_total",
		metric.WithDescription("Total NVD baseline derivation verdicts per plan call (#10728): planned (one chunk minted), held (in-flight chunk, pace wait, missing or stale seed, or open trip; the hold reason rides the plan span), complete (every seed year reads complete)"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselinePlans counter: %w", err)
	}
	inst.NVDBaselineTripTransitions, err = meter.Int64Counter(
		"eshu_dp_nvd_baseline_trip_transitions_total",
		metric.WithDescription("Total NVD baseline trip-line transitions (#10728): opened (>15k NVD-proxy dirty rows/h in 2 consecutive hourly samples), paged (oldest dirty row past 96h; a page, never an auto-flip), eligible (oldest dirty under 48h for 6 hourly samples since the trip opened), closed (operator re-enabled), close_refused (operator tried to re-enable before six quiet hourly samples; the trip stays open)"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineTripTransitions counter: %w", err)
	}
	inst.NVDBaselineDirtyProxyRate, err = meter.Float64Gauge(
		"eshu_dp_nvd_baseline_dirty_proxy_rows_per_hour",
		metric.WithDescription("Latest sampled NVD-proxy dirty rate in rows/h (dirty-log rows whose advisory_id starts with CVE- captured in the trailing hour; #10728)"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineDirtyProxyRate gauge: %w", err)
	}
	inst.NVDBaselineOldestDirtyAge, err = meter.Float64Gauge(
		"eshu_dp_nvd_baseline_oldest_dirty_age_seconds",
		metric.WithDescription("Latest sampled oldest pending dirty age in seconds (the whole winners dirty-log backlog, not only NVD-proxy rows; #10728)"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineOldestDirtyAge gauge: %w", err)
	}
	return nil
}
