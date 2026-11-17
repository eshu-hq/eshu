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
	// NVDBaselineOutcomeAdvanced: the commit policy accepted the verdict and
	// armed the checkpoint writer. It is recorded at the decision, before the
	// commit transaction runs, so the writer can still refuse a seed rollback
	// (counted on the seed rotation counter's rollback direction) and a failed
	// commit leaves the checkpoint where it was; the committed-chunk INFO line
	// is the signal that the advance landed.
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

// NVDBaselineSeedRotationDirections is the closed direction set of
// eshu_dp_nvd_baseline_seed_rotations_total (eshu-hq/eshu#10810).
const (
	// NVDBaselineSeedRotationForward: the committed chunk read a newer seed
	// than the stored one, so the lane resumed across the rotation.
	NVDBaselineSeedRotationForward = "forward"
	// NVDBaselineSeedRotationRollback: the candidate read an older seed than
	// the stored one, so the commit transaction refused it and the whole
	// generation rolled back.
	NVDBaselineSeedRotationRollback = "rollback"
)

var nvdBaselineSeedRotationDirections = [...]string{
	NVDBaselineSeedRotationForward,
	NVDBaselineSeedRotationRollback,
}

// NVDBaselineSeedRotationDirectionSet returns the closed, ordered direction set.
func NVDBaselineSeedRotationDirectionSet() []string {
	return slices.Clone(nvdBaselineSeedRotationDirections[:])
}

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

// RecordNVDBaselineSeedRotation counts one seed rotation seen by the baseline
// checkpoint commit: forward or rollback. A direction outside the closed set
// is dropped, so a typo never mints a label. Nil instruments or a nil counter
// skip the signal.
func (inst *Instruments) RecordNVDBaselineSeedRotation(ctx context.Context, direction string) {
	if inst == nil || inst.NVDBaselineSeedRotations == nil {
		return
	}
	if !slices.Contains(nvdBaselineSeedRotationDirections[:], direction) {
		return
	}
	inst.NVDBaselineSeedRotations.Add(ctx, 1, metric.WithAttributes(attribute.String("direction", direction)))
}

// RecordNVDBaselineSettlementEviction counts one chunk settlement the host's
// bounded post-commit stash dropped at capacity (#10746). The counter
// carries no label, so a work item or run id can never become a label value.
// Nil instruments or a nil counter skip the signal.
func (inst *Instruments) RecordNVDBaselineSettlementEviction(ctx context.Context) {
	if inst == nil || inst.NVDBaselineSettlementEvictions == nil {
		return
	}
	inst.NVDBaselineSettlementEvictions.Add(ctx, 1)
}

// RecordNVDBaselineProgress records the latest run-wide baseline progress
// planned from the stored checkpoints: years replayed whole of the years the
// run covers, and records done of the records the started years hold. Values,
// never labels, carry the figures; negative inputs clamp to zero. Nil
// instruments or nil gauges skip the signal.
func (inst *Instruments) RecordNVDBaselineProgress(ctx context.Context, yearsComplete, yearsTotal, recordsDone, recordsTotal int64) {
	if inst == nil {
		return
	}
	record := func(gauge metric.Int64Gauge, value int64) {
		if gauge == nil {
			return
		}
		if value < 0 {
			value = 0
		}
		gauge.Record(ctx, value)
	}
	record(inst.NVDBaselineYearsComplete, yearsComplete)
	record(inst.NVDBaselineYearsTotal, yearsTotal)
	record(inst.NVDBaselineRecordsDone, recordsDone)
	record(inst.NVDBaselineRecordsTotal, recordsTotal)
}

