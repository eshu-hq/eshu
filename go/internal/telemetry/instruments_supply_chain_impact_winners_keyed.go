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

// Write-conflict sites for
// eshu_dp_supply_chain_impact_winners_write_conflicts_total (#10851). The
// value set is closed: the SupplyChainImpactWinnersConflictSite* constants.
const (
	// SupplyChainImpactWinnersConflictSiteWriterCapture is the writer's
	// capture statement (fact_upsert staging, fact_retract staging).
	SupplyChainImpactWinnersConflictSiteWriterCapture = "writer_capture"
	// SupplyChainImpactWinnersConflictSiteWriterCommit is the writer
	// transaction's commit.
	SupplyChainImpactWinnersConflictSiteWriterCommit = "writer_commit"
	// SupplyChainImpactWinnersConflictSiteTick is any phase of a keyed
	// tick: the claim, the collapse, the consume, the build, the apply,
	// the stamp, or the commit.
	SupplyChainImpactWinnersConflictSiteTick = "tick"
)

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
	// SupplyChainImpactWinnersPhaseConsume admits the oldest dirty-log window
	// into the consumed temp table (#9805 chunked consume). The per-source
	// tally and the delete of the applied rows run under the apply phase, once
	// the chunk bound has decided which admitted rows the tick applies.
	SupplyChainImpactWinnersPhaseConsume = "consume"
	// SupplyChainImpactWinnersPhaseCollapse folds the shared staging queue
	// into the dirty log (#10837 direction C): one atomic key-ordered
	// GROUP BY merge-upsert plus the seq-bounded delete, under the resweep
	// lock before the tick-clock read. A slow collapse reads as a slow
	// staging drain, not as a slow read or write.
	SupplyChainImpactWinnersPhaseCollapse = "collapse"
	// SupplyChainImpactWinnersPhaseBuildIdents is the identifier set built from
	// the consumed rows, its ANALYZE, and the cap check on it.
	SupplyChainImpactWinnersPhaseBuildIdents = "build_idents"
	// SupplyChainImpactWinnersPhaseBuildActiveFacts is the keyed active_facts
	// temp table (the probe into fact_records), the operator facts added to it,
	// and its ANALYZE.
	SupplyChainImpactWinnersPhaseBuildActiveFacts = "build_active_facts"
	// SupplyChainImpactWinnersPhaseApply converts a partially applied scope
	// row's remainder to fact rows, lock-then-deletes exactly the admitted
	// dirty rows this chunk applied in the global key order (tallying their
	// dirty_version sums by capture source; a re-marked key keeps its bumped
	// version for the next tick), and runs the winner select and the row
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

// supplyChainImpactWinnersStaging holds the last staging-queue depth and
// oldest-row age a tick sampled (#10837 direction C), for the observable
// staging gauges to report at scrape time.
type supplyChainImpactWinnersStaging struct {
	depth         atomic.Int64
	oldestAgeNano atomic.Int64
	upperBound    atomic.Bool
	sampled       atomic.Bool
}

