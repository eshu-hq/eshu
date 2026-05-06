package cypher

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestInstrumentedExecutorRecordsDurationAndCreatesSpan(t *testing.T) {
	t.Parallel()

	// Setup OTEL test infrastructure
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	tracer := tracerProvider.Tracer("test")

	metricReader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	// Setup fake executor
	inner := &recordingExecutor{}
	executor := &InstrumentedExecutor{
		Inner:       inner,
		Tracer:      tracer,
		Instruments: instruments,
	}

	// Execute a statement
	statement := Statement{
		Operation:  OperationUpsertNode,
		Cypher:     "MERGE (n:Test) SET n.value = $value",
		Parameters: map[string]any{"value": "test"},
	}

	ctx := context.Background()
	err = executor.Execute(ctx, statement)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Verify inner executor was called
	if got, want := len(inner.calls), 1; got != want {
		t.Fatalf("inner.calls = %d, want %d", got, want)
	}
	if got, want := inner.calls[0].Operation, OperationUpsertNode; got != want {
		t.Fatalf("inner.calls[0].Operation = %q, want %q", got, want)
	}

	// Verify span was created
	spans := spanRecorder.Ended()
	if got, want := len(spans), 1; got != want {
		t.Fatalf("len(spans) = %d, want %d", got, want)
	}

	span := spans[0]
	if got, want := span.Name(), "neo4j.execute"; got != want {
		t.Fatalf("span.Name() = %q, want %q", got, want)
	}
	if got, want := span.Status().Code, codes.Unset; got != want {
		t.Fatalf("span.Status().Code = %v, want %v", got, want)
	}

	// Verify span attributes
	attrs := span.Attributes()
	hasDBSystem := false
	hasDBOperation := false
	for _, attr := range attrs {
		if attr.Key == "db.system" && attr.Value.AsString() == "neo4j" {
			hasDBSystem = true
		}
		if attr.Key == "db.operation" && attr.Value.AsString() == string(OperationUpsertNode) {
			hasDBOperation = true
		}
	}
	if !hasDBSystem {
		t.Fatal("span missing db.system=neo4j attribute")
	}
	if !hasDBOperation {
		t.Fatalf("span missing db.operation=%s attribute", OperationUpsertNode)
	}

	// Verify histogram was recorded
	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	if len(rm.ScopeMetrics) == 0 {
		t.Fatal("no scope metrics recorded")
	}

	var histogramFound bool
	for _, scopeMetric := range rm.ScopeMetrics {
		for _, m := range scopeMetric.Metrics {
			if m.Name == "eshu_dp_neo4j_query_duration_seconds" {
				histogramFound = true
				histogram, ok := m.Data.(metricdata.Histogram[float64])
				if !ok {
					t.Fatalf("metric data type = %T, want Histogram[float64]", m.Data)
				}
				if len(histogram.DataPoints) == 0 {
					t.Fatal("histogram has no data points")
				}
				dp := histogram.DataPoints[0]
				if dp.Count == 0 {
					t.Fatal("histogram count = 0, want > 0")
				}
				// Verify operation=write attribute
				hasOperation := false
				for _, attr := range dp.Attributes.ToSlice() {
					if attr.Key == "operation" && attr.Value.AsString() == "write" {
						hasOperation = true
					}
				}
				if !hasOperation {
					t.Fatal("histogram missing operation=write attribute")
				}
			}
		}
	}
	if !histogramFound {
		t.Fatal("eshu_dp_neo4j_query_duration_seconds histogram not recorded")
	}
}

func TestInstrumentedExecutorSetsSpanStatusOnError(t *testing.T) {
	t.Parallel()

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	tracer := tracerProvider.Tracer("test")

	inner := &recordingExecutor{errAtCall: errors.New("connection failed")}
	executor := &InstrumentedExecutor{
		Inner:  inner,
		Tracer: tracer,
	}

	statement := Statement{
		Operation: OperationDeleteNode,
		Cypher:    "MATCH (n:Test) DELETE n",
	}

	err := executor.Execute(context.Background(), statement)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}
	if got, want := err.Error(), "connection failed"; got != want {
		t.Fatalf("Execute() error = %q, want %q", got, want)
	}

	spans := spanRecorder.Ended()
	if got, want := len(spans), 1; got != want {
		t.Fatalf("len(spans) = %d, want %d", got, want)
	}

	span := spans[0]
	if got, want := span.Status().Code, codes.Error; got != want {
		t.Fatalf("span.Status().Code = %v, want %v", got, want)
	}
	if got := span.Status().Description; got != "connection failed" {
		t.Fatalf("span.Status().Description = %q, want %q", got, "connection failed")
	}
}

func TestInstrumentedExecutorNilTracerAndInstrumentsPassesThrough(t *testing.T) {
	t.Parallel()

	inner := &recordingExecutor{}
	executor := &InstrumentedExecutor{
		Inner:       inner,
		Tracer:      nil,
		Instruments: nil,
	}

	statement := Statement{
		Operation:  OperationUpsertNode,
		Cypher:     "MERGE (n:Test)",
		Parameters: map[string]any{},
	}

	err := executor.Execute(context.Background(), statement)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	if got, want := len(inner.calls), 1; got != want {
		t.Fatalf("inner.calls = %d, want %d", got, want)
	}
}

