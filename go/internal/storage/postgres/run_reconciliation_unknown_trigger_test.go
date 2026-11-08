// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const unknownTriggerSkipsMetric = "eshu_dp_workflow_run_unknown_trigger_skips_total"

// TestReconcileSkipsUnknownTriggerRunAndCompletesHealthyRuns is the #10369
// regression: one workflow_runs row with a trigger kind outside the closed
// TriggerKind set must not abort the whole ReconcileWorkflowRuns pass. The
// poison row sorts first (updated_at ASC, as in production), yet the healthy
// run behind it still completes in the same pass while the skip is counted
// on a bounded counter instead of surfacing as an error.
func TestReconcileSkipsUnknownTriggerRunAndCompletesHealthyRuns(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 8, 0, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{
			{
				"run-poison",
				"synthetic-test-10001",
				string(workflow.RunStatusCollectionPending),
				"[]",
				sql.NullString{},
				now.Add(-time.Hour),
				now.Add(-time.Hour),
				sql.NullTime{},
			},
			{
				"run-healthy",
				string(workflow.TriggerKindSchedule),
				string(workflow.RunStatusReducerConverging),
				"[]",
				sql.NullString{String: string(scope.CollectorAWS), Valid: true},
				now.Add(-time.Minute),
				now.Add(-time.Minute),
				sql.NullTime{},
			},
		}},
		{rows: [][]any{{
			string(scope.CollectorAWS),
			19,
			0,
			0,
			19,
			0,
			0,
		}}},
		{rows: nil},
		{rows: nil},
	}}

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("unknown-trigger-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	store := NewWorkflowControlStore(db)
	store.Instruments = instruments

	reconciled, err := store.ReconcileWorkflowRuns(context.Background(), now)
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil (poison row must not abort the pass)", err)
	}
	if got, want := reconciled, 1; got != want {
		t.Fatalf("reconciled = %d, want %d (healthy run completes in the same pass)", got, want)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d (only the healthy run writes; the poison row is never touched)", got, want)
	}
	if !strings.Contains(db.execs[0].query, "UPDATE workflow_runs") {
		t.Fatalf("first exec missing workflow_runs update: %s", db.execs[0].query)
	}
	if got, want := db.execs[0].args[0], "run-healthy"; got != want {
		t.Fatalf("updated run = %v, want %q", got, want)
	}
	if got, want := db.execs[0].args[1], string(workflow.RunStatusComplete); got != want {
		t.Fatalf("updated status = %v, want %q", got, want)
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got, want := counterTotal(collected, unknownTriggerSkipsMetric), int64(1); got != want {
		t.Fatalf("%s = %d, want %d", unknownTriggerSkipsMetric, got, want)
	}
}

// TestReconcileWithOnlyUnknownTriggerRunsReturnsNoError pins the startup
// half of #10369: a pass that finds nothing but poison rows returns zero
// reconciled and a nil error, so the coordinator's initial reconciliation
// (service.go) completes instead of exiting and crash-looping.
func TestReconcileWithOnlyUnknownTriggerRunsReturnsNoError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 8, 0, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{
			"run-poison",
			"synthetic-test-10001",
			string(workflow.RunStatusCollectionPending),
			"[]",
			sql.NullString{},
			now.Add(-time.Hour),
			now.Add(-time.Hour),
			sql.NullTime{},
		}}},
	}}

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("unknown-trigger-only-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	store := NewWorkflowControlStore(db)
	store.Instruments = instruments

	reconciled, err := store.ReconcileWorkflowRuns(context.Background(), now)
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
	if got, want := reconciled, 0; got != want {
		t.Fatalf("reconciled = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 0; got != want {
		t.Fatalf("exec count = %d, want %d (a skipped run writes nothing)", got, want)
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got, want := counterTotal(collected, unknownTriggerSkipsMetric), int64(1); got != want {
		t.Fatalf("%s = %d, want %d", unknownTriggerSkipsMetric, got, want)
	}
}
