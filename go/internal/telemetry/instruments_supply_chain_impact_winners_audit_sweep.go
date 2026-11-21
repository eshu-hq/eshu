// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/metric"
)

// registerSupplyChainImpactWinnersAuditSweepInstruments registers the staged
// audit range sweep's observable instruments over src (#11131), read at
// scrape time:
//
//   - cumulative counters since process start: ranges committed, range reads
//     retried at half budget, and the facts, candidates, pending candidates,
//     repairs and discards of committed ranges;
//   - eshu_dp_supply_chain_impact_winners_audit_sweep_ranges_done: the sweep
//     position, from the persisted cursor row, so it survives a roll;
//   - ..._audit_sweep_last_completed_age_seconds: age of the last completed
//     sweep from the persisted row (no value before the first completion);
//   - ..._audit_sweep_last_seconds, ..._last_ranges, ..._last_candidates,
//     ..._last_repairs, ..._last_discards: the last completed sweep;
//   - ..._audit_sweep_last_range_seconds and ..._last_range_budget: the last
//     committed range in this process.
func registerSupplyChainImpactWinnersAuditSweepInstruments(meter metric.Meter, src SupplyChainImpactWinnersStagedAuditSource) error {
	ranges, err := meter.Int64ObservableCounter(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_ranges_total",
		metric.WithDescription("Staged winners audit sweep ranges committed (cursor advanced). Flat while the audit is allowed means the sweep is stuck; read the deferrals by reason."),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_ranges_total: %w", err)
	}
	retries, err := meter.Int64ObservableCounter(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_range_retries_total",
		metric.WithDescription("Staged winners audit range reads retried at half budget after the 25 s statement guard expired."),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_range_retries_total: %w", err)
	}
	facts, err := meter.Int64ObservableCounter(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_facts_total",
		metric.WithDescription("Impact facts the committed staged winners audit ranges recomputed."),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_facts_total: %w", err)
	}
	candidates, err := meter.Int64ObservableCounter(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_candidates_total",
		metric.WithDescription("Identifiers whose recomputed winners differed from the stored winners in committed staged audit ranges."),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_candidates_total: %w", err)
	}
	pending, err := meter.Int64ObservableCounter(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_pending_total",
		metric.WithDescription("Staged audit range candidates already queued in the dirty log or staging, dropped before the resweep lock."),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_pending_total: %w", err)
	}
	repairs, err := meter.Int64ObservableCounter(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_repairs_total",
		metric.WithDescription("Stale identifiers committed staged audit ranges queued for the next keyed tick (capture holes). Steady state is zero."),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_repairs_total: %w", err)
	}
	discards, err := meter.Int64ObservableCounter(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_discards_total",
		metric.WithDescription("Staged audit range candidates that were not repaired (already queued, or clean under the lock)."),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_discards_total: %w", err)
	}
	position, err := meter.Int64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_ranges_done",
		metric.WithDescription("Ranges the staged winners audit sweep in progress has committed, read from the persisted cursor row (survives a roll)."),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_ranges_done: %w", err)
	}
	completedAge, err := meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_last_completed_age_seconds",
		metric.WithDescription("Age of the last completed staged winners audit sweep from the persisted cursor row. The bar is 24 h; no value before the first completion."), metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_last_completed_age_seconds: %w", err)
	}
	lastSeconds, err := meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_last_seconds",
		metric.WithDescription("Duration of the last completed staged winners audit sweep, first range commit to last."), metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_last_seconds: %w", err)
	}
	lastRanges, err := meter.Int64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_last_ranges",
		metric.WithDescription("Ranges of the last completed staged winners audit sweep."),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_last_ranges: %w", err)
	}
	lastCandidates, err := meter.Int64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_last_candidates",
		metric.WithDescription("Candidates of the last completed staged winners audit sweep."),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_last_candidates: %w", err)
	}
	lastRepairs, err := meter.Int64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_last_repairs",
		metric.WithDescription("Repairs of the last completed staged winners audit sweep."),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_last_repairs: %w", err)
	}
	lastDiscards, err := meter.Int64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_last_discards",
		metric.WithDescription("Discards of the last completed staged winners audit sweep."),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_last_discards: %w", err)
	}
	lastRange, err := meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_last_range_seconds",
		metric.WithDescription("Wall time of the last committed staged winners audit range in this process."), metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_last_range_seconds: %w", err)
	}
	lastBudget, err := meter.Int64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_audit_sweep_last_range_budget",
		metric.WithDescription("Row budget of the last committed staged winners audit range; below the configured budget means timeouts halved it."),
	)
	if err != nil {
		return fmt.Errorf("register eshu_dp_supply_chain_impact_winners_audit_sweep_last_range_budget: %w", err)
	}
	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		snap := src.StagedAuditSnapshot()
		o.ObserveInt64(ranges, snap.Ranges)
		o.ObserveInt64(retries, snap.RangeRetries)
		o.ObserveInt64(facts, snap.RangeFacts)
		o.ObserveInt64(candidates, snap.RangeCandidates)
		o.ObserveInt64(pending, snap.RangePending)
		o.ObserveInt64(repairs, snap.RangeRepairs)
		o.ObserveInt64(discards, snap.RangeDiscards)
		if snap.Ranges > 0 {
			o.ObserveFloat64(lastRange, snap.LastRange.Seconds())
			o.ObserveInt64(lastBudget, snap.LastRangeBudget)
		}
		if !snap.SweepSeen {
			return nil
		}
		o.ObserveInt64(position, snap.SweepRangesDone)
		if !snap.LastSweepCompletedAt.IsZero() {
			o.ObserveFloat64(completedAge, time.Since(snap.LastSweepCompletedAt).Seconds())
			o.ObserveFloat64(lastSeconds, snap.LastSweepSeconds)
			o.ObserveInt64(lastRanges, snap.LastSweepRanges)
			o.ObserveInt64(lastCandidates, snap.LastSweepCandidates)
			o.ObserveInt64(lastRepairs, snap.LastSweepRepairs)
			o.ObserveInt64(lastDiscards, snap.LastSweepDiscards)
		}
		return nil
	}, ranges, retries, facts, candidates, pending, repairs, discards, position, completedAge, lastSeconds, lastRanges, lastCandidates, lastRepairs, lastDiscards, lastRange, lastBudget)
	if err != nil {
		return fmt.Errorf("register audit sweep callback: %w", err)
	}
	return nil
}
