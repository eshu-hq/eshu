// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"slices"

	"go.opentelemetry.io/otel/metric"
)

// Closed plan-row outcomes (eshu-hq/eshu#10828): the outcome label of
// eshu_dp_nvd_baseline_plan_rows_total, counted once per guarded admission by
// the workflow store after the commit.
const (
	// NVDBaselinePlanRowRecorded: the admission inserted the chunk's work item
	// and wrote its per-year plan row in the same transaction.
	NVDBaselinePlanRowRecorded = "recorded"
	// NVDBaselinePlanRowNotInserted: the admission was handed a plan for a
	// chunk whose work item it did not insert, because the open-target guard,
	// the terminal-run skip, or the insert dropped it. No plan row is written:
	// a row without its work item would read as a chunk in flight that does
	// not exist.
	NVDBaselinePlanRowNotInserted = "not_inserted"
)

var nvdBaselinePlanRowOutcomes = [...]string{
	NVDBaselinePlanRowRecorded,
	NVDBaselinePlanRowNotInserted,
}

// NVDBaselinePlanRowOutcomes returns the closed, ordered plan-row outcome set.
func NVDBaselinePlanRowOutcomes() []string {
	return slices.Clone(nvdBaselinePlanRowOutcomes[:])
}

// RecordNVDBaselinePlanRows counts the plan rows one guarded admission wrote
// (recorded) and the plans it was handed for chunks it did not insert
// (not_inserted). Each count is added whole, so one admission is one call, and
// a zero or negative count adds no series. Labels stay outcome-only: no run,
// year, or scope becomes a label. Nil instruments or a nil counter skip the
// signal.
func (inst *Instruments) RecordNVDBaselinePlanRows(ctx context.Context, recorded, notInserted int64) {
	if inst == nil || inst.NVDBaselinePlanRows == nil {
		return
	}
	if recorded > 0 {
		inst.NVDBaselinePlanRows.Add(ctx, recorded, metric.WithAttributes(AttrOutcome(NVDBaselinePlanRowRecorded)))
	}
	if notInserted > 0 {
		inst.NVDBaselinePlanRows.Add(ctx, notInserted, metric.WithAttributes(AttrOutcome(NVDBaselinePlanRowNotInserted)))
	}
}