func TestInstrumentedExecutorRecordsRealisticDuration(t *testing.T) {
	t.Parallel()

	metricReader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	// Executor that sleeps to simulate work
	inner := &slowExecutor{delay: 50 * time.Millisecond}
	executor := &InstrumentedExecutor{
		Inner:       inner,
		Instruments: instruments,
	}

	ctx := context.Background()
	err = executor.Execute(ctx, Statement{Operation: OperationUpsertNode, Cypher: "MERGE (n:Test)"})
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	for _, scopeMetric := range rm.ScopeMetrics {
		for _, m := range scopeMetric.Metrics {
			if m.Name == "eshu_dp_neo4j_query_duration_seconds" {
				histogram := m.Data.(metricdata.Histogram[float64])
				if len(histogram.DataPoints) == 0 {
					t.Fatal("histogram has no data points")
				}
				dp := histogram.DataPoints[0]
				// Sum should be at least 50ms (0.05s)
				if dp.Sum < 0.05 {
					t.Fatalf("histogram sum = %f seconds, want >= 0.05", dp.Sum)
				}
				return
			}
		}
	}
	t.Fatal("histogram not found")
}

type slowExecutor struct {
	delay time.Duration
}

func (s *slowExecutor) Execute(_ context.Context, _ Statement) error {
	time.Sleep(s.delay)
	return nil
}

func TestInstrumentedExecutorForwardsExecuteGroup(t *testing.T) {
	t.Parallel()

	inner := &groupCapableExecutor{}
	ie := &InstrumentedExecutor{Inner: inner}

	stmts := []Statement{
		{Operation: OperationCanonicalRetract, Cypher: "MATCH (d) DETACH DELETE d"},
		{Operation: OperationCanonicalUpsert, Cypher: "MERGE (f:File {path: $path})"},
	}

	err := ie.ExecuteGroup(context.Background(), stmts)
	if err != nil {
		t.Fatalf("ExecuteGroup() error = %v", err)
	}

	if got := int(inner.executeGroupCalls.Load()); got != 1 {
		t.Errorf("executeGroupCalls = %d, want 1", got)
	}
	if got := int(inner.executeCalls.Load()); got != 0 {
		t.Errorf("executeCalls = %d, want 0", got)
	}
	if len(inner.groupStmts) != 2 {
		t.Errorf("forwarded stmts = %d, want 2", len(inner.groupStmts))
	}
}

func TestInstrumentedExecutorExecuteGroupRecordsPerStatementBatchMetrics(t *testing.T) {
	t.Parallel()

	metricReader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	inner := &groupCapableExecutor{}
	ie := &InstrumentedExecutor{Inner: inner, Instruments: instruments}

	stmts := []Statement{
		{
			Operation: OperationCanonicalUpsert,
			Cypher:    "UNWIND $rows AS row MERGE (f:File {path: row.path})",
			Parameters: map[string]any{
				"rows": []map[string]any{
					{"path": "/repo/a.go"},
					{"path": "/repo/b.go"},
				},
				StatementMetadataPhaseKey: CanonicalPhaseFiles,
			},
		},
		{
			Operation: OperationCanonicalUpsert,
			Cypher:    "UNWIND $rows AS row MERGE (n:Function {uid: row.entity_id})",
			Parameters: map[string]any{
				"rows": []map[string]any{
					{"entity_id": "function-1"},
				},
				StatementMetadataPhaseKey:       CanonicalPhaseEntities,
				StatementMetadataEntityLabelKey: "Function",
			},
		},
	}

	if err := ie.ExecuteGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecuteGroup() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	assertFloat64HistogramCount(t, rm, "eshu_dp_neo4j_batch_size", map[string]string{
		"operation":   string(OperationCanonicalUpsert),
		"write_phase": CanonicalPhaseFiles,
	}, 1)
	assertFloat64HistogramCount(t, rm, "eshu_dp_neo4j_batch_size", map[string]string{
		"operation":   string(OperationCanonicalUpsert),
		"write_phase": CanonicalPhaseEntities,
		"node_type":   "Function",
	}, 1)
	assertInt64CounterValue(t, rm, "eshu_dp_neo4j_batches_executed_total", map[string]string{
		"operation":   string(OperationCanonicalUpsert),
		"write_phase": CanonicalPhaseFiles,
	}, 1)
	assertFloat64HistogramCount(t, rm, "eshu_dp_neo4j_query_duration_seconds", map[string]string{
		"operation": "write_group",
	}, 1)
}

func TestInstrumentedExecutorExecuteGroupErrorsWithoutGroupExecutor(t *testing.T) {
	t.Parallel()

	inner := &recordingExecutor{}
	ie := &InstrumentedExecutor{Inner: inner}

	err := ie.ExecuteGroup(context.Background(), []Statement{{Cypher: "test"}})
	if err == nil {
		t.Fatal("expected error when Inner does not implement GroupExecutor")
	}
}

func TestInstrumentedExecutorExecuteGroupPropagatesErrors(t *testing.T) {
	t.Parallel()

	inner := &groupCapableExecutor{groupErr: errors.New("neo4j transaction failed")}
	ie := &InstrumentedExecutor{Inner: inner}

	err := ie.ExecuteGroup(context.Background(), []Statement{{Cypher: "test"}})
	if err == nil {
		t.Fatal("expected error to propagate from inner ExecuteGroup")
	}
	if err.Error() != "neo4j transaction failed" {
		t.Fatalf("error = %v, want 'neo4j transaction failed'", err)
	}
}

func TestExecuteOnlyExecutorHidesGroupExecutor(t *testing.T) {
	t.Parallel()

	inner := &groupCapableExecutor{}
	executor := ExecuteOnlyExecutor{Inner: inner}
	if _, ok := any(executor).(GroupExecutor); ok {
		t.Fatal("ExecuteOnlyExecutor implements GroupExecutor, want execute-only surface")
	}

	err := executor.Execute(context.Background(), Statement{Cypher: "RETURN 1"})
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if got := int(inner.executeCalls.Load()); got != 1 {
		t.Fatalf("inner Execute calls = %d, want 1", got)
	}
}
