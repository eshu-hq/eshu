// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestNewMetricsPublishesWorkItemRetentionSignals(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder, err := NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics() error = %v, want nil", err)
	}
	retention, ok := recorder.(WorkItemRetentionMetrics)
	if !ok {
		t.Fatal("NewMetrics() recorder does not implement WorkItemRetentionMetrics")
	}

	retention.RecordWorkItemRetention(context.Background(), WorkItemRetentionObservation{
		Outcome:  workItemRetentionOutcomeSuccess,
		Duration: 2 * time.Second,
		Result: workflow.WorkItemRetentionResult{
			Batches: 2, DeletedCompleted: 40, DeletedFailedTerminal: 7,
			BatchDurations: []time.Duration{20 * time.Millisecond, 30 * time.Millisecond},
		},
		TableSize:      workflow.WorkItemTableSize{Bytes: 1 << 20, EstimatedRows: 9000},
		TableSizeKnown: true,
	})
	// A failed pass still counts its committed deletes but keeps the last
	// known table size when its catalog read failed.
	retention.RecordWorkItemRetention(context.Background(), WorkItemRetentionObservation{
		Outcome:  workItemRetentionOutcomeError,
		Duration: time.Second,
		Result:   workflow.WorkItemRetentionResult{Batches: 1, DeletedCompleted: 5},
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := coordinatorCounterValue(t, rm, "eshu_dp_workflow_coordinator_work_item_retention_passes_total",
		map[string]string{"outcome": "success"}); got != 1 {
		t.Fatalf("passes{success} = %d, want 1", got)
	}
	if got := coordinatorCounterValue(t, rm, "eshu_dp_workflow_coordinator_work_item_retention_passes_total",
		map[string]string{"outcome": "error"}); got != 1 {
		t.Fatalf("passes{error} = %d, want 1", got)
	}
	if got := coordinatorCounterValue(t, rm, "eshu_dp_workflow_coordinator_work_item_retention_deleted_total",
		map[string]string{"status": "completed"}); got != 45 {
		t.Fatalf("deleted{completed} = %d, want 45", got)
	}
	if got := coordinatorCounterValue(t, rm, "eshu_dp_workflow_coordinator_work_item_retention_deleted_total",
		map[string]string{"status": "failed_terminal"}); got != 7 {
		t.Fatalf("deleted{failed_terminal} = %d, want 7", got)
	}
	if got := coordinatorHistogramCount(t, rm, "eshu_dp_workflow_coordinator_work_item_retention_batch_duration_seconds",
		map[string]string{}); got != 2 {
		t.Fatalf("batch duration observations = %d, want 2", got)
	}
	if got := coordinatorHistogramCount(t, rm, "eshu_dp_workflow_coordinator_work_item_retention_pass_duration_seconds",
		map[string]string{"outcome": "success"}); got != 1 {
		t.Fatalf("pass duration{success} observations = %d, want 1", got)
	}
	if got := coordinatorGaugeValue(t, rm, "eshu_dp_workflow_coordinator_work_items_table_bytes"); got != 1<<20 {
		t.Fatalf("table bytes = %d, want %d", got, 1<<20)
	}
	if got := coordinatorGaugeValue(t, rm, "eshu_dp_workflow_coordinator_work_items_estimated_rows"); got != 9000 {
		t.Fatalf("estimated rows = %d, want 9000", got)
	}
}
