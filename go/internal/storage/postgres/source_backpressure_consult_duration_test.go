// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const sourceConsultDurationMetric = "eshu_dp_reducer_admission_source_consult_duration_seconds"

// failingQueryDB fails every query with err and serves no exec, so a consult
// over it fails at the signal read.
type failingQueryDB struct {
	sourceBackpressureFakeDB
	err error
}

func (f *failingQueryDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, f.err
}

// consultDurationStore builds a control store over fake with real
// instruments and returns a collector for the consult duration histogram.
func consultDurationStore(t *testing.T, fake db.ExecQueryer) (*WorkflowControlStore, func() []metricdata.HistogramDataPoint[float64]) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	store := NewWorkflowControlStore(fake)
	store.Instruments = instruments
	return store, func() []metricdata.HistogramDataPoint[float64] {
		var collected metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &collected); err != nil {
			t.Fatalf("Collect() error = %v", err)
		}
		for _, scopeMetrics := range collected.ScopeMetrics {
			for _, m := range scopeMetrics.Metrics {
				if m.Name != sourceConsultDurationMetric {
					continue
				}
				histogram, ok := m.Data.(metricdata.Histogram[float64])
				if !ok {
					t.Fatalf("%s data = %T, want Histogram[float64]", m.Name, m.Data)
				}
				return histogram.DataPoints
			}
		}
		return nil
	}
}

// consultPointAttrs flattens one data point's attributes for assertions.
func consultPointAttrs(point metricdata.HistogramDataPoint[float64]) map[string]string {
	out := map[string]string{}
	for _, kv := range point.Attributes.ToSlice() {
		out[string(kv.Key)] = kv.Value.AsString()
	}
	return out
}

// TestCheckSourceBackpressureForSourceRecordsConsultDuration pins the #10882
// replay instrument for the per-source consult: one sample per consult,
// labelled with the consulted source and outcome=ok, covering the whole
// consult (signal, latch read, latch write on the transition).
func TestCheckSourceBackpressureForSourceRecordsConsultDuration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	fake := &sourceBackpressureFakeDB{responses: []fakeRows{
		{rows: [][]any{{float64(7200), int64(400), int64(400), int64(400)}}}, // bounded probe: behind
		{rows: [][]any{}}, // latches
	}}
	store, collect := consultDurationStore(t, fake)

	if _, err := store.CheckSourceBackpressureForSource(context.Background(), "git", sourceBackpressureTestConfig(), now); err != nil {
		t.Fatalf("CheckSourceBackpressureForSource() error = %v, want nil", err)
	}

	points := collect()
	if len(points) != 1 {
		t.Fatalf("consult duration data points = %d, want 1", len(points))
	}
	if points[0].Count != 1 {
		t.Fatalf("consult duration count = %d, want 1", points[0].Count)
	}
	attrs := consultPointAttrs(points[0])
	if attrs["source_system"] != "git" || attrs["outcome"] != "ok" || len(attrs) != 2 {
		t.Fatalf("consult duration attrs = %v, want only source_system=git and outcome=ok", attrs)
	}
}

// TestCheckSourceBackpressureRecordsFamilyConsultDuration pins the claim-grant
// consult: the family consult carries collector_kind, never a source or
// instance id, so the label stays bounded by the collector-kind set.
func TestCheckSourceBackpressureRecordsFamilyConsultDuration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	fake := &sourceBackpressureFakeDB{responses: []fakeRows{
		{rows: [][]any{{"vuln-intel"}}},
		{rows: [][]any{{float64(60), int64(3), int64(0), int64(0)}}}, // bounded probe: healthy
		{rows: [][]any{}}, // latches
	}}
	store, collect := consultDurationStore(t, fake)

	if _, err := store.CheckSourceBackpressure(context.Background(), workflow.ClaimSelector{
		CollectorKind:       scope.CollectorKind("vuln"),
		CollectorInstanceID: "vuln-primary",
	}, sourceBackpressureTestConfig(), now); err != nil {
		t.Fatalf("CheckSourceBackpressure() error = %v, want nil", err)
	}

	points := collect()
	if len(points) != 1 || points[0].Count != 1 {
		t.Fatalf("family consult data points = %+v, want one point with one sample", points)
	}
	attrs := consultPointAttrs(points[0])
	if attrs["collector_kind"] != "vuln" || attrs["outcome"] != "ok" || len(attrs) != 2 {
		t.Fatalf("family consult attrs = %v, want only collector_kind=vuln and outcome=ok", attrs)
	}
}

// TestCheckSourceBackpressureForSourceRecordsErrorOutcome proves a failed
// consult is still timed, under outcome=error, so a slow failing probe is
// visible instead of vanishing from the percentile.
func TestCheckSourceBackpressureForSourceRecordsErrorOutcome(t *testing.T) {
	t.Parallel()

	fake := &failingQueryDB{err: errors.New("probe unavailable")}
	store, collect := consultDurationStore(t, fake)

	if _, err := store.CheckSourceBackpressureForSource(
		context.Background(), "git", sourceBackpressureTestConfig(), time.Now()); err == nil {
		t.Fatal("CheckSourceBackpressureForSource() error = nil, want the probe failure")
	}

	points := collect()
	if len(points) != 1 || points[0].Count != 1 {
		t.Fatalf("error consult data points = %+v, want one point with one sample", points)
	}
	if attrs := consultPointAttrs(points[0]); attrs["outcome"] != "error" || attrs["source_system"] != "git" {
		t.Fatalf("error consult attrs = %v, want outcome=error source_system=git", attrs)
	}
}

// TestCheckSourceBackpressureForSourceCancelledContextRecordsNothing keeps
// shutdown out of the percentile, matching the failures counter.
func TestCheckSourceBackpressureForSourceCancelledContextRecordsNothing(t *testing.T) {
	t.Parallel()

	fake := &failingQueryDB{err: context.Canceled}
	store, collect := consultDurationStore(t, fake)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.CheckSourceBackpressureForSource(ctx, "git", sourceBackpressureTestConfig(), time.Now()); err == nil {
		t.Fatal("CheckSourceBackpressureForSource() error = nil, want the cancelled-context failure")
	}
	if points := collect(); len(points) != 0 {
		t.Fatalf("cancelled consult recorded %d data points, want 0", len(points))
	}
}

// TestCheckSourceBackpressureDisabledRecordsNoConsultDuration pins the
// default-off contract: with the zero config no consult runs, so no sample
// is recorded and the instrument adds no series on a disabled deployment.
func TestCheckSourceBackpressureDisabledRecordsNoConsultDuration(t *testing.T) {
	t.Parallel()

	store, collect := consultDurationStore(t, &sourceBackpressureFakeDB{})
	if _, err := store.CheckSourceBackpressureForSource(
		context.Background(), "git", workflow.SourceBackpressureConfig{}, time.Now()); err != nil {
		t.Fatalf("CheckSourceBackpressureForSource() error = %v, want nil", err)
	}
	if _, err := store.CheckSourceBackpressure(context.Background(), workflow.ClaimSelector{
		CollectorKind: scope.CollectorKind("vuln"), CollectorInstanceID: "vuln-primary",
	}, workflow.SourceBackpressureConfig{}, time.Now()); err != nil {
		t.Fatalf("CheckSourceBackpressure() error = %v, want nil", err)
	}
	if points := collect(); len(points) != 0 {
		t.Fatalf("disabled consult recorded %d data points, want 0", len(points))
	}
}
