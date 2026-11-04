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

// MetricDimensionPath labels eshu_dp_supply_chain_impact_winners_resweep_duration_seconds
// with the path that produced the sample (#9805). The value set is closed: the
// SupplyChainImpactWinnersPath* constants.
const MetricDimensionPath = "path"

// Resweep path label values. They are the closed set of the path label.
const (
	// SupplyChainImpactWinnersPathFull is the whole-table resweep: the startup
	// reconcile, or a keyed tick the cap sent to the full rebuild.
	SupplyChainImpactWinnersPathFull = "full"
	// SupplyChainImpactWinnersPathKeyed is a keyed resweep of the keys the dirty
	// log named.
	SupplyChainImpactWinnersPathKeyed = "keyed"
	// SupplyChainImpactWinnersPathAudit is the full-recompute audit read.
	SupplyChainImpactWinnersPathAudit = "audit"
)

// Dirty-log capture source label values for
// eshu_dp_supply_chain_impact_winners_capture_rows_total. They match the source
// column of supply_chain_impact_winners_dirty_log, and are the closed set of the
// source label on that counter.
const (
	// SupplyChainImpactWinnersSourceFactUpsert is a finding the writer inserted
	// or rewrote with changed content.
	SupplyChainImpactWinnersSourceFactUpsert = "fact_upsert"
	// SupplyChainImpactWinnersSourceFactRetract is a finding the writer retracted.
	SupplyChainImpactWinnersSourceFactRetract = "fact_retract"
	// SupplyChainImpactWinnersSourceActivation is a generation activation flip.
	SupplyChainImpactWinnersSourceActivation = "activation"
	// SupplyChainImpactWinnersSourceOperator is an operator-suppression
	// generation change.
	SupplyChainImpactWinnersSourceOperator = "operator"
	// SupplyChainImpactWinnersSourceScanner is a scanner classification or
	// association change.
	SupplyChainImpactWinnersSourceScanner = "scanner"
	// SupplyChainImpactWinnersSourceAudit is a key the audit backstop queued.
	SupplyChainImpactWinnersSourceAudit = "audit"
)

// Keyed winners tick phase label values (#9975). They are the closed set of the
// phase label on eshu_dp_supply_chain_impact_winners_tick_phase_duration_seconds.
// Each names one contiguous group of statements of ResweepWinnersKeyed, so the
// phases of a tick that reached them sum to the tick's wall time less the
// transaction rollback.
const (
	// SupplyChainImpactWinnersPhaseClaim is the transaction begin and the
	// resweep try-lock. It is the only phase of a tick that lost the lock.
	SupplyChainImpactWinnersPhaseClaim = "claim"
	// SupplyChainImpactWinnersPhaseConsume is the dirty-log DELETE ... RETURNING
	// into the consumed temp table and its tally by capture source.
	SupplyChainImpactWinnersPhaseConsume = "consume"
	// SupplyChainImpactWinnersPhaseBuildIdents is the identifier set built from
	// the consumed rows, its ANALYZE, and the cap check on it.
	SupplyChainImpactWinnersPhaseBuildIdents = "build_idents"
	// SupplyChainImpactWinnersPhaseBuildActiveFacts is the keyed active_facts
	// temp table (the probe into fact_records), the operator facts added to it,
	// and its ANALYZE.
	SupplyChainImpactWinnersPhaseBuildActiveFacts = "build_active_facts"
	// SupplyChainImpactWinnersPhaseApply is the winner select and the row
	// writes of one keyed apply statement.
	SupplyChainImpactWinnersPhaseApply = "apply"
	// SupplyChainImpactWinnersPhaseFullRebuild is the whole-table rebuild a tick
	// ran in place of the keyed build and apply because it went over the cap.
	SupplyChainImpactWinnersPhaseFullRebuild = "full_rebuild"
	// SupplyChainImpactWinnersPhaseStamp is the watermark stamp of a tick that
	// consumed nothing.
	SupplyChainImpactWinnersPhaseStamp = "stamp"
	// SupplyChainImpactWinnersPhaseCommit is the transaction commit.
	SupplyChainImpactWinnersPhaseCommit = "commit"
)

