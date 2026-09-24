// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
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
	statement, skip := GuardStatementIndexKeys(ctx, statement, i.Instruments)
	if skip {
		return nil
	}
	start := time.Now()

	// Start span if tracer is available
	var span trace.Span
	if i.Tracer != nil {
		ctx, span = i.Tracer.Start(
			ctx, "neo4j.execute",
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
	stmts = GuardStatementsIndexKeys(ctx, stmts, i.Instruments)
	if len(stmts) == 0 {
		return nil
	}

	start := time.Now()

	var span trace.Span
	if i.Tracer != nil {
		ctx, span = i.Tracer.Start(
			ctx, "neo4j.execute_group",
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

// ExecuteProbe forwards to Inner.ExecuteProbe when the inner executor
// implements ProbeExecutor. Returns an error without attempting the probe
// when it does not, mirroring ExecuteGroup's fail-closed capability check
// (#5998 review F1): InstrumentedExecutor wraps the reducer's base
// reducerNeo4jExecutor in production (observed_service_wiring.go) BELOW the
// backpressure gates and ABOVE the retry/adapter seam, so a missing forward
// here makes the outer chain still type-assert as ProbeExecutor (every
// wrapper above this one always has the method) while every call silently
// dead-ends into "unsupported" -- exactly the failure this fixes.
func (i *InstrumentedExecutor) ExecuteProbe(ctx context.Context, stmt Statement) (bool, error) {
	pe, ok := i.Inner.(ProbeExecutor)
	if !ok {
		return false, fmt.Errorf("inner executor does not support ExecuteProbe")
	}

	start := time.Now()

	var span trace.Span
	if i.Tracer != nil {
		ctx, span = i.Tracer.Start(
			ctx, "neo4j.execute_probe",
			trace.WithAttributes(
				attribute.String("db.system", "neo4j"),
				attribute.String("db.operation", string(stmt.Operation)),
			),
		)
		defer span.End()
	}

	found, err := pe.ExecuteProbe(ctx, stmt)

	if i.Instruments != nil && i.Instruments.Neo4jQueryDuration != nil {
		duration := time.Since(start).Seconds()
		i.Instruments.Neo4jQueryDuration.Record(ctx, duration, metric.WithAttributes(
			attribute.String("operation", "probe"),
		))
	}

	if err != nil && span != nil {
		span.SetStatus(codes.Error, err.Error())
	}

	return found, err
}

// recordStatementBatchMetrics emits one bounded row-count signal per UNWIND
// statement. Grouped Neo4j writes use this to expose the same phase and label
// clues available to NornicDB phase-group logs without splitting the transaction.
func (i *InstrumentedExecutor) recordStatementBatchMetrics(ctx context.Context, statement Statement) {
	if i.Instruments == nil || i.Instruments.Neo4jBatchSize == nil || i.Instruments.Neo4jBatchesExecuted == nil {
		return
	}
	rowCount, ok := StatementRowsCount(statement)
	if !ok {
		return
	}
	attrs := statementBatchMetricAttributes(statement)
	i.Instruments.Neo4jBatchSize.Record(ctx, float64(rowCount), metric.WithAttributes(attrs...))
	i.Instruments.Neo4jBatchesExecuted.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// StatementRowsCount returns the row count for common UNWIND parameter shapes.
func StatementRowsCount(statement Statement) (int, bool) {
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

// GuardStatementIndexKeys applies graph.GuardIndexKeyWrites to one write
// statement: rows that would put more than graph.MaxIndexKeyBytes into a
// schema index key are removed before the backend sees them, so one
// oversized source value skips one node (and the edges its row carries)
// instead of failing the atomic write it rides in (#7058). Each removed row
// logs one WARN and adds one eshu_dp_graph_oversized_index_keys_skipped_total
// increment. It returns skip=true when the indexed value came from a scalar
// parameter; the caller must then not execute the statement.
//
// Every production graph write passes through InstrumentedExecutor, which
// calls this, so the guard is backend-neutral and covers every writer. It sits
// above the transient-retry executor, so a driver retry does not count again;
// a work-item retry that rebuilds the write does.
func GuardStatementIndexKeys(ctx context.Context, statement Statement, instruments *telemetry.Instruments) (Statement, bool) {
	statement, _, skip := guardStatementIndexKeys(ctx, statement, instruments)
	return statement, skip
}

// guardStatementIndexKeys is GuardStatementIndexKeys that also returns how
// many rows it removed.
func guardStatementIndexKeys(ctx context.Context, statement Statement, instruments *telemetry.Instruments) (Statement, int, bool) {
	params, dropped, skip := graph.GuardIndexKeyWrites(statement.Cypher, statement.Parameters)
	for _, d := range dropped {
		slog.WarnContext(
			ctx, "graph write skipped: indexed value exceeds key size limit",
			"operation", string(statement.Operation),
			"node_label", d.Label,
			"property", d.Property,
			"key_bytes", d.KeyBytes,
			"limit_bytes", graph.MaxIndexKeyBytes,
			"param", d.Param,
			"scope_id", d.ScopeID,
			"repo_id", d.RepoID,
			"generation_id", d.GenerationID,
			"entity_id", d.EntityID,
			"file_path", d.FilePath,
			"value_prefix", d.ValuePrefix,
			"statement_skipped", skip,
		)
		if instruments != nil && instruments.GraphOversizedIndexKeysSkipped != nil {
			instruments.GraphOversizedIndexKeysSkipped.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrNodeLabel(d.Label),
				telemetry.AttrProperty(d.Property),
			))
		}
	}
	if len(dropped) > 0 {
		trace.SpanFromContext(ctx).SetAttributes(attribute.Int("oversized_index_keys_skipped", len(dropped)))
	}
	statement.Parameters = params
	return statement, len(dropped), skip
}

// guardStatementsIndexKeys applies GuardStatementIndexKeys to every statement
// of a group, removing statements it reports as skipped. The input slice is
// returned unchanged when nothing is dropped.
func GuardStatementsIndexKeys(ctx context.Context, stmts []Statement, instruments *telemetry.Instruments) []Statement {
	var out []Statement
	for idx, stmt := range stmts {
		guarded, dropped, skip := guardStatementIndexKeys(ctx, stmt, instruments)
		if out == nil && dropped == 0 {
			continue
		}
		if out == nil {
			out = make([]Statement, idx, len(stmts))
			copy(out, stmts[:idx])
		}
		if !skip {
			out = append(out, guarded)
		}
	}
	if out == nil {
		return stmts
	}
	return out
}
