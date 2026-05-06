package cypher

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentedExecutor wraps a Cypher Executor with OTEL tracing and metrics.
// Both Tracer and Instruments are optional; if nil, the wrapper passes through
// without instrumentation overhead.
type InstrumentedExecutor struct {
	Inner       Executor
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
}

// Execute executes a Neo4j statement with optional OTEL tracing and metrics.
//
// If Tracer is non-nil, creates a child span named "neo4j.execute" with
// attributes db.system=neo4j and db.operation=<statement.Operation>.
//
// If Instruments is non-nil, records the execution duration on the
// eshu_dp_neo4j_query_duration_seconds histogram with attribute operation=write.
//
// On error, sets span status to error if tracing is enabled, then returns
// the error unchanged.
func (i *InstrumentedExecutor) Execute(ctx context.Context, statement Statement) error {
	start := time.Now()

	// Start span if tracer is available
	var span trace.Span
	if i.Tracer != nil {
		ctx, span = i.Tracer.Start(ctx, "neo4j.execute",
			trace.WithAttributes(
				attribute.String("db.system", "neo4j"),
				attribute.String("db.operation", string(statement.Operation)),
			),
		)
		defer span.End()
	}

	// Execute the inner statement
	err := i.Inner.Execute(ctx, statement)

	// Record duration if instruments are available
	if i.Instruments != nil {
		duration := time.Since(start).Seconds()
		i.Instruments.Neo4jQueryDuration.Record(ctx, duration, metric.WithAttributes(
			attribute.String("operation", "write"),
		))
	}

	i.recordStatementBatchMetrics(ctx, statement)

	// Set span status on error
	if err != nil && span != nil {
		span.SetStatus(codes.Error, err.Error())
	}

	return err
}

// ExecuteGroup forwards to Inner.ExecuteGroup when the inner executor
// implements GroupExecutor. Returns an error if it does not.
func (i *InstrumentedExecutor) ExecuteGroup(ctx context.Context, stmts []Statement) error {
	ge, ok := i.Inner.(GroupExecutor)
	if !ok {
		return fmt.Errorf("inner executor does not support ExecuteGroup")
	}

	start := time.Now()

	var span trace.Span
	if i.Tracer != nil {
		ctx, span = i.Tracer.Start(ctx, "neo4j.execute_group",
			trace.WithAttributes(
				attribute.String("db.system", "neo4j"),
				attribute.Int("db.statement_count", len(stmts)),
			),
		)
		defer span.End()
	}

	err := ge.ExecuteGroup(ctx, stmts)

	if i.Instruments != nil && i.Instruments.Neo4jQueryDuration != nil {
		duration := time.Since(start).Seconds()
		i.Instruments.Neo4jQueryDuration.Record(ctx, duration, metric.WithAttributes(
			attribute.String("operation", "write_group"),
		))
	}
	for _, stmt := range stmts {
		i.recordStatementBatchMetrics(ctx, stmt)
	}

	if err != nil && span != nil {
		span.SetStatus(codes.Error, err.Error())
	}

	return err
}

// recordStatementBatchMetrics emits one bounded row-count signal per UNWIND
// statement. Grouped Neo4j writes use this to expose the same phase and label
// clues available to NornicDB phase-group logs without splitting the transaction.
func (i *InstrumentedExecutor) recordStatementBatchMetrics(ctx context.Context, statement Statement) {
	if i.Instruments == nil || i.Instruments.Neo4jBatchSize == nil || i.Instruments.Neo4jBatchesExecuted == nil {
		return
	}
	rowCount, ok := statementRowsCount(statement)
	if !ok {
		return
	}
	attrs := statementBatchMetricAttributes(statement)
	i.Instruments.Neo4jBatchSize.Record(ctx, float64(rowCount), metric.WithAttributes(attrs...))
	i.Instruments.Neo4jBatchesExecuted.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// statementRowsCount returns the row count for common UNWIND parameter shapes.
func statementRowsCount(statement Statement) (int, bool) {
	rows, ok := statement.Parameters["rows"]
	if !ok {
		return 0, false
	}
	switch typed := rows.(type) {
	case []map[string]any:
		return len(typed), true
	case []map[string]string:
		return len(typed), true
	case []any:
		return len(typed), true
	default:
		return 0, false
	}
}

// statementBatchMetricAttributes keeps grouped-write batch labels bounded to
// operation, canonical phase, and node type.
func statementBatchMetricAttributes(statement Statement) []attribute.KeyValue {
	attrs := []attribute.KeyValue{attribute.String("operation", string(statement.Operation))}
	if phase, ok := statement.Parameters[StatementMetadataPhaseKey].(string); ok && phase != "" {
		attrs = append(attrs, telemetry.AttrWritePhase(phase))
	}
	if label, ok := statement.Parameters[StatementMetadataEntityLabelKey].(string); ok && label != "" {
		attrs = append(attrs, telemetry.AttrNodeType(label))
	}
	return attrs
}
