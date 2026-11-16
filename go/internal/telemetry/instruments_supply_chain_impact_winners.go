// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricDimensionTransition labels eshu_dp_supply_chain_impact_stale_copy_repairs_total
// with the retraction-state change of the repaired copy (#10765). The value set
// is closed: the SupplyChainImpactStaleCopyTransition* constants.
const MetricDimensionTransition = "transition"

// Stale-copy repair transition label values. They are the closed set of the
// transition label. "Active" means any suppression state other than
// advisory_retracted.
const (
	// SupplyChainImpactStaleCopyTransitionActiveToRetracted counts a copy that
	// was not retracted and a pass has now rewritten as advisory_retracted.
	SupplyChainImpactStaleCopyTransitionActiveToRetracted = "active_to_retracted"
	// SupplyChainImpactStaleCopyTransitionRetractedToActive counts a copy that
	// was advisory_retracted and a pass has now rewritten as not retracted.
	SupplyChainImpactStaleCopyTransitionRetractedToActive = "retracted_to_active"
)

// AttrTransition returns a transition attribute. v must be one of the
// SupplyChainImpactStaleCopyTransition* constants.
func AttrTransition(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionTransition, v)
}

// Winner-table action label values for eshu_dp_supply_chain_impact_winners_rows_total.
// They are the closed set of the action label.
const (
	// SupplyChainImpactWinnersActionInserted counts winner rows a resweep added.
	SupplyChainImpactWinnersActionInserted = "inserted"
	// SupplyChainImpactWinnersActionUpdated counts winner rows whose content changed.
	SupplyChainImpactWinnersActionUpdated = "updated"
	// SupplyChainImpactWinnersActionDeleted counts winner rows a resweep removed.
	SupplyChainImpactWinnersActionDeleted = "deleted"
	// SupplyChainImpactWinnersActionUnchanged counts current winners a resweep
	// compared and left untouched, so wrote no row version for.
	SupplyChainImpactWinnersActionUnchanged = "unchanged"
)

// supplyChainImpactWinnersResweepBuckets reach 300 s because the resweep is
// O(findings) and already takes tens of seconds at QA scale: an operator must see
// a statement pass the 60 s lease TTL, not only a sub-second to few-second range.
var supplyChainImpactWinnersResweepBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60, 120, 300}

// registerSupplyChainImpactWinnersInstruments registers the signals of the
// supply-chain impact winners resweep (#9774) on inst.
//
//   - eshu_dp_supply_chain_impact_winners_resweep_duration_seconds: wall time of
//     one committed resweep statement. The recompute is O(findings); a statement
//     past the 60 s lease TTL can overlap another instance.
//   - eshu_dp_supply_chain_impact_winners_rows_total{action}: winner rows by
//     inserted, updated, deleted and unchanged. Only inserted, updated and
//     deleted write a row version.
//   - eshu_dp_supply_chain_impact_winners_resweep_skipped_total: resweeps that
//     found another in flight and wrote nothing. Any rate means a resweep
//     outlasted the lease TTL.
//   - eshu_dp_supply_chain_impact_winners_watermark_age_seconds: now minus the
//     last watermark the maintainer saw, computed at scrape time. It keeps
//     climbing through a stall, when no resweep commits and no read succeeds, and
//     it never queries Postgres from a scrape. It has no value until the
//     process first sees a watermark.
//   - eshu_dp_supply_chain_impact_stale_copy_repairs_total{transition}: stored
//     finding copies a pass rewrote across the retraction boundary, by
//     active_to_retracted and retracted_to_active (#10765). Each is one stale
//     copy the holder fan-out found and repaired, so the rate says how often
//     a stale active copy could outrank a retracted one in the default view.
//
// No label carries a canonical key, scope, repository or CVE.
func registerSupplyChainImpactWinnersInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SupplyChainImpactWinnersResweepDuration, err = meter.Float64Histogram(
		"eshu_dp_supply_chain_impact_winners_resweep_duration_seconds",
		metric.WithDescription("Wall time of one committed supply-chain impact winners resweep statement. The recompute is O(findings), and a statement past the 60 s lease TTL can overlap another instance. A failed or skipped resweep records no sample; the resweep failure log carries the error."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(supplyChainImpactWinnersResweepBuckets...),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersResweepDuration histogram: %w", err)
	}
	if inst.SupplyChainImpactWinnersRows, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_rows_total",
		metric.WithDescription("Supply-chain impact winner rows a committed resweep handled, by action (inserted, updated, deleted, unchanged). Only inserted, updated and deleted write a row version; unchanged rows are compared and skipped."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersRows counter: %w", err)
	}
	if inst.SupplyChainImpactWinnersResweepSkipped, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_resweep_skipped_total",
		metric.WithDescription("Supply-chain impact winners resweeps that found another resweep in flight, so wrote and stamped nothing. A resweep can only overlap another after outlasting the lease TTL, so any rate means statements are running past it."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersResweepSkipped counter: %w", err)
	}
	if inst.SupplyChainImpactStaleCopyRepairs, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_stale_copy_repairs_total",
		metric.WithDescription("Stored supply-chain impact finding copies a pass rewrote across the retraction boundary, by transition (active_to_retracted, retracted_to_active). Each is one stale copy the holder fan-out repaired; until its scope re-derives, a stale active copy can outrank a retracted copy in the default findings view (#10765)."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactStaleCopyRepairs counter: %w", err)
	}
	inst.supplyChainImpactWinnersWatermarkUnixNano = new(atomic.Int64)
	if inst.SupplyChainImpactWinnersWatermarkAge, err = meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_watermark_age_seconds",
		metric.WithDescription("Seconds since the supply-chain impact winners read watermark was stamped, computed at scrape time from the last watermark this process saw. It rises between resweeps and drops on each commit, and keeps climbing through a stall. Above the API freshness window the read reports stale."),
		metric.WithUnit("s"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			if at := inst.supplyChainImpactWinnersWatermarkUnixNano.Load(); at != 0 {
				o.Observe(time.Since(time.Unix(0, at)).Seconds())
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersWatermarkAge gauge: %w", err)
	}
	return nil
}

// RecordSupplyChainImpactWinnersWatermark sets the watermark the age gauge ages
// from. It is safe on a nil receiver and from any goroutine.
func (i *Instruments) RecordSupplyChainImpactWinnersWatermark(at time.Time) {
	if i == nil || i.supplyChainImpactWinnersWatermarkUnixNano == nil {
		return
	}
	i.supplyChainImpactWinnersWatermarkUnixNano.Store(at.UnixNano())
}

// RecordSupplyChainImpactStaleCopyRepairs counts repaired stale copies under
// transition, which must be one of the SupplyChainImpactStaleCopyTransition*
// constants. It is safe on a nil receiver and adds nothing for a count below
// one.
func (i *Instruments) RecordSupplyChainImpactStaleCopyRepairs(ctx context.Context, transition string, repairs int64) {
	if i == nil || i.SupplyChainImpactStaleCopyRepairs == nil || repairs <= 0 {
		return
	}
	i.SupplyChainImpactStaleCopyRepairs.Add(ctx, repairs, metric.WithAttributes(AttrTransition(transition)))
}
