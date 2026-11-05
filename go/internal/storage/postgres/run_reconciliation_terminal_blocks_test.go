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

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const terminalDeadLetterBlocksMetric = "eshu_dp_workflow_run_terminal_dead_letter_blocks_total"

// TestReconcileCountsDeadLetterBlockOnlyWhenTheRunTerminates pins the documented
// meaning of the dead-letter counter: reconciliations that terminated a run. A
// dead-lettered run now stays open while collection is pending (#10091), so it
// is reconciled on every tick; counting those ticks would inflate the rate an
// operator alerts on. The blocked completeness row carries the signal meanwhile.
func TestReconcileCountsDeadLetterBlockOnlyWhenTheRunTerminates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		pending     int
		completed   int
		wantStatus  workflow.RunStatus
		wantCounted int64
	}{
		{
			name:        "open run with pending collection is not counted",
			pending:     1,
			completed:   1,
			wantStatus:  workflow.RunStatusCollectionActive,
			wantCounted: 0,
		},
		{
			name:        "run that terminates is counted",
			pending:     0,
			completed:   2,
			wantStatus:  workflow.RunStatusFailed,
			wantCounted: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
			db := &fakeExecQueryer{queryResponses: []queueFakeRows{
				{rows: [][]any{{
					"run-dead-letter",
					string(workflow.TriggerKindSchedule),
					string(workflow.RunStatusCollectionActive),
					"[]",
					sql.NullString{},
					now.Add(-time.Hour),
					now.Add(-time.Hour),
					sql.NullTime{},
				}}},
				{rows: [][]any{{string(scope.CollectorGit), 2, tt.pending, 0, tt.completed, 0}}},
				{rows: nil},
				{rows: [][]any{{
					string(scope.CollectorGit),
					string(gpphase.KeyspaceServiceUID),
					string(gpphase.PhaseDeploymentMapping),
					1,
				}}},
				{rows: nil},
			}}

			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			instruments, err := telemetry.NewInstruments(provider.Meter("terminal-blocks-test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v, want nil", err)
			}
			store := NewWorkflowControlStore(db)
			store.Instruments = instruments

			if _, err := store.ReconcileWorkflowRuns(context.Background(), now); err != nil {
				t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
			}
			if len(db.execs) == 0 || !strings.Contains(db.execs[0].query, "UPDATE workflow_runs") {
				t.Fatalf("first exec = %+v, want the workflow_runs status update", db.execs)
			}
			if got, want := db.execs[0].args[1], string(tt.wantStatus); got != want {
				t.Fatalf("written run status = %v, want %q", got, want)
			}

			var collected metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &collected); err != nil {
				t.Fatalf("Collect() error = %v, want nil", err)
			}
			if got := counterTotal(collected, terminalDeadLetterBlocksMetric); got != tt.wantCounted {
				t.Fatalf("%s = %d, want %d", terminalDeadLetterBlocksMetric, got, tt.wantCounted)
			}
		})
	}
}
