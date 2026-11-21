// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/metric"
)

// supplyChainImpactWinnersAuditSweepCounter is one cumulative sweep counter
// read from the snapshot.
type supplyChainImpactWinnersAuditSweepCounter struct {
	name, description string
	read              func(SupplyChainImpactWinnersStagedAuditSnapshot) int64
}

// supplyChainImpactWinnersAuditSweepCounters are the range sweep's cumulative
// counters (#11131), since process start.
var supplyChainImpactWinnersAuditSweepCounters = []supplyChainImpactWinnersAuditSweepCounter{
	{
		"eshu_dp_supply_chain_impact_winners_audit_sweep_ranges_total",
		"Staged winners audit sweep ranges committed (cursor advanced). Flat while the audit is allowed means the sweep is stuck; read the deferrals by reason.",
		func(s SupplyChainImpactWinnersStagedAuditSnapshot) int64 { return s.Ranges },
	},
	{
		"eshu_dp_supply_chain_impact_winners_audit_sweep_range_retries_total",
		"Staged winners audit range reads retried at half budget after the 25 s statement guard expired.",
		func(s SupplyChainImpactWinnersStagedAuditSnapshot) int64 { return s.RangeRetries },
	},
	{
		"eshu_dp_supply_chain_impact_winners_audit_sweep_facts_total",
		"Impact facts the committed staged winners audit ranges recomputed.",
		func(s SupplyChainImpactWinnersStagedAuditSnapshot) int64 { return s.RangeFacts },
	},
	{
		"eshu_dp_supply_chain_impact_winners_audit_sweep_candidates_total",
		"Identifiers whose recomputed winners differed from the stored winners in committed staged audit ranges.",
		func(s SupplyChainImpactWinnersStagedAuditSnapshot) int64 { return s.RangeCandidates },
	},
	{
		"eshu_dp_supply_chain_impact_winners_audit_sweep_pending_total",
		"Staged audit range candidates already queued in the dirty log or staging, dropped before the resweep lock.",
		func(s SupplyChainImpactWinnersStagedAuditSnapshot) int64 { return s.RangePending },
	},
	{
		"eshu_dp_supply_chain_impact_winners_audit_sweep_repairs_total",
		"Stale identifiers committed staged audit ranges queued for the next keyed tick (capture holes). Steady state is zero.",
		func(s SupplyChainImpactWinnersStagedAuditSnapshot) int64 { return s.RangeRepairs },
	},
	{
		"eshu_dp_supply_chain_impact_winners_audit_sweep_discards_total",
		"Staged audit range candidates that were not repaired (already queued, or clean under the lock).",
		func(s SupplyChainImpactWinnersStagedAuditSnapshot) int64 { return s.RangeDiscards },
	},
}

