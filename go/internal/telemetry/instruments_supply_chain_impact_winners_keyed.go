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
//     fact_upsert and fact_retract when it appends them; the maintainer counts
//     activation, operator and scanner (database triggers cannot emit a metric)
//     when it consumes them. A source that goes flat while the others move is a
//     capture path that stopped.
//   - eshu_dp_supply_chain_impact_winners_backstop_repairs_total: stale keys the
//     full-recompute audit queued. Non-zero means a change class left no
//     capture row; steady state is zero.
//   - eshu_dp_supply_chain_impact_winners_dirty_log_depth and
//     ..._dirty_log_oldest_age_seconds: the dirty log's backlog and how long its
//     oldest row has waited, sampled every tick.
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
		metric.WithDescription("Supply-chain impact winners dirty-log rows appended, by source (fact_upsert, fact_retract, activation, operator, scanner, audit). The impact writer counts fact_upsert and fact_retract at capture; the maintainer counts the trigger sources activation, operator and scanner when it consumes them. A source that goes flat while the others move is a capture path that stopped."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersCaptureRows counter: %w", err)
	}
	if inst.SupplyChainImpactWinnersBackstopRepairs, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_backstop_repairs_total",
		metric.WithDescription("Stale supply-chain impact winner keys the full-recompute audit found and queued for the next keyed tick. Steady state is zero; a non-zero rate means a change class left no dirty-log row."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersBackstopRepairs counter: %w", err)
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