// nvdBaselinePlanHoldReasons is the closed reason set of
// eshu_dp_nvd_baseline_plan_holds_total (eshu-hq/eshu#10814). The
// coordinator's NVDBaselineHold* constants carry the same strings, and a
// parity test there keeps the two sets equal and in the same order.
var nvdBaselinePlanHoldReasons = [...]string{
	"baseline_chunk_in_flight",
	"baseline_projection_in_flight",
	"baseline_reducer_in_flight",
	"baseline_pace_wait",
	"baseline_retry_wait",
	"baseline_trip_open",
	"baseline_trip_unloaded",
	"baseline_trip_stale",
	"baseline_drain_unloaded",
	"baseline_downstream_failed",
	"baseline_config_error",
}

// nvdBaselineChunkCycleBuckets are the explicit boundaries of
// eshu_dp_nvd_baseline_chunk_cycle_seconds, in seconds. A chunk's
// plan-to-drained time runs from minutes (the 5 minute pace floor) to hours
// (a slow reducer drain), so the set resolves one minute to one day; the
// default OTEL set is sized for milliseconds and would put every cycle in
// one bucket (#7084).
var nvdBaselineChunkCycleBuckets = []float64{
	60, 120, 300, 600, 900, 1200, 1800, 2700, 3600, 5400, 7200, 14400, 28800, 86400,
}

// NVDBaselinePlanHoldReasons returns the closed, ordered plan hold reason set.
func NVDBaselinePlanHoldReasons() []string {
	return slices.Clone(nvdBaselinePlanHoldReasons[:])
}

