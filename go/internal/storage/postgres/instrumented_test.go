package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// instrumentedTestExecQueryer tracks method calls for testing.
type instrumentedTestExecQueryer struct {
	execCalled  bool
	queryCalled bool
	execErr     error
	queryErr    error
	queryResult *instrumentedTestRows
}

func (f *instrumentedTestExecQueryer) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	f.execCalled = true
	if f.execErr != nil {
		return nil, f.execErr
	}
	return &instrumentedTestResult{}, nil
}

func (f *instrumentedTestExecQueryer) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	f.queryCalled = true
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if f.queryResult != nil {
		return f.queryResult, nil
	}
	return &instrumentedTestRows{}, nil
}

// instrumentedTestResult implements sql.Result.
type instrumentedTestResult struct{}

func (f *instrumentedTestResult) LastInsertId() (int64, error) { return 0, nil }
func (f *instrumentedTestResult) RowsAffected() (int64, error) { return 1, nil }

// instrumentedTestRows implements Rows.
type instrumentedTestRows struct {
	closed bool
}

func (f *instrumentedTestRows) Next() bool        { return false }
func (f *instrumentedTestRows) Scan(...any) error { return nil }
func (f *instrumentedTestRows) Err() error        { return nil }
func (f *instrumentedTestRows) Close() error      { f.closed = true; return nil }

func TestInstrumentedDB_ExecContext_RecordsSpanAndMetric(t *testing.T) {
	fake := &instrumentedTestExecQueryer{}
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer("test")

	metricReader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(metricReader))
	meter := meterProvider.Meter("test")

	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments failed: %v", err)
	}

	instrumented := &InstrumentedDB{
		Inner:       fake,
		Tracer:      tracer,
		Instruments: instruments,
		StoreName:   "test_store",
	}

	ctx := context.Background()
	_, err = instrumented.ExecContext(ctx, "INSERT INTO test VALUES ($1)", 42)
	if err != nil {
		t.Fatalf("ExecContext failed: %v", err)
	}

	if !fake.execCalled {
		t.Error("ExecContext was not called on inner ExecQueryer")
	}

	// Verify span
	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Name() != "postgres.exec" {
		t.Errorf("expected span name 'postgres.exec', got %q", span.Name())
	}

	// Verify span attributes
	expectedAttrs := map[string]string{
		"db.system":    "postgresql",
		"db.operation": "exec",
		"eshu.store":   "test_store",
	}

	for key, expectedValue := range expectedAttrs {
		found := false
		for _, attr := range span.Attributes() {
			if string(attr.Key) == key {
				found = true
				if attr.Value.AsString() != expectedValue {
					t.Errorf("attribute %q: expected %q, got %q", key, expectedValue, attr.Value.AsString())
				}
				break
			}
		}
		if !found {
			t.Errorf("span missing attribute %q", key)
		}
	}

	// Verify metric
	var rm metricdata.ResourceMetrics
	err = metricReader.Collect(ctx, &rm)
	if err != nil {
		t.Fatalf("failed to collect metrics: %v", err)
	}

	if len(rm.ScopeMetrics) == 0 {
		t.Fatal("no scope metrics recorded")
	}

	var foundHistogram bool
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "eshu_dp_postgres_query_duration_seconds" {
				foundHistogram = true
				histogram, ok := m.Data.(metricdata.Histogram[float64])
				if !ok {
					t.Errorf("expected Histogram data type, got %T", m.Data)
					continue
				}

				if len(histogram.DataPoints) == 0 {
					t.Error("histogram has no data points")
					continue
				}

				// Verify attributes
				dp := histogram.DataPoints[0]
				attrMap := make(map[string]string)
				for _, attr := range dp.Attributes.ToSlice() {
					attrMap[string(attr.Key)] = attr.Value.AsString()
				}

				if attrMap["operation"] != "write" {
					t.Errorf("expected operation=write, got %q", attrMap["operation"])
				}
				if attrMap["store"] != "test_store" {
					t.Errorf("expected store=test_store, got %q", attrMap["store"])
				}
			}
		}
	}

	if !foundHistogram {
		t.Error("postgres query duration histogram not found in metrics")
	}
}

func TestInstrumentedDB_QueryContext_RecordsSpanAndMetric(t *testing.T) {
	fake := &instrumentedTestExecQueryer{queryResult: &instrumentedTestRows{}}
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer("test")

	metricReader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(metricReader))
	meter := meterProvider.Meter("test")

	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments failed: %v", err)
	}

	instrumented := &InstrumentedDB{
		Inner:       fake,
		Tracer:      tracer,
		Instruments: instruments,
		StoreName:   "content",
	}

	ctx := context.Background()
	rows, err := instrumented.QueryContext(ctx, "SELECT * FROM test")
	if err != nil {
		t.Fatalf("QueryContext failed: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if !fake.queryCalled {
		t.Error("QueryContext was not called on inner ExecQueryer")
	}

	// Verify span
	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Name() != "postgres.query" {
		t.Errorf("expected span name 'postgres.query', got %q", span.Name())
	}

	// Verify span attributes
	expectedAttrs := map[string]string{
		"db.system":    "postgresql",
		"db.operation": "query",
		"eshu.store":   "content",
	}

	for key, expectedValue := range expectedAttrs {
		found := false
		for _, attr := range span.Attributes() {
			if string(attr.Key) == key {
				found = true
				if attr.Value.AsString() != expectedValue {
					t.Errorf("attribute %q: expected %q, got %q", key, expectedValue, attr.Value.AsString())
				}
				break
			}
		}
		if !found {
			t.Errorf("span missing attribute %q", key)
		}
	}

	// Verify metric
	var rm metricdata.ResourceMetrics
	err = metricReader.Collect(ctx, &rm)
	if err != nil {
		t.Fatalf("failed to collect metrics: %v", err)
	}

	var foundHistogram bool
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "eshu_dp_postgres_query_duration_seconds" {
				foundHistogram = true
				histogram, ok := m.Data.(metricdata.Histogram[float64])
				if !ok {
					t.Errorf("expected Histogram data type, got %T", m.Data)
					continue
				}

				if len(histogram.DataPoints) == 0 {
					t.Error("histogram has no data points")
					continue
				}

				dp := histogram.DataPoints[0]
				attrMap := make(map[string]string)
				for _, attr := range dp.Attributes.ToSlice() {
					attrMap[string(attr.Key)] = attr.Value.AsString()
				}

				if attrMap["operation"] != "read" {
					t.Errorf("expected operation=read, got %q", attrMap["operation"])
				}
				if attrMap["store"] != "content" {
					t.Errorf("expected store=content, got %q", attrMap["store"])
				}
			}
		}
	}

	if !foundHistogram {
		t.Error("postgres query duration histogram not found in metrics")
	}
}

