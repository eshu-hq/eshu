// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// planRowOutcomes collects eshu_dp_nvd_baseline_plan_rows_total by outcome.
func planRowOutcomes(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	outcomes := make(map[string]int64)
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != "eshu_dp_nvd_baseline_plan_rows_total" {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want an int64 sum", metric.Data)
			}
			for _, point := range sum.DataPoints {
				value, _ := point.Attributes.Value(telemetry.AttrOutcome("").Key)
				outcomes[value.AsString()] += point.Value
			}
		}
	}
	return outcomes
}

// TestWorkflowControlStoreCountsBaselinePlanRowsAfterCommit proves the counter
// splits the plans an admission was handed into recorded and not_inserted, once
// per admission, after the commit. A rolled-back admission counts nothing:
// the plan rows did not land, and the coordinator's error log carries it.
func TestWorkflowControlStoreCountsBaselinePlanRowsAfterCommit(t *testing.T) {
	t.Parallel()

	newStore := func(t *testing.T, database db.ExecQueryer) (*WorkflowControlStore, *sdkmetric.ManualReader) {
		t.Helper()
		reader := sdkmetric.NewManualReader()
		instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("plan-rows-test"))
		if err != nil {
			t.Fatalf("NewInstruments() error = %v", err)
		}
		store := NewWorkflowControlStore(database)
		store.Instruments = instruments
		return store, reader
	}

	t.Run("one recorded one not inserted", func(t *testing.T) {
		t.Parallel()
		run, items, plans := baselineAdmissionFixture(2026, 2025)
		store, reader := newStore(t, &orderedStatementRecorder{
			rows:     []queueFakeRows{{rows: [][]any{{false}}}, {rows: [][]any{{0}, {1}}}, {rows: [][]any{{items[1].ScopeID}}}},
			execRows: []int64{1, 1, 1},
		})
		if _, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(context.Background(), run, items, plans, nil); err != nil {
			t.Fatalf("admission error = %v", err)
		}
		got := planRowOutcomes(t, reader)
		if got["recorded"] != 1 || got["not_inserted"] != 1 || len(got) != 2 {
			t.Fatalf("outcomes = %v, want recorded=1 not_inserted=1", got)
		}
	})
	t.Run("guard emptied the admission", func(t *testing.T) {
		t.Parallel()
		run, items, plans := baselineAdmissionFixture(2026, 2025)
		store, reader := newStore(t, &orderedStatementRecorder{
			rows:     []queueFakeRows{{rows: [][]any{{false}}}, {rows: nil}},
			execRows: []int64{1},
		})
		if _, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(context.Background(), run, items, plans, nil); err != nil {
			t.Fatalf("admission error = %v", err)
		}
		got := planRowOutcomes(t, reader)
		if got["not_inserted"] != 2 || got["recorded"] != 0 {
			t.Fatalf("outcomes = %v, want both plans counted not_inserted", got)
		}
	})
	t.Run("rollback counts nothing", func(t *testing.T) {
		t.Parallel()
		run, items, plans := baselineAdmissionFixture(2026)
		fault := &planWriteFault{err: errors.New("boom")}
		fault.orderedStatementRecorder = &orderedStatementRecorder{
			rows:     []queueFakeRows{{rows: [][]any{{false}}}, {rows: [][]any{{0}}}, {rows: [][]any{{items[0].ScopeID}}}},
			execRows: []int64{1, 1},
		}
		store, reader := newStore(t, fault)
		if _, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(context.Background(), run, items, plans, nil); err == nil {
			t.Fatal("error = nil, want the plan write failure")
		}
		if got := planRowOutcomes(t, reader); len(got) != 0 {
			t.Fatalf("outcomes = %v, want nothing counted for a rolled-back admission", got)
		}
	})
	t.Run("nil instruments are a no-op", func(t *testing.T) {
		t.Parallel()
		run, items, plans := baselineAdmissionFixture(2026)
		store := NewWorkflowControlStore(&orderedStatementRecorder{
			rows:     []queueFakeRows{{rows: [][]any{{false}}}, {rows: [][]any{{0}}}, {rows: [][]any{{items[0].ScopeID}}}},
			execRows: []int64{1, 1, 1},
		})
		if _, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(context.Background(), run, items, plans, nil); err != nil {
			t.Fatalf("admission error = %v, want telemetry to be optional", err)
		}
	})
}