// RecordNVDBaselinePlanHold counts one plan call that minted no baseline
// chunk because the lane held, under its closed hold reason
// (eshu-hq/eshu#10814). A hold that other years' chunks overrode (some year
// planned while another waited) is not a hold of the plan and is not counted
// here; the plan span carries the failed and unprovable year counts. The
// label is reason-only: no run, year, or scope ever becomes a label, and a
// value outside the closed set records as other. Nil instruments or a nil
// counter skip the signal.
func (inst *Instruments) RecordNVDBaselinePlanHold(ctx context.Context, reason string) {
	if inst == nil || inst.NVDBaselinePlanHolds == nil {
		return
	}
	if !slices.Contains(nvdBaselinePlanHoldReasons[:], reason) {
		reason = NVDBaselineOutcomeOther
	}
	inst.NVDBaselinePlanHolds.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// RecordNVDBaselinePaceInterval records the latest per-year pace interval the
// planner applied, in seconds. A plan that held before the pace was known
// (an open, unloaded, or stale trip) passes zero, and a non-positive value
// records nothing, so the gauge keeps the last interval the planner really
// applied instead of a value it never used. Nil instruments or a nil gauge
// skip the signal.
func (inst *Instruments) RecordNVDBaselinePaceInterval(ctx context.Context, seconds float64) {
	if inst == nil || inst.NVDBaselinePaceInterval == nil || seconds <= 0 {
		return
	}
	inst.NVDBaselinePaceInterval.Record(ctx, seconds)
}

// RecordNVDBaselineChunkCycle records one committed chunk's plan-to-drained
// time in seconds: from the plan that minted it to the last of its collector
// claim, generation activation, and reducer item settling. A non-positive
// sample records nothing. The histogram carries no label. Nil instruments or
// a nil histogram skip the signal.
func (inst *Instruments) RecordNVDBaselineChunkCycle(ctx context.Context, seconds float64) {
	if inst == nil || inst.NVDBaselineChunkCycle == nil || seconds <= 0 {
		return
	}
	inst.NVDBaselineChunkCycle.Record(ctx, seconds)
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
// transition counter, the seed-rotation and settlement-eviction counters, and
// the dirty-probe and progress gauges. Labels stay closed and
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
		metric.WithDescription("Total NVD baseline derivation verdicts per plan call (#10728): planned (one chunk minted), held (in-flight chunk, pace wait, missing or stale seed, open trip, trip latch never loaded, or invalid baseline config isolated to a hold per #10762; the hold reason rides the plan span), complete (every seed year reads complete)"),
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
	inst.NVDBaselineSeedRotations, err = meter.Int64Counter(
		"eshu_dp_nvd_baseline_seed_rotations_total",
		metric.WithDescription("Total NVD baseline seed rotations seen in the checkpoint commit transaction (#10810): forward (the chunk read a newer seed than the stored one; the lane resumes) and rollback (the chunk read an older seed; the commit refuses and the whole generation rolls back with class nvd_baseline_seed_rollback)"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineSeedRotations counter: %w", err)
	}
	inst.NVDBaselineSettlementEvictions, err = meter.Int64Counter(
		"eshu_dp_nvd_baseline_settlement_evictions_total",
		metric.WithDescription("Total NVD baseline chunk settlements the extension host's bounded post-commit stash dropped at capacity (#10746): the chunk's commit is unaffected, but when it had committed, its forward-rotation count and committed-chunk log line are lost, so a rise means those signals may undercount; the WARN names the dropped work item"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineSettlementEvictions counter: %w", err)
	}
	inst.NVDBaselineYearsComplete, err = meter.Int64Gauge(
		"eshu_dp_nvd_baseline_years_complete",
		metric.WithDescription("Latest planned count of baseline years replayed whole in the run (#10809)"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineYearsComplete gauge: %w", err)
	}
	inst.NVDBaselineYearsTotal, err = meter.Int64Gauge(
		"eshu_dp_nvd_baseline_years_total",
		metric.WithDescription("Latest planned count of years the baseline run covers, first year 2002 through the configured last_year (#10809)"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineYearsTotal gauge: %w", err)
	}
	inst.NVDBaselineRecordsDone, err = meter.Int64Gauge(
		"eshu_dp_nvd_baseline_records_done",
		metric.WithDescription("Latest planned sum of records committed across the baseline run's started years; 0 until the collector reports its record counts (#10809)"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineRecordsDone gauge: %w", err)
	}
	inst.NVDBaselineRecordsTotal, err = meter.Int64Gauge(
		"eshu_dp_nvd_baseline_records_total",
		metric.WithDescription("Latest planned sum of records the baseline run's started years hold; 0 until the collector reports its record counts (#10809)"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineRecordsTotal gauge: %w", err)
	}
	inst.NVDBaselinePlanHolds, err = meter.Int64Counter(
		"eshu_dp_nvd_baseline_plan_holds_total",
		metric.WithDescription("Total plan calls that minted no NVD baseline chunk because the lane held (#10814), by closed reason: baseline_chunk_in_flight (a claimed chunk or the in-flight year bound), baseline_projection_in_flight and baseline_reducer_in_flight (the prior chunk has not finished projecting or reducing), baseline_pace_wait and baseline_retry_wait (the pace interval or the retry wait has not passed), baseline_trip_open, baseline_trip_unloaded, and baseline_trip_stale (the trip latch is open, never loaded, or its last sample is older than 90 minutes), baseline_drain_unloaded (the drain probe failed or never ran), baseline_downstream_failed (a terminally failed projector or reducer item; other years keep planning, so the plan span carries the count), baseline_config_error (invalid baseline config)"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselinePlanHolds counter: %w", err)
	}
	inst.NVDBaselinePaceInterval, err = meter.Float64Gauge(
		"eshu_dp_nvd_baseline_pace_interval_seconds",
		metric.WithDescription("Latest per-year NVD baseline pace interval the planner applied, in seconds: max(floor, 30 minutes / 2^quiet-hour streak); not updated while the lane holds before the pace is known (#10814)"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselinePaceInterval gauge: %w", err)
	}
	inst.NVDBaselineChunkCycle, err = meter.Float64Histogram(
		"eshu_dp_nvd_baseline_chunk_cycle_seconds",
		metric.WithDescription("Seconds from planning an NVD baseline chunk to its drain: collector claim closed, generation active, and supply-chain impact reducer item done, recorded when the year's next chunk plans (#10814)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(nvdBaselineChunkCycleBuckets...),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineChunkCycle histogram: %w", err)
	}
	return registerNVDBaselineTripLatch(meter, inst)
}