func TestInstrumentedDB_NilTracerAndInstruments_PassesThrough(t *testing.T) {
	fake := &instrumentedTestExecQueryer{queryResult: &instrumentedTestRows{}}

	instrumented := &InstrumentedDB{
		Inner:       fake,
		Tracer:      nil,
		Instruments: nil,
		StoreName:   "test",
	}

	ctx := context.Background()

	// Test ExecContext
	_, err := instrumented.ExecContext(ctx, "INSERT INTO test VALUES ($1)", 1)
	if err != nil {
		t.Fatalf("ExecContext with nil tracer/instruments failed: %v", err)
	}
	if !fake.execCalled {
		t.Error("ExecContext was not called on inner ExecQueryer")
	}

	// Test QueryContext
	rows, err := instrumented.QueryContext(ctx, "SELECT * FROM test")
	if err != nil {
		t.Fatalf("QueryContext with nil tracer/instruments failed: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if !fake.queryCalled {
		t.Error("QueryContext was not called on inner ExecQueryer")
	}
}

func TestInstrumentedDB_ErrorHandling(t *testing.T) {
	execErr := errors.New("exec failed")
	queryErr := errors.New("query failed")

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer("test")

	metricReader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(metricReader))
	meter := meterProvider.Meter("test")

	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments failed: %v", err)
	}

	t.Run("ExecContext error sets span status", func(t *testing.T) {
		fake := &instrumentedTestExecQueryer{execErr: execErr}
		instrumented := &InstrumentedDB{
			Inner:       fake,
			Tracer:      tracer,
			Instruments: instruments,
			StoreName:   "test",
		}

		ctx := context.Background()
		_, err := instrumented.ExecContext(ctx, "INSERT INTO test VALUES ($1)", 1)
		if err == nil || err != execErr {
			t.Fatalf("expected exec error, got %v", err)
		}

		// Allow time for span to be recorded
		time.Sleep(10 * time.Millisecond)

		spans := spanRecorder.Ended()
		if len(spans) == 0 {
			t.Fatal("no spans recorded")
		}

		lastSpan := spans[len(spans)-1]
		if lastSpan.Status().Code != codes.Error {
			t.Errorf("expected span status code Error, got %v", lastSpan.Status().Code)
		}
	})

	t.Run("QueryContext error sets span status", func(t *testing.T) {
		fake := &instrumentedTestExecQueryer{queryErr: queryErr}
		instrumented := &InstrumentedDB{
			Inner:       fake,
			Tracer:      tracer,
			Instruments: instruments,
			StoreName:   "test",
		}

		ctx := context.Background()
		_, err := instrumented.QueryContext(ctx, "SELECT * FROM test")
		if err == nil || err != queryErr {
			t.Fatalf("expected query error, got %v", err)
		}

		// Allow time for span to be recorded
		time.Sleep(10 * time.Millisecond)

		spans := spanRecorder.Ended()
		if len(spans) == 0 {
			t.Fatal("no spans recorded")
		}

		lastSpan := spans[len(spans)-1]
		if lastSpan.Status().Code != codes.Error {
			t.Errorf("expected span status code Error, got %v", lastSpan.Status().Code)
		}
	})
}

func TestInstrumentedDB_RecordsDuration(t *testing.T) {
	fake := &instrumentedTestExecQueryer{}
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer("test")

	metricReader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(metricReader))
	meter := meterProvider.Meter("test")

	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments failed: %v", err)
	}

	instrumented := &InstrumentedDB{
		Inner:       fake,
		Tracer:      tracer,
		Instruments: instruments,
		StoreName:   "test",
	}

	ctx := context.Background()
	_, err = instrumented.ExecContext(ctx, "INSERT INTO test VALUES ($1)", 1)
	if err != nil {
		t.Fatalf("ExecContext failed: %v", err)
	}

	var rm metricdata.ResourceMetrics
	err = metricReader.Collect(ctx, &rm)
	if err != nil {
		t.Fatalf("failed to collect metrics: %v", err)
	}

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "eshu_dp_postgres_query_duration_seconds" {
				histogram, ok := m.Data.(metricdata.Histogram[float64])
				if !ok {
					t.Fatalf("expected Histogram data type, got %T", m.Data)
				}

				if len(histogram.DataPoints) == 0 {
					t.Fatal("histogram has no data points")
				}

				dp := histogram.DataPoints[0]
				if dp.Count == 0 {
					t.Error("histogram count is 0")
				}

				// Duration should be recorded (sum should be > 0)
				if dp.Sum <= 0 {
					t.Errorf("expected positive duration sum, got %f", dp.Sum)
				}
			}
		}
	}
}
