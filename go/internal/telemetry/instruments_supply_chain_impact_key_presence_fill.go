// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Step outcomes of eshu_dp_supply_chain_impact_key_presence_fill_steps_total
// (#11626). The value set is closed.
const (
	// SupplyChainImpactKeyPresenceFillStepChunk is a step that filled (or
	// skipped) one chunk of supports whose bits the writer did not fill.
	SupplyChainImpactKeyPresenceFillStepChunk = "chunk"
	// SupplyChainImpactKeyPresenceFillStepVerify is a step that ran a clean
	// verification slice that did not complete the marker.
	SupplyChainImpactKeyPresenceFillStepVerify = "verify"
	// SupplyChainImpactKeyPresenceFillStepGap is a verification slice that found
	// a support whose stored bits disagree with its payload and restarted the
	// sweep instead of completing the marker.
	SupplyChainImpactKeyPresenceFillStepGap = "gap"
	// SupplyChainImpactKeyPresenceFillStepComplete is the step that set the
	// marker.
	SupplyChainImpactKeyPresenceFillStepComplete = "complete"
	// SupplyChainImpactKeyPresenceFillStepFailed is a step that returned an
	// error.
	SupplyChainImpactKeyPresenceFillStepFailed = "failed"
)

// Row outcomes of eshu_dp_supply_chain_impact_key_presence_fill_rows_total.
const (
	// SupplyChainImpactKeyPresenceFillRowsFilled counts supports given correct
	// bits.
	SupplyChainImpactKeyPresenceFillRowsFilled = "filled"
	// SupplyChainImpactKeyPresenceFillRowsSkipped counts supports a chunk left
	// alone because a concurrent writer already filled them.
	SupplyChainImpactKeyPresenceFillRowsSkipped = "skipped"
)

// Pause reasons of eshu_dp_supply_chain_impact_key_presence_fill_pauses_total.
// The value set is closed.
const (
	// SupplyChainImpactKeyPresenceFillPauseReplayLag is a pause because the
	// replica replay lag was over the limit.
	SupplyChainImpactKeyPresenceFillPauseReplayLag = "replay_lag"
	// SupplyChainImpactKeyPresenceFillPauseFreeDisk is a pause because the free
	// disk reading was under the limit.
	SupplyChainImpactKeyPresenceFillPauseFreeDisk = "free_disk"
	// SupplyChainImpactKeyPresenceFillPauseDiskUnreadable is a pause because a
	// free disk limit is set and no free disk reading is available.
	SupplyChainImpactKeyPresenceFillPauseDiskUnreadable = "disk_unreadable"
	// SupplyChainImpactKeyPresenceFillPauseIndexBuild is a pause because an
	// index build was running (pg_stat_progress_create_index not empty).
	SupplyChainImpactKeyPresenceFillPauseIndexBuild = "index_build"
	// SupplyChainImpactKeyPresenceFillPauseProbeError is a pause because a pause
	// condition could not be read; an unreadable condition is never taken as
	// clear.
	SupplyChainImpactKeyPresenceFillPauseProbeError = "probe_error"
)

func registerSupplyChainImpactKeyPresenceFillInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SupplyChainImpactKeyPresenceFillSteps, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_key_presence_fill_steps_total",
		metric.WithDescription("Key-presence fill steps by outcome (chunk, verify, gap, complete, failed). Each chunk step updates at most 500 supports of one scope generation under its conflict-domain lock; a gap step found disagreeing bits and restarted the sweep instead of completing the marker."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactKeyPresenceFillSteps counter: %w", err)
	}
	if inst.SupplyChainImpactKeyPresenceFillRows, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_key_presence_fill_rows_total",
		metric.WithDescription("Supports the key-presence fill gave correct bits (filled) or left alone because a concurrent writer already filled them (skipped)."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactKeyPresenceFillRows counter: %w", err)
	}
	if inst.SupplyChainImpactKeyPresenceFillPauses, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_key_presence_fill_pauses_total",
		metric.WithDescription("Key-presence fill passes held back by a pause condition, by reason (replay_lag, free_disk, disk_unreadable, index_build, probe_error)."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactKeyPresenceFillPauses counter: %w", err)
	}
	if inst.SupplyChainImpactKeyPresenceFillComplete, err = meter.Int64Gauge(
		"eshu_dp_supply_chain_impact_key_presence_fill_complete",
		metric.WithDescription("1 once the key-presence fill marker is set (a clean verification found no support whose stored bits disagree with its payload), else 0."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactKeyPresenceFillComplete gauge: %w", err)
	}
	return nil
}

// RecordSupplyChainImpactKeyPresenceFillStep counts one fill step under outcome
// (one of the SupplyChainImpactKeyPresenceFillStep* constants). It is safe on a
// nil receiver and on an Instruments that did not register the counter.
func (i *Instruments) RecordSupplyChainImpactKeyPresenceFillStep(ctx context.Context, outcome string) {
	if i == nil || i.SupplyChainImpactKeyPresenceFillSteps == nil {
		return
	}
	i.SupplyChainImpactKeyPresenceFillSteps.Add(ctx, 1, metric.WithAttributes(AttrOutcome(outcome)))
}

// RecordSupplyChainImpactKeyPresenceFillRows counts rows under outcome (filled
// or skipped); a zero or negative count records nothing. Nil-safe.
func (i *Instruments) RecordSupplyChainImpactKeyPresenceFillRows(ctx context.Context, outcome string, rows int64) {
	if i == nil || i.SupplyChainImpactKeyPresenceFillRows == nil || rows <= 0 {
		return
	}
	i.SupplyChainImpactKeyPresenceFillRows.Add(ctx, rows, metric.WithAttributes(AttrOutcome(outcome)))
}

// RecordSupplyChainImpactKeyPresenceFillPause counts one held-back pass under
// reason (one of the SupplyChainImpactKeyPresenceFillPause* constants).
// Nil-safe.
func (i *Instruments) RecordSupplyChainImpactKeyPresenceFillPause(ctx context.Context, reason string) {
	if i == nil || i.SupplyChainImpactKeyPresenceFillPauses == nil {
		return
	}
	i.SupplyChainImpactKeyPresenceFillPauses.Add(ctx, 1, metric.WithAttributes(AttrReason(reason)))
}

// RecordSupplyChainImpactKeyPresenceFillComplete sets the marker-complete
// gauge. Nil-safe.
func (i *Instruments) RecordSupplyChainImpactKeyPresenceFillComplete(ctx context.Context, complete bool) {
	if i == nil || i.SupplyChainImpactKeyPresenceFillComplete == nil {
		return
	}
	var value int64
	if complete {
		value = 1
	}
	i.SupplyChainImpactKeyPresenceFillComplete.Record(ctx, value)
}
