// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Per-support table row actions for eshu_dp_supply_chain_impact_supports_rows_total.
// They are the closed set of the action label.
const (
	// SupplyChainImpactSupportsActionInserted counts a support row created.
	SupplyChainImpactSupportsActionInserted = "inserted"
	// SupplyChainImpactSupportsActionUpdated counts a support row whose derived
	// columns changed and was rewritten.
	SupplyChainImpactSupportsActionUpdated = "updated"
	// SupplyChainImpactSupportsActionUnchanged counts a support the write-on-change
	// guard left alone (a replay of an unchanged finding).
	SupplyChainImpactSupportsActionUnchanged = "unchanged"
	// SupplyChainImpactSupportsActionDeleted counts a support row removed with
	// the fact a retraction tombstoned.
	SupplyChainImpactSupportsActionDeleted = "deleted"
)

// Per-support table writers for the source label of
// eshu_dp_supply_chain_impact_supports_rows_total.
const (
	// SupplyChainImpactSupportsSourceWriter is the impact writer's transaction.
	SupplyChainImpactSupportsSourceWriter = "writer"
	// SupplyChainImpactSupportsSourceBackfill is the reducer-owned backfill.
	SupplyChainImpactSupportsSourceBackfill = "backfill"
)

// Supports backfill step outcomes for
// eshu_dp_supply_chain_impact_supports_backfill_steps_total. They are the closed
// set of the outcome label.
const (
	// SupplyChainImpactSupportsBackfillStepChunk is a step that wrote or cleaned
	// supports of one (scope, generation).
	SupplyChainImpactSupportsBackfillStepChunk = "chunk"
	// SupplyChainImpactSupportsBackfillStepGap is a verification that found live
	// facts without a support and restarted the sweep.
	SupplyChainImpactSupportsBackfillStepGap = "verify_gap"
	// SupplyChainImpactSupportsBackfillStepComplete is the step that set the
	// readiness marker.
	SupplyChainImpactSupportsBackfillStepComplete = "complete"
	// SupplyChainImpactSupportsBackfillStepIdle is a step after completion.
	SupplyChainImpactSupportsBackfillStepIdle = "idle"
	// SupplyChainImpactSupportsBackfillStepContended is a pass another reducer
	// replica held the backfill lock for.
	SupplyChainImpactSupportsBackfillStepContended = "contended"
	// SupplyChainImpactSupportsBackfillStepFailed is a step that returned an error.
	SupplyChainImpactSupportsBackfillStepFailed = "failed"
)

// registerSupplyChainImpactSupportsInstruments registers the per-support table
// signals of #10880 PR-B1 on inst.
//
//   - eshu_dp_supply_chain_impact_supports_rows_total{action,source}: support
//     rows the impact writer's transaction (counted after it commits, so a
//     rollback adds nothing) or the backfill wrote. A writer whose inserted and
//     updated stay flat while findings are written is a maintainer that stopped.
//
//   - eshu_dp_supply_chain_impact_supports_backfill_steps_total{outcome}: backfill
//     steps by outcome. A rising verify_gap count means supports are going
//     missing after the sweep passed them (a writer without the maintainer).
//
//   - eshu_dp_supply_chain_impact_supports_backfill_ready: 1 once the readiness
//     marker is set at the current contract version, else 0.
//
// No label carries a scope, repository, canonical key or CVE.
func registerSupplyChainImpactSupportsInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SupplyChainImpactSupportsRows, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_supports_rows_total",
		metric.WithDescription("Per-support table rows written, by action (inserted, updated, unchanged, deleted) and source (writer, backfill). The impact writer's rows count after its transaction commits."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactSupportsRows counter: %w", err)
	}
	if inst.SupplyChainImpactSupportsBackfillSteps, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_supports_backfill_steps_total",
		metric.WithDescription("Per-support backfill steps by outcome (chunk, verify_gap, complete, idle, contended, failed). A rising verify_gap count means supports went missing after the sweep passed them."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactSupportsBackfillSteps counter: %w", err)
	}
	if inst.SupplyChainImpactSupportsBackfillReady, err = meter.Int64Gauge(
		"eshu_dp_supply_chain_impact_supports_backfill_ready",
		metric.WithDescription("1 once the per-support backfill finished and verified at the current contract version, else 0."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactSupportsBackfillReady gauge: %w", err)
	}
	return nil
}

// RecordSupplyChainImpactSupportsRows counts support rows written under action
// by source. A zero count records nothing. It is safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactSupportsRows(ctx context.Context, action, source string, rows int64) {
	if i == nil || i.SupplyChainImpactSupportsRows == nil || rows <= 0 {
		return
	}
	i.SupplyChainImpactSupportsRows.Add(ctx, rows, metric.WithAttributes(AttrAction(action), AttrSource(source)))
}

// RecordSupplyChainImpactSupportsBackfillStep counts one backfill step under
// outcome, which must be one of the SupplyChainImpactSupportsBackfillStep*
// constants. It is safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactSupportsBackfillStep(ctx context.Context, outcome string) {
	if i == nil || i.SupplyChainImpactSupportsBackfillSteps == nil {
		return
	}
	i.SupplyChainImpactSupportsBackfillSteps.Add(ctx, 1, metric.WithAttributes(AttrOutcome(outcome)))
}

// RecordSupplyChainImpactSupportsBackfillReady sets the readiness gauge. It is
// safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactSupportsBackfillReady(ctx context.Context, ready bool) {
	if i == nil || i.SupplyChainImpactSupportsBackfillReady == nil {
		return
	}
	var value int64
	if ready {
		value = 1
	}
	i.SupplyChainImpactSupportsBackfillReady.Record(ctx, value)
}
