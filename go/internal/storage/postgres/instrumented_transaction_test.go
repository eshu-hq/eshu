// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// instrumentedTestBeginner is a fake ExecQueryer that also implements
// Beginner, returning a fake Transaction whose ExecContext calls are counted
// but never actually instrumented on their own -- proving that any histogram
// observation recorded for a call made through the transaction came from the
// WRAPPER InstrumentedDB.Begin applies, not from the fake itself.
type instrumentedTestBeginner struct {
	instrumentedTestExecQueryer
	txExecCalls        int
	readSnapshotBegins int
}

func (f *instrumentedTestBeginner) Begin(ctx context.Context) (Transaction, error) {
	return &instrumentedTestTx{parent: f}, nil
}

func (f *instrumentedTestBeginner) BeginReadOnlyRepeatableRead(
	ctx context.Context,
) (Transaction, error) {
	f.readSnapshotBegins++
	return &instrumentedTestTx{parent: f}, nil
}

type instrumentedTestTx struct {
	parent     *instrumentedTestBeginner
	committed  bool
	rolledBack bool
}

func (tx *instrumentedTestTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	tx.parent.txExecCalls++
	return &instrumentedTestResult{}, nil
}

func (tx *instrumentedTestTx) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return &instrumentedTestRows{}, nil
}

func (tx *instrumentedTestTx) Commit() error   { tx.committed = true; return nil }
func (tx *instrumentedTestTx) Rollback() error { tx.rolledBack = true; return nil }

// TestInstrumentedDBBeginWrapsTransactionWithMetrics is the regression for the
// silent observability drop an unwrapped Begin left open (#5848 adjacent fix,
// caught by go/internal/replay/costcounting's aws_cloud_runtime_drift cost
// budget once that writer started routing real per-write statements through a
// transaction). Before the fix, InstrumentedDB.Begin returned the inner
// Beginner's transaction directly, so ExecContext calls made through it never
// reached PostgresQueryDuration -- this asserts they do now.
func TestInstrumentedDBBeginWrapsTransactionWithMetrics(t *testing.T) {
	fake := &instrumentedTestBeginner{}
	metricReader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(metricReader))
	meter := meterProvider.Meter("test")
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments failed: %v", err)
	}

	instrumented := &InstrumentedDB{Inner: fake, Instruments: instruments, StoreName: "aws_drift_admission"}

	ctx := context.Background()
	tx, err := instrumented.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO t VALUES ($1)", 1); err != nil {
		t.Fatalf("tx.ExecContext() error = %v", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM t WHERE id = $1", 1); err != nil {
		t.Fatalf("tx.ExecContext() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() error = %v", err)
	}

	if fake.txExecCalls != 2 {
		t.Fatalf("inner transaction ExecContext calls = %d, want 2", fake.txExecCalls)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var observations uint64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_postgres_query_duration_seconds" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("expected Histogram data type, got %T", m.Data)
			}
			for _, dp := range hist.DataPoints {
				observations += dp.Count
			}
		}
	}
	if observations != 2 {
		t.Fatalf(
			"eshu_dp_postgres_query_duration_seconds observations = %d, want 2: "+
				"ExecContext calls made through a transaction Begin() returned must be instrumented "+
				"the same as non-transactional calls, or a writer that moves to a transaction "+
				"silently loses Postgres query duration observability",
			observations,
		)
	}
}

// TestInstrumentedDBBeginReturnsErrorWhenInnerCannotBegin preserves the
// pre-existing behavior for an Inner that does not implement Beginner.
func TestInstrumentedDBBeginReturnsErrorWhenInnerCannotBegin(t *testing.T) {
	fake := &instrumentedTestExecQueryer{}
	instrumented := &InstrumentedDB{Inner: fake, StoreName: "test"}

	if _, err := instrumented.Begin(context.Background()); err == nil {
		t.Fatal("Begin() error = nil, want error for a non-Beginner Inner")
	}
}

func TestInstrumentedDBBeginReadOnlyRepeatableReadWrapsTransaction(t *testing.T) {
	fake := &instrumentedTestBeginner{}
	metricReader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments failed: %v", err)
	}
	instrumented := &InstrumentedDB{
		Inner: fake, Instruments: instruments, StoreName: "facts",
	}

	ctx := context.Background()
	tx, err := instrumented.BeginReadOnlyRepeatableRead(ctx)
	if err != nil {
		t.Fatalf("BeginReadOnlyRepeatableRead() error = %v", err)
	}
	rows, err := tx.QueryContext(ctx, "SELECT 1")
	if err != nil {
		t.Fatalf("tx.QueryContext() error = %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("rows.Close() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() error = %v", err)
	}
	if fake.readSnapshotBegins != 1 {
		t.Fatalf("inner read-snapshot begins = %d, want 1", fake.readSnapshotBegins)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var observations uint64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_postgres_query_duration_seconds" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("expected Histogram data type, got %T", m.Data)
			}
			for _, point := range hist.DataPoints {
				observations += point.Count
			}
		}
	}
	if observations != 1 {
		t.Fatalf("transaction query duration observations = %d, want 1", observations)
	}
}

func TestInstrumentedDBBeginReadOnlyRepeatableReadRequiresCapability(t *testing.T) {
	instrumented := &InstrumentedDB{Inner: &instrumentedTestExecQueryer{}, StoreName: "facts"}

	_, err := instrumented.BeginReadOnlyRepeatableRead(context.Background())
	if err == nil {
		t.Fatal("BeginReadOnlyRepeatableRead() error = nil, want capability error")
	}
}