// registerSupplyChainImpactWinnersKeyedInstruments registers the keyed-resweep
// signals of #9805 on inst.
//
//   - eshu_dp_supply_chain_impact_winners_keys_per_sweep: canonical keys a keyed
//     tick recomputed. A value near the cap is the signal before a tick falls
//     back to the full rebuild.
//
//   - eshu_dp_supply_chain_impact_winners_identifiers_consumed_total: dirty-log
//     identifier rows a keyed tick consumed.
//
//   - eshu_dp_supply_chain_impact_winners_capture_rows_total{source}: dirty-log
//     marks appended to the log, by capture source. The impact writer counts
//     fact_upsert and fact_retract once their transaction commits; the maintainer counts
//     activation, operator and scanner (database triggers cannot emit a metric)
//     when it consumes them, summing dirty_version so one consumed row counts
//     every mark it collapsed. A source that goes flat while the others move is a
//     capture path that stopped.
//
//   - eshu_dp_supply_chain_impact_winners_backstop_repairs_total: stale keys the
//     full-recompute audit queued. Non-zero means a change class left no
//     capture row; steady state is zero.
//
//   - eshu_dp_supply_chain_impact_winners_audit_skipped_total: audits that
//     could not take the resweep lock, so verified nothing (#9957). A streak
//     means the backstop is not running.
//
//   - eshu_dp_supply_chain_impact_winners_dirty_log_depth and
//     ..._dirty_log_oldest_age_seconds: the dirty log's backlog and how long its
//     oldest row has waited, sampled every tick.
//
//   - eshu_dp_supply_chain_impact_winners_chunked_ticks_total: keyed ticks
//     that stopped at the chunk bound with dirty rows still queued and so
//     stamped no watermark. Rising with a falling depth is a healthy drain.
//
//   - eshu_dp_supply_chain_impact_winners_tick_phase_duration_seconds{phase}:
//     wall time of each phase of one keyed tick, so a slow tick reads as a slow
//     read (build_idents, build_active_facts), a slow write (apply, commit), or
//     a slow consume. A phase that fails still records the time it spent.
//
//   - eshu_dp_supply_chain_impact_winners_write_conflicts_total{site,
//     failure_class}: writer and tick statements the database aborted with a
//     deadlock or a serialization failure. The victim is classified only;
//     nothing retries.
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
		metric.WithDescription("Supply-chain impact winners dirty-log marks a keyed resweep consumed: the drain sums dirty_version, so one consumed row counts every mark it collapsed."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersIdentifiersConsumed counter: %w", err)
	}
	if inst.SupplyChainImpactWinnersCaptureRows, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_capture_rows_total",
		metric.WithDescription("Supply-chain impact winners dirty-log marks appended, by source (fact_upsert, fact_retract, activation, operator, scanner, audit). The impact writer counts fact_upsert and fact_retract after its transaction commits, so a rollback adds nothing; the maintainer counts the trigger sources activation, operator and scanner when it consumes them, summing dirty_version so one consumed row counts every mark it collapsed. A source that goes flat while the others move is a capture path that stopped."),
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
	if inst.SupplyChainImpactWinnersAuditRangeDuration, err = meter.Float64Histogram(
		"eshu_dp_supply_chain_impact_winners_audit_range_duration_seconds",
		metric.WithDescription("Wall time of one staged winners audit sweep range, standby read through writer commit, by outcome (advanced, completed, skipped, lock_busy, cursor_race, deferred). The standby statement has a 25 s guard and the writer verify a 30 s bound; a p99 near 55 s means ranges are at their budget."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2.5, 5, 10, 15, 20, 25, 30, 45, 60, 120),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersAuditRangeDuration histogram: %w", err)
	}
	if inst.SupplyChainImpactWinnersChunkedTicks, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_chunked_ticks_total",
		metric.WithDescription("Keyed supply-chain impact winners ticks that stopped at the identifier chunk bound (post-expansion identifiers per transaction) with dirty-log rows still queued, so committed their chunk without stamping the watermark. A rising counter with a falling dirty-log depth is a backlog draining in bounded ticks; a rising counter with a flat or climbing depth means arrivals outpace the bound."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersChunkedTicks counter: %w", err)
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
	inst.supplyChainImpactWinnersStaging = new(supplyChainImpactWinnersStaging)
	staging := inst.supplyChainImpactWinnersStaging
	if inst.SupplyChainImpactWinnersStagingDepth, err = meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_staging_depth",
		metric.WithDescription("Rows waiting in the supply-chain impact winners staging queue at the last keyed tick this process sampled. A depth that keeps climbing means captures outpace the per-tick collapse."),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			if staging.sampled.Load() {
				o.Observe(float64(staging.depth.Load()))
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersStagingDepth gauge: %w", err)
	}
	if inst.SupplyChainImpactWinnersStagingOldestAge, err = meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_staging_oldest_age_seconds",
		metric.WithDescription("Age of the oldest row in the supply-chain impact winners staging queue at the last keyed tick this process sampled, 0 when the queue was empty. It bounds how stale a capture can sit before the collapse folds it in."),
		metric.WithUnit("s"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			if staging.sampled.Load() {
				o.Observe(time.Duration(staging.oldestAgeNano.Load()).Seconds())
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersStagingOldestAge gauge: %w", err)
	}
	if inst.SupplyChainImpactWinnersStagingDepthUpperBound, err = meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_staging_depth_upper_bound",
		metric.WithDescription("1 when the last sampled supply-chain impact winners staging depth, and the dirty-log depth that includes it, is an upper bound (the visible seq span once the staged backlog passes the exact-count cap of one collapse budget), 0 when it is exact. The oldest-age gauges stay exact either way."),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			if staging.sampled.Load() {
				bound := 0.0
				if staging.upperBound.Load() {
					bound = 1
				}
				o.Observe(bound)
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersStagingDepthUpperBound gauge: %w", err)
	}
	if inst.SupplyChainImpactWinnersStagingCollapsed, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_staging_collapsed_rows_total",
		metric.WithDescription("Supply-chain impact winners staged rows one keyed tick collapsed into the dirty log: the seq-bounded delete count, so one collapsed group counts every mark it combined."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersStagingCollapsed counter: %w", err)
	}
	if inst.SupplyChainImpactWinnersScopeRemainderConverted, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_scope_remainder_converted_rows_total",
		metric.WithDescription("Supply-chain impact winners dirty-log fact rows a keyed tick wrote (inserted or merged) when it converted the unapplied remainder of a partly applied scope row. Each converted row keeps its scope row's capture time, so while any is queued the watermark stays under that activation's scope mark: a rising rate with a flat watermark means a large activation is draining over bounded ticks."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersScopeRemainderConverted counter: %w", err)
	}
	if inst.SupplyChainImpactWinnersWriteConflicts, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_write_conflicts_total",
		metric.WithDescription("Supply-chain impact winners writer and tick statements the database aborted with a deadlock (40P01) or a serialization failure (40001), by site (writer_capture, writer_commit, tick) and failure_class (deadlock, serialization_failure). The counter classifies the victim only: nothing retries, the transaction rolls back and the queue re-drives it."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersWriteConflicts counter: %w", err)
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
// dirty log under source: by the impact writer once its transaction commits, or by the
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

// RecordSupplyChainImpactWinnersStaging sets the staging-queue depth and
// oldest-row age the staging gauges report (#10837 direction C), and whether
// the depth is an upper bound (#11129). An empty queue reads as zero depth
// and zero age. It is safe on a nil receiver and from any goroutine.
func (i *Instruments) RecordSupplyChainImpactWinnersStaging(depth int64, oldest time.Duration, upperBound bool) {
	if i == nil || i.supplyChainImpactWinnersStaging == nil {
		return
	}
	s := i.supplyChainImpactWinnersStaging
	s.depth.Store(depth)
	s.oldestAgeNano.Store(int64(oldest))
	s.upperBound.Store(upperBound)
	s.sampled.Store(true)
}

// RecordSupplyChainImpactWinnersStagingCollapsed counts the staged rows one
// keyed tick collapsed into the dirty log (#10837 direction C): the
// seq-bounded delete count, so one collapsed group counts every mark it
// combined. A zero count records nothing. It is safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactWinnersStagingCollapsed(ctx context.Context, rows int64) {
	if i == nil || i.SupplyChainImpactWinnersStagingCollapsed == nil || rows <= 0 {
		return
	}
	i.SupplyChainImpactWinnersStagingCollapsed.Add(ctx, rows)
}

// RecordSupplyChainImpactWinnersScopeRemainderConverted counts the dirty-log
// fact rows one keyed tick's scope-remainder convert wrote (#11145). A zero or
// negative count records nothing. It is safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactWinnersScopeRemainderConverted(ctx context.Context, rows int64) {
	if i == nil || i.SupplyChainImpactWinnersScopeRemainderConverted == nil || rows <= 0 {
		return
	}
	i.SupplyChainImpactWinnersScopeRemainderConverted.Add(ctx, rows)
}

// RecordSupplyChainImpactWinnersWriteConflict counts one writer or tick
// statement the database aborted with a write conflict (#10851). site must
// be one of the SupplyChainImpactWinnersConflictSite* constants and class
// one of deadlock or serialization_failure; an empty class records nothing,
// and an unknown site is dropped so a typo'd caller cannot open a new
// series. It is safe on a nil receiver and from any goroutine.
func (i *Instruments) RecordSupplyChainImpactWinnersWriteConflict(ctx context.Context, site, class string) {
	if i == nil || i.SupplyChainImpactWinnersWriteConflicts == nil || class == "" {
		return
	}
	switch site {
	case SupplyChainImpactWinnersConflictSiteWriterCapture,
		SupplyChainImpactWinnersConflictSiteWriterCommit,
		SupplyChainImpactWinnersConflictSiteTick:
	default:
		return
	}
	i.SupplyChainImpactWinnersWriteConflicts.Add(ctx, 1, metric.WithAttributes(
		attribute.String(MetricDimensionSite, site),
		attribute.String(MetricDimensionFailureClass, class),
	))
}
