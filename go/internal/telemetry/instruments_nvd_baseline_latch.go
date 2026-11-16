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

// Closed outcome set of the durable NVD baseline trip latch
// (eshu-hq/eshu#10768): the outcome label of
// eshu_dp_nvd_baseline_trip_latch_total. The coordinator's
// NVDBaselineTripLatchOutcome values carry the same strings, and a parity
// test there keeps the two sets equal.
const (
	// NVDBaselineTripLatchLoaded: the stored latch was read.
	NVDBaselineTripLatchLoaded = "loaded"
	// NVDBaselineTripLatchAbsent: the run has no latch row yet; planning
	// holds until the first hourly sample writes one.
	NVDBaselineTripLatchAbsent = "absent"
	// NVDBaselineTripLatchReadFailed: the latch could not be read; planning
	// holds (fail closed).
	NVDBaselineTripLatchReadFailed = "read_failed"
	// NVDBaselineTripLatchApplied: an hourly sample was folded and saved.
	NVDBaselineTripLatchApplied = "applied"
	// NVDBaselineTripLatchAppliedAfterGap: a sample was saved after a missed
	// hour broke the hot and quiet streaks.
	NVDBaselineTripLatchAppliedAfterGap = "applied_after_gap"
	// NVDBaselineTripLatchStale: a replayed sample at or before the stored
	// sample time changed nothing.
	NVDBaselineTripLatchStale = "stale"
	// NVDBaselineTripLatchTooSoon: a sample inside the minimum spacing
	// changed nothing.
	NVDBaselineTripLatchTooSoon = "too_soon"
	// NVDBaselineTripLatchLostRace: another writer moved the latch first, so
	// this sample was dropped.
	NVDBaselineTripLatchLostRace = "lost_race"
	// NVDBaselineTripLatchWriteFailed: saving the latch failed.
	NVDBaselineTripLatchWriteFailed = "write_failed"
)

var nvdBaselineTripLatchOutcomes = [...]string{
	NVDBaselineTripLatchLoaded,
	NVDBaselineTripLatchAbsent,
	NVDBaselineTripLatchReadFailed,
	NVDBaselineTripLatchApplied,
	NVDBaselineTripLatchAppliedAfterGap,
	NVDBaselineTripLatchStale,
	NVDBaselineTripLatchTooSoon,
	NVDBaselineTripLatchLostRace,
	NVDBaselineTripLatchWriteFailed,
}

// NVDBaselineTripLatchOutcomes returns the closed, ordered latch outcome set.
func NVDBaselineTripLatchOutcomes() []string {
	return slices.Clone(nvdBaselineTripLatchOutcomes[:])
}

// RecordNVDBaselineTripLatch counts one durable-latch load or save outcome.
// A load that could not prove the trip shut (absent, read_failed) is the
// signal that distinguishes a held lane with an unreadable latch from a held
// lane with a real open trip. Labels stay outcome-only; no run id ever
// becomes a label. A value outside the closed set records as other. Nil
// instruments or a nil meter skip the signal.
func (inst *Instruments) RecordNVDBaselineTripLatch(ctx context.Context, outcome string) {
	if inst == nil || inst.NVDBaselineTripLatchOutcomes == nil {
		return
	}
	if !slices.Contains(nvdBaselineTripLatchOutcomes[:], outcome) {
		outcome = NVDBaselineOutcomeOther
	}
	inst.NVDBaselineTripLatchOutcomes.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

// registerNVDBaselineTripLatch registers the durable-latch outcome counter
// (#10768).
func registerNVDBaselineTripLatch(meter metric.Meter, inst *Instruments) error {
	var err error
	inst.NVDBaselineTripLatchOutcomes, err = meter.Int64Counter(
		"eshu_dp_nvd_baseline_trip_latch_total",
		metric.WithDescription("Total durable NVD baseline trip latch outcomes (#10768): loaded, absent (no row yet; planning holds until the first hourly sample), read_failed (planning holds, fail closed), applied and applied_after_gap (an hourly sample saved; a missed hour restarts the hot and quiet streaks), stale and too_soon (a replayed or re-sampled hour changed nothing), lost_race (another writer moved the latch first; the sample was dropped), write_failed"),
	)
	if err != nil {
		return fmt.Errorf("register NVDBaselineTripLatchOutcomes counter: %w", err)
	}
	return nil
}
