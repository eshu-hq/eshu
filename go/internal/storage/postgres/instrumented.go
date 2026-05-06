// Package postgres provides OTEL-instrumented wrappers for Postgres storage operations.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// InstrumentedDB wraps an ExecQueryer with OTEL tracing and metrics.
// It decorates each database operation with spans and duration metrics.
type InstrumentedDB struct {
	Inner       ExecQueryer
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	StoreName   string // e.g. "facts", "queue", "content", "decisions", "intents"
}

// ExecContext wraps the inner ExecContext with tracing and metrics.
func (db *InstrumentedDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	start := time.Now()

	// Create span if tracer is available
	if db.Tracer != nil {
		var span trace.Span
		ctx, span = db.Tracer.Start(ctx, "postgres.exec",
			trace.WithAttributes(
				attribute.String("db.system", "postgresql"),
				attribute.String("db.operation", "exec"),
				attribute.String("eshu.store", db.StoreName),
			),
		)
		defer span.End()

		// Execute the query
		result, err := db.Inner.ExecContext(ctx, query, args...)

		// Record error in span if present
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}

		// Record duration metric if instruments are available
		if db.Instruments != nil {
			duration := time.Since(start).Seconds()
			db.Instruments.PostgresQueryDuration.Record(ctx, duration,
				metric.WithAttributes(
					attribute.String("operation", "write"),
					attribute.String("store", db.StoreName),
				),
			)
		}

		return result, err
	}

	// No tracer, just execute and optionally record metric
	result, err := db.Inner.ExecContext(ctx, query, args...)

	if db.Instruments != nil {
		duration := time.Since(start).Seconds()
		db.Instruments.PostgresQueryDuration.Record(ctx, duration,
			metric.WithAttributes(
				attribute.String("operation", "write"),
				attribute.String("store", db.StoreName),
			),
		)
	}

	return result, err
}

// QueryContext wraps the inner QueryContext with tracing and metrics.
func (db *InstrumentedDB) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	start := time.Now()

	// Create span if tracer is available
	if db.Tracer != nil {
		var span trace.Span
		ctx, span = db.Tracer.Start(ctx, "postgres.query",
			trace.WithAttributes(
				attribute.String("db.system", "postgresql"),
				attribute.String("db.operation", "query"),
				attribute.String("eshu.store", db.StoreName),
			),
		)
		defer span.End()

		// Execute the query
		rows, err := db.Inner.QueryContext(ctx, query, args...)

		// Record error in span if present
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}

		// Record duration metric if instruments are available
		if db.Instruments != nil {
			duration := time.Since(start).Seconds()
			db.Instruments.PostgresQueryDuration.Record(ctx, duration,
				metric.WithAttributes(
					attribute.String("operation", "read"),
					attribute.String("store", db.StoreName),
				),
			)
		}

		return rows, err
	}

	// No tracer, just execute and optionally record metric
	rows, err := db.Inner.QueryContext(ctx, query, args...)

	if db.Instruments != nil {
		duration := time.Since(start).Seconds()
		db.Instruments.PostgresQueryDuration.Record(ctx, duration,
			metric.WithAttributes(
				attribute.String("operation", "read"),
				attribute.String("store", db.StoreName),
			),
		)
	}

	return rows, err
}

// Begin proxies to the inner database if it implements Beginner.
// This allows InstrumentedDB to satisfy the Beginner interface when the
// underlying connection supports transactions (e.g. SQLDB).
func (db *InstrumentedDB) Begin(ctx context.Context) (Transaction, error) {
	if beginner, ok := db.Inner.(Beginner); ok {
		return beginner.Begin(ctx)
	}
	return nil, fmt.Errorf("inner database does not support transactions")
}
