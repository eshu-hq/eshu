// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestEdgeWriterUnroutableRowsCounterRecordsPartialBatch closes a review gap on
// PR #6008: every other test in this file builds its writer with
// NewEdgeWriter, which leaves Instruments nil, so the counter branch inside
// reportUnroutableRows was never executed at unit level — only the log was.
// A nil-guarded metric that no test ever reaches is indistinguishable from one
// that was wired to the wrong instrument.
//
// Asserts the count is the DROPPED-row count rather than the batch size, and
// that the reason label is the bounded partial_batch value.
func TestEdgeWriterUnroutableRowsCounterRecordsPartialBatch(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	writer := NewEdgeWriter(&recordingExecutor{}, 0)
	writer.Instruments = instruments

	rows := []reducer.SharedProjectionIntentRow{
		routableRepoDependencyRow("i1"),
		unroutableRepoDependencyRow("i2"),
		unroutableRepoDependencyRow("i3"),
	}
	if _, err := writer.WriteEdges(
		context.Background(),
		reducer.DomainRepoDependency,
		rows,
		"finalization/workloads",
	); err != nil {
		t.Fatalf("WriteEdges() error = %v, want nil when some rows routed", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertInt64CounterValue(t, rm, "eshu_dp_shared_edge_unroutable_rows_total", map[string]string{
		"domain": reducer.DomainRepoDependency,
		"reason": unroutableReasonPartialBatch,
	}, 2)
}

// TestEdgeWriterUnroutableRowsCounterRecordsWholeBatch pins the other bounded
// reason value, so a future edit cannot collapse the two into one label and
// leave an operator unable to tell a partial loss from a batch that produced
// nothing.
func TestEdgeWriterUnroutableRowsCounterRecordsWholeBatch(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	writer := NewEdgeWriter(&recordingExecutor{}, 0)
	writer.Instruments = instruments

	rows := []reducer.SharedProjectionIntentRow{
		unroutableRepoDependencyRow("i1"),
		unroutableRepoDependencyRow("i2"),
	}
	// The whole-batch case is not an error (the rows can never route, so there
	// is nothing to retry); the counter and the report carry the loss.
	report, err := writer.WriteEdges(
		context.Background(),
		reducer.DomainRepoDependency,
		rows,
		"finalization/workloads",
	)
	if err != nil {
		t.Fatalf("WriteEdges() error = %v, want nil", err)
	}
	if got, want := len(report.UnroutableRows), 2; got != want {
		t.Fatalf("reported rows = %d, want %d", got, want)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertInt64CounterValue(t, rm, "eshu_dp_shared_edge_unroutable_rows_total", map[string]string{
		"domain": reducer.DomainRepoDependency,
		"reason": unroutableReasonWholeBatch,
	}, 2)
}

// TestEdgeWriterControlRowOnlyBatchRecordsNoUnroutableMetric is the negative
// control for both tests above: a control row carries no edge by design, so it
// must not appear on the lost-edge counter. Without this, the counter could
// silently start counting repo-refresh rows and read as edge loss.
func TestEdgeWriterControlRowOnlyBatchRecordsNoUnroutableMetric(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	writer := NewEdgeWriter(&recordingExecutor{}, 0)
	writer.Instruments = instruments

	rows := []reducer.SharedProjectionIntentRow{{
		IntentID:     "refresh",
		RepositoryID: "repo-a",
		Payload:      map[string]any{"repo_id": "repo-a", "intent_type": "repo_refresh"},
	}}
	if _, err := writer.WriteEdges(
		context.Background(),
		reducer.DomainCodeCalls,
		rows,
		"parser/code-calls",
	); err != nil {
		t.Fatalf("WriteEdges() error = %v, want nil for a control-row-only batch", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "eshu_dp_shared_edge_unroutable_rows_total" {
				t.Fatalf("control-row-only batch recorded %q; a row that carries no edge is not a lost edge", m.Name)
			}
		}
	}
}