// supplyChainImpactWinnersKeysBuckets span a quiet tick (a few keys) to the cap
// (50,000 identifiers) so an operator sees a tick approach the full-rebuild
// fallback.
var supplyChainImpactWinnersKeysBuckets = []float64{1, 10, 100, 1000, 5000, 10000, 25000, 50000, 100000}

// AttrPath returns a path attribute. v must be one of the
// SupplyChainImpactWinnersPath* constants.
func AttrPath(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionPath, v)
}

// supplyChainImpactWinnersDirtyLog holds the last dirty-log depth and oldest-row
// age a tick sampled, for the observable gauges to report at scrape time.
type supplyChainImpactWinnersDirtyLog struct {
	depth         atomic.Int64
	oldestAgeNano atomic.Int64
	sampled       atomic.Bool
}

// registerSupplyChainImpactWinnersKeyedInstruments registers the keyed-resweep
// signals of #9805 on inst.
//
//   - eshu_dp_supply_chain_impact_winners_keys_per_sweep: canonical keys a keyed
//     tick recomputed. A value near the cap is the signal before a tick falls
//     back to the full rebuild.
//   - eshu_dp_supply_chain_impact_winners_identifiers_consumed_total: dirty-log
//     identifier rows a keyed tick consumed.
//   - eshu_dp_supply_chain_impact_winners_capture_rows_total{source}: dirty-log
//     rows appended to the log, by capture source. The impact writer counts
//     fact_upsert and fact_retract once their transaction commits; the maintainer counts
//     activation, operator and scanner (database triggers cannot emit a metric)
//     when it consumes them. A source that goes flat while the others move is a
//     capture path that stopped.
//   - eshu_dp_supply_chain_impact_winners_backstop_repairs_total: stale keys the
//     full-recompute audit queued. Non-zero means a change class left no
//     capture row; steady state is zero.
//   - eshu_dp_supply_chain_impact_winners_audit_skipped_total: audits that
//     could not take the resweep lock, so verified nothing (#9957). A streak
//     means the backstop is not running.
//   - eshu_dp_supply_chain_impact_winners_dirty_log_depth and
//     ..._dirty_log_oldest_age_seconds: the dirty log's backlog and how long its
//     oldest row has waited, sampled every tick.
//   - eshu_dp_supply_chain_impact_winners_tick_phase_duration_seconds{phase}:
//     wall time of each phase of one keyed tick, so a slow tick reads as a slow
//     read (build_idents, build_active_facts), a slow write (apply, commit), or
//     a slow consume. A phase that fails still records the time it spent.
//
// No label carries a canonical key, scope, repository or CVE.
func registerSupplyChainImpactWinnersKeyedInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SupplyChainImpactWinnersKeysPerSweep, err = meter.Int64Histogram(
		"eshu_dp_supply_chain_impact_winners_keys_per_sweep",
		metric.WithDescription("Canonical keys one keyed supply-chain impact winners resweep recomputed. A value near the 50,000 identifier cap is the signal before a tick falls back to the full rebuild."),
		metric.WithExplicitBucketBoundaries(supplyChainImpactWinnersKeysBuckets...),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersKeysPerSweep histogram: %w", err)
	}
	if inst.SupplyChainImpactWinnersIdentifiersConsumed, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_identifiers_consumed_total",
		metric.WithDescription("Supply-chain impact winners dirty-log identifier rows a keyed resweep consumed."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersIdentifiersConsumed counter: %w", err)
	}
	if inst.SupplyChainImpactWinnersCaptureRows, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_capture_rows_total",
		metric.WithDescription("Supply-chain impact winners dirty-log rows appended, by source (fact_upsert, fact_retract, activation, operator, scanner, audit). The impact writer counts fact_upsert and fact_retract after its transaction commits, so a rollback adds nothing; the maintainer counts the trigger sources activation, operator and scanner when it consumes them. A source that goes flat while the others move is a capture path that stopped."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersCaptureRows counter: %w", err)
	}
	if inst.SupplyChainImpactWinnersBackstopRepairs, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_backstop_repairs_total",
		metric.WithDescription("Stale supply-chain impact winner keys the full-recompute audit found and queued for the next keyed tick. Steady state is zero; a non-zero rate means a change class left no dirty-log row."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersBackstopRepairs counter: %w", err)
	}
	if inst.SupplyChainImpactWinnersAuditSkipped, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_audit_skipped_total",
		metric.WithDescription("Supply-chain impact winners audits that found stale-key candidates but could not take the resweep lock, so verified nothing and queued nothing. The audit is the backstop for a change that left no dirty-log row; a streak of skips means the backstop is not running."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersAuditSkipped counter: %w", err)
	}
	if inst.SupplyChainImpactWinnersTickPhaseDuration, err = meter.Float64Histogram(
		"eshu_dp_supply_chain_impact_winners_tick_phase_duration_seconds",
		metric.WithDescription("Wall time of one phase of a keyed supply-chain impact winners tick, by phase (claim, consume, build_idents, build_active_facts, apply, full_rebuild, stamp, commit). One sample per phase the tick ran; a phase whose statement failed still records the time it spent, and a tick that lost the resweep lock records only claim. It splits the tick's resweep_duration_seconds into reads (build_idents, build_active_facts) and writes (apply, commit)."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(supplyChainImpactWinnersResweepBuckets...),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersTickPhaseDuration histogram: %w", err)
	}
	inst.supplyChainImpactWinnersDirtyLog = new(supplyChainImpactWinnersDirtyLog)
	dirty := inst.supplyChainImpactWinnersDirtyLog
	if inst.SupplyChainImpactWinnersDirtyLogDepth, err = meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_dirty_log_depth",
		metric.WithDescription("Rows waiting in the supply-chain impact winners dirty log at the last keyed tick this process sampled. A depth that keeps climbing means the keyed resweep is not keeping up or is not running."),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			if dirty.sampled.Load() {
				o.Observe(float64(dirty.depth.Load()))
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersDirtyLogDepth gauge: %w", err)
	}
	if inst.SupplyChainImpactWinnersDirtyLogOldestAge, err = meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_dirty_log_oldest_age_seconds",
		metric.WithDescription("Age of the oldest row in the supply-chain impact winners dirty log at the last keyed tick this process sampled, 0 when the log was empty. It bounds how stale a winner can be behind the facts."),
		metric.WithUnit("s"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			if dirty.sampled.Load() {
				o.Observe(time.Duration(dirty.oldestAgeNano.Load()).Seconds())
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersDirtyLogOldestAge gauge: %w", err)
	}
	return nil
}

// RecordSupplyChainImpactWinnersDirtyLog sets the dirty-log depth and oldest-row
// age the gauges report. It is safe on a nil receiver and from any goroutine.
func (i *Instruments) RecordSupplyChainImpactWinnersDirtyLog(depth int64, oldest time.Duration) {
	if i == nil || i.supplyChainImpactWinnersDirtyLog == nil {
		return
	}
	d := i.supplyChainImpactWinnersDirtyLog
	d.depth.Store(depth)
	d.oldestAgeNano.Store(int64(oldest))
	d.sampled.Store(true)
}

// RecordSupplyChainImpactWinnersTickPhase records the wall time one phase of a
// keyed winners tick spent, whether the phase succeeded or failed (#9975). phase
// must be one of the SupplyChainImpactWinnersPhase* constants. It is safe on a
// nil receiver and from any goroutine.
func (i *Instruments) RecordSupplyChainImpactWinnersTickPhase(ctx context.Context, phase string, spent time.Duration) {
	if i == nil || i.SupplyChainImpactWinnersTickPhaseDuration == nil {
		return
	}
	i.SupplyChainImpactWinnersTickPhaseDuration.Record(ctx, spent.Seconds(), metric.WithAttributes(AttrPhase(phase)))
}

// RecordSupplyChainImpactWinnersCapture counts rows appended to the winners
// dirty log under source: by the impact writer at capture time, or by the
// maintainer at consume time for the trigger-owned sources. source must be one of the
// SupplyChainImpactWinnersSource* constants. A zero count records nothing, so a
// writer that captured nothing leaves its source's counter flat, which is the
// signal for a capture path that stopped. It is safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactWinnersCapture(ctx context.Context, source string, rows int64) {
	if i == nil || i.SupplyChainImpactWinnersCaptureRows == nil || rows <= 0 {
		return
	}
	i.SupplyChainImpactWinnersCaptureRows.Add(ctx, rows, metric.WithAttributes(AttrSource(source)))
}