// registerSupplyChainImpactWinnersAuditSweepInstruments registers the range
// sweep's observable instruments over src (#11131):
//
//   - the cumulative counters above;
//   - eshu_dp_supply_chain_impact_winners_audit_sweep_ranges_done: the sweep
//     position, ranges the sweep in progress has committed, from the
//     persisted cursor row, so it survives a roll;
//   - ..._audit_sweep_last_completed_age_seconds: age of the last completed
//     sweep from the persisted row (no value before the first completion);
//   - ..._audit_sweep_last_seconds, ..._last_ranges, ..._last_candidates,
//     ..._last_repairs, ..._last_discards: the last completed sweep;
//   - ..._audit_sweep_last_range_seconds and ..._last_range_budget: the last
//     committed range in this process.
func registerSupplyChainImpactWinnersAuditSweepInstruments(meter metric.Meter, src SupplyChainImpactWinnersStagedAuditSource) error {
	counters := make([]metric.Int64ObservableCounter, len(supplyChainImpactWinnersAuditSweepCounters))
	observables := make([]metric.Observable, 0, len(counters)+9)
	for i, c := range supplyChainImpactWinnersAuditSweepCounters {
		counter, err := meter.Int64ObservableCounter(c.name, metric.WithDescription(c.description))
		if err != nil {
			return fmt.Errorf("register %s: %w", c.name, err)
		}
		counters[i] = counter
		observables = append(observables, counter)
	}
	gauge := func(name, description string) (metric.Int64ObservableGauge, error) {
		g, err := meter.Int64ObservableGauge(name, metric.WithDescription(description))
		if err != nil {
			return nil, fmt.Errorf("register %s: %w", name, err)
		}
		observables = append(observables, g)
		return g, nil
	}
	seconds := func(name, description string) (metric.Float64ObservableGauge, error) {
		g, err := meter.Float64ObservableGauge(name, metric.WithDescription(description), metric.WithUnit("s"))
		if err != nil {
			return nil, fmt.Errorf("register %s: %w", name, err)
		}
		observables = append(observables, g)
		return g, nil
	}
	position, err := gauge("eshu_dp_supply_chain_impact_winners_audit_sweep_ranges_done",
		"Ranges the staged winners audit sweep in progress has committed, read from the persisted cursor row (survives a roll).")
	if err != nil {
		return fmt.Errorf("register audit sweep ranges done gauge: %w", err)
	}
	completedAge, err := seconds("eshu_dp_supply_chain_impact_winners_audit_sweep_last_completed_age_seconds",
		"Age of the last completed staged winners audit sweep from the persisted cursor row. The bar is 24 h; no value before the first completion.")
	if err != nil {
		return fmt.Errorf("register audit sweep completed age gauge: %w", err)
	}
	lastSeconds, err := seconds("eshu_dp_supply_chain_impact_winners_audit_sweep_last_seconds",
		"Duration of the last completed staged winners audit sweep, first range commit to last.")
	if err != nil {
		return fmt.Errorf("register audit sweep last seconds gauge: %w", err)
	}
	lastRanges, err := gauge("eshu_dp_supply_chain_impact_winners_audit_sweep_last_ranges",
		"Ranges of the last completed staged winners audit sweep.")
	if err != nil {
		return fmt.Errorf("register audit sweep last ranges gauge: %w", err)
	}
	lastCandidates, err := gauge("eshu_dp_supply_chain_impact_winners_audit_sweep_last_candidates",
		"Candidates of the last completed staged winners audit sweep.")
	if err != nil {
		return fmt.Errorf("register audit sweep last candidates gauge: %w", err)
	}
	lastRepairs, err := gauge("eshu_dp_supply_chain_impact_winners_audit_sweep_last_repairs",
		"Repairs of the last completed staged winners audit sweep.")
	if err != nil {
		return fmt.Errorf("register audit sweep last repairs gauge: %w", err)
	}
	lastDiscards, err := gauge("eshu_dp_supply_chain_impact_winners_audit_sweep_last_discards",
		"Discards of the last completed staged winners audit sweep.")
	if err != nil {
		return fmt.Errorf("register audit sweep last discards gauge: %w", err)
	}
	lastRange, err := seconds("eshu_dp_supply_chain_impact_winners_audit_sweep_last_range_seconds",
		"Wall time of the last committed staged winners audit range in this process.")
	if err != nil {
		return fmt.Errorf("register audit sweep last range gauge: %w", err)
	}
	lastBudget, err := gauge("eshu_dp_supply_chain_impact_winners_audit_sweep_last_range_budget",
		"Row budget of the last committed staged winners audit range; below the configured budget means timeouts halved it.")
	if err != nil {
		return fmt.Errorf("register audit sweep last range budget gauge: %w", err)
	}
	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		snap := src.StagedAuditSnapshot()
		for i, c := range supplyChainImpactWinnersAuditSweepCounters {
			o.ObserveInt64(counters[i], c.read(snap))
		}
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
	}, observables...)
	if err != nil {
		return fmt.Errorf("register audit sweep callback: %w", err)
	}
	return nil
}
