// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// workItemRetentionBatchBuckets span one batch transaction's wall time, which
// is also its row-lock hold. The #8829 EXPLAIN measured 14 to 16 ms per
// 1,000-row batch; a batch near the 30 s statement timeout is a regressed plan.
var workItemRetentionBatchBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

// workItemRetentionPassBuckets span one pass: up to 100 batches by default.
var workItemRetentionPassBuckets = []float64{0.01, 0.05, 0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 300}

// workItemRetentionInstruments holds the workflow_work_items retention
// instruments (#8829). The names are literal so the telemetry coverage gate
// can see their registration.
type workItemRetentionInstruments struct {
	passes        metric.Int64Counter
	passDuration  metric.Float64Histogram
	deleted       metric.Int64Counter
	batchDuration metric.Float64Histogram

	// tableSizeKnown is set by the first pass whose catalog read succeeded.
	// Until then the gauges export no data point: dark mode, retention
	// turned off, and the time before the first pass must not read as an
	// empty table.
	tableSizeKnown     atomic.Bool
	tableBytes         atomic.Int64
	tableEstimatedRows atomic.Int64
}

// newWorkItemRetentionInstruments registers the retention counters,
// histograms and the table size gauges on meter.
func newWorkItemRetentionInstruments(meter metric.Meter) (*workItemRetentionInstruments, error) {
	inst := &workItemRetentionInstruments{}
	var err error
	if inst.passes, err = meter.Int64Counter(
		"eshu_dp_workflow_coordinator_work_item_retention_passes_total",
		metric.WithDescription("workflow_work_items retention passes, by outcome"),
	); err != nil {
		return nil, fmt.Errorf("register work item retention passes counter: %w", err)
	}
	if inst.passDuration, err = meter.Float64Histogram(
		"eshu_dp_workflow_coordinator_work_item_retention_pass_duration_seconds",
		metric.WithDescription("workflow_work_items retention pass wall time, by outcome"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(workItemRetentionPassBuckets...),
	); err != nil {
		return nil, fmt.Errorf("register work item retention pass duration histogram: %w", err)
	}
	if inst.deleted, err = meter.Int64Counter(
		"eshu_dp_workflow_coordinator_work_item_retention_deleted_total",
		metric.WithDescription("workflow_work_items rows deleted by retention, by status (completed, failed_terminal)"),
		metric.WithUnit("{row}"),
	); err != nil {
		return nil, fmt.Errorf("register work item retention deleted counter: %w", err)
	}
	if inst.batchDuration, err = meter.Float64Histogram(
		"eshu_dp_workflow_coordinator_work_item_retention_batch_duration_seconds",
		metric.WithDescription("One workflow_work_items retention batch transaction, which is also its row-lock hold"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(workItemRetentionBatchBuckets...),
	); err != nil {
		return nil, fmt.Errorf("register work item retention batch duration histogram: %w", err)
	}
	tableBytes, err := meter.Int64ObservableGauge(
		"eshu_dp_workflow_coordinator_work_items_table_bytes",
		metric.WithDescription("workflow_work_items total relation size (heap, indexes, TOAST) at the last retention pass"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("register work items table bytes gauge: %w", err)
	}
	tableRows, err := meter.Int64ObservableGauge(
		"eshu_dp_workflow_coordinator_work_items_estimated_rows",
		metric.WithDescription("workflow_work_items planner live-row estimate (pg_class.reltuples) at the last retention pass"),
		metric.WithUnit("{row}"),
	)
	if err != nil {
		return nil, fmt.Errorf("register work items estimated rows gauge: %w", err)
	}
	if _, err := meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		if !inst.tableSizeKnown.Load() {
			return nil
		}
		observer.ObserveInt64(tableBytes, inst.tableBytes.Load())
		observer.ObserveInt64(tableRows, inst.tableEstimatedRows.Load())
		return nil
	}, tableBytes, tableRows); err != nil {
		return nil, fmt.Errorf("register work items table size callback: %w", err)
	}
	return inst, nil
}

// RecordWorkItemRetention records one retention pass. Deleted rows count on
// both outcomes, because a failed pass still committed its earlier batches.
func (m *otelMetrics) RecordWorkItemRetention(ctx context.Context, observation WorkItemRetentionObservation) {
	if m == nil || m.workItemRetention == nil {
		return
	}
	inst := m.workItemRetention
	outcome := observation.Outcome
	if outcome != workItemRetentionOutcomeSuccess && outcome != workItemRetentionOutcomeCanceled {
		outcome = workItemRetentionOutcomeError
	}
	outcomeAttrs := metric.WithAttributes(attribute.String(telemetry.MetricDimensionOutcome, outcome))
	inst.passes.Add(ctx, 1, outcomeAttrs)
	inst.passDuration.Record(ctx, observation.Duration.Seconds(), outcomeAttrs)
	if deleted := observation.Result.DeletedCompleted; deleted > 0 {
		inst.deleted.Add(ctx, int64(deleted),
			metric.WithAttributes(attribute.String(telemetry.MetricDimensionStatus, "completed")))
	}
	if deleted := observation.Result.DeletedFailedTerminal; deleted > 0 {
		inst.deleted.Add(ctx, int64(deleted),
			metric.WithAttributes(attribute.String(telemetry.MetricDimensionStatus, "failed_terminal")))
	}
	for _, batch := range observation.Result.BatchDurations {
		inst.batchDuration.Record(ctx, batch.Seconds())
	}
	if observation.TableSizeKnown {
		inst.tableBytes.Store(observation.TableSize.Bytes)
		inst.tableEstimatedRows.Store(observation.TableSize.EstimatedRows)
		inst.tableSizeKnown.Store(true)
	}
}
