// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Step outcomes of eshu_dp_supply_chain_impact_stable_key_fill_steps_total (#10880
// PR-C1k). The value set is closed.
const (
	// SupplyChainImpactStableKeyFillStepChunk is a step that filled (or skipped)
	// one chunk of keyless supports.
	SupplyChainImpactStableKeyFillStepChunk = "chunk"
	// SupplyChainImpactStableKeyFillStepVerify is a step that ran a clean
	// verification slice that did not complete the marker.
	SupplyChainImpactStableKeyFillStepVerify = "verify"
	// SupplyChainImpactStableKeyFillStepGap is a verification slice that found a
	// keyless support whose twin has a key and restarted the sweep.
	SupplyChainImpactStableKeyFillStepGap = "gap"
	// SupplyChainImpactStableKeyFillStepComplete is the step that set the marker.
	SupplyChainImpactStableKeyFillStepComplete = "complete"
	// SupplyChainImpactStableKeyFillStepFailed is a step that returned an error.
	SupplyChainImpactStableKeyFillStepFailed = "failed"
)

// Row outcomes of eshu_dp_supply_chain_impact_stable_key_fill_rows_total.
const (
	// SupplyChainImpactStableKeyFillRowsFilled counts supports given a key.
	SupplyChainImpactStableKeyFillRowsFilled = "filled"
	// SupplyChainImpactStableKeyFillRowsSkipped counts keyless supports a chunk
	// left alone because their fact twin has no key.
	SupplyChainImpactStableKeyFillRowsSkipped = "skipped"
)

// Pause reasons of eshu_dp_supply_chain_impact_stable_key_fill_pauses_total. The
// value set is closed.
const (
	// SupplyChainImpactStableKeyFillPauseReplayLag is a pause because the replica
	// replay lag was over the limit.
	SupplyChainImpactStableKeyFillPauseReplayLag = "replay_lag"
	// SupplyChainImpactStableKeyFillPauseFreeDisk is a pause because the free disk
	// reading was under the limit.
	SupplyChainImpactStableKeyFillPauseFreeDisk = "free_disk"
	// SupplyChainImpactStableKeyFillPauseDiskUnreadable is a pause because a free
	// disk limit is set and no free disk reading is available.
	SupplyChainImpactStableKeyFillPauseDiskUnreadable = "disk_unreadable"
	// SupplyChainImpactStableKeyFillPauseIndexBuild is a pause because an index
	// build was running (pg_stat_progress_create_index not empty).
	SupplyChainImpactStableKeyFillPauseIndexBuild = "index_build"
	// SupplyChainImpactStableKeyFillPauseProbeError is a pause because a pause
	// condition could not be read; an unreadable condition is never taken as clear.
	SupplyChainImpactStableKeyFillPauseProbeError = "probe_error"
)

func registerSupplyChainImpactStableKeyFillInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SupplyChainImpactStableKeyFillSteps, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_stable_key_fill_steps_total",
		metric.WithDescription("Stable-key fill steps by outcome (chunk, verify, gap, complete, failed). Each chunk step updates at most 500 supports of one scope generation under its conflict-domain lock."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactStableKeyFillSteps counter: %w", err)
	}
	if inst.SupplyChainImpactStableKeyFillRows, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_stable_key_fill_rows_total",
		metric.WithDescription("Supports the stable-key fill gave a key (filled) or left keyless because their fact twin has no key (skipped)."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactStableKeyFillRows counter: %w", err)
	}
	if inst.SupplyChainImpactStableKeyFillPauses, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_stable_key_fill_pauses_total",
		metric.WithDescription("Stable-key fill passes held back by a pause condition, by reason (replay_lag, free_disk, disk_unreadable, index_build, probe_error)."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactStableKeyFillPauses counter: %w", err)
	}
	if inst.SupplyChainImpactStableKeyFillComplete, err = meter.Int64Gauge(
		"eshu_dp_supply_chain_impact_stable_key_fill_complete",
		metric.WithDescription("1 once the stable-key fill marker is set (a clean verification found no keyless support whose twin has a key), else 0."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactStableKeyFillComplete gauge: %w", err)
	}
	return nil
}

// RecordSupplyChainImpactStableKeyFillStep counts one fill step under outcome
// (one of the SupplyChainImpactStableKeyFillStep* constants). It is safe on a nil
// receiver and on an Instruments that did not register the counter.
func (i *Instruments) RecordSupplyChainImpactStableKeyFillStep(ctx context.Context, outcome string) {
	if i == nil || i.SupplyChainImpactStableKeyFillSteps == nil {
		return
	}
	i.SupplyChainImpactStableKeyFillSteps.Add(ctx, 1, metric.WithAttributes(AttrOutcome(outcome)))
}

// RecordSupplyChainImpactStableKeyFillRows counts rows under outcome (filled or
// skipped); a zero or negative count records nothing. Nil-safe.
func (i *Instruments) RecordSupplyChainImpactStableKeyFillRows(ctx context.Context, outcome string, rows int64) {
	if i == nil || i.SupplyChainImpactStableKeyFillRows == nil || rows <= 0 {
		return
	}
	i.SupplyChainImpactStableKeyFillRows.Add(ctx, rows, metric.WithAttributes(AttrOutcome(outcome)))
}

// RecordSupplyChainImpactStableKeyFillPause counts one held-back pass under reason
// (one of the SupplyChainImpactStableKeyFillPause* constants). Nil-safe.
func (i *Instruments) RecordSupplyChainImpactStableKeyFillPause(ctx context.Context, reason string) {
	if i == nil || i.SupplyChainImpactStableKeyFillPauses == nil {
		return
	}
	i.SupplyChainImpactStableKeyFillPauses.Add(ctx, 1, metric.WithAttributes(AttrReason(reason)))
}

// RecordSupplyChainImpactStableKeyFillComplete sets the marker-complete gauge.
// Nil-safe.
func (i *Instruments) RecordSupplyChainImpactStableKeyFillComplete(ctx context.Context, complete bool) {
	if i == nil || i.SupplyChainImpactStableKeyFillComplete == nil {
		return
	}
	var value int64
	if complete {
		value = 1
	}
	i.SupplyChainImpactStableKeyFillComplete.Record(ctx, value)
}
