// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package postgres provides OTEL-instrumented wrappers for Postgres storage operations.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// InstrumentedDB wraps an ExecQueryer with OTEL tracing and metrics.
// It decorates each database operation with spans and duration metrics.
type InstrumentedDB struct {
	Inner       db.ExecQueryer
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	StoreName   string // e.g. "facts", "queue", "content", "decisions", "intents"
}

// ExecContext wraps the inner ExecContext with tracing and metrics.
func (database *InstrumentedDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	start := time.Now()

	// Create span if tracer is available
	if database.Tracer != nil {
		var span trace.Span
		ctx, span = database.Tracer.Start(
			ctx, "postgres.exec",
			trace.WithAttributes(
				attribute.String("db.system", "postgresql"),
				attribute.String("db.operation", "exec"),
				attribute.String("eshu.store", database.StoreName),
			),
		)
		defer span.End()

		// Execute the query
		result, err := database.Inner.ExecContext(ctx, query, args...)
		// Record error in span if present
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}

		// Record duration metric if instruments are available
		if database.Instruments != nil {
			duration := time.Since(start).Seconds()
			database.Instruments.PostgresQueryDuration.Record(
				ctx, duration,
				metric.WithAttributes(
					attribute.String("operation", "write"),
					attribute.String("store", database.StoreName),
				),
			)
		}

		return result, err
	}

	// No tracer, just execute and optionally record metric
	result, err := database.Inner.ExecContext(ctx, query, args...)

	if database.Instruments != nil {
		duration := time.Since(start).Seconds()
		database.Instruments.PostgresQueryDuration.Record(
			ctx, duration,
			metric.WithAttributes(
				attribute.String("operation", "write"),
				attribute.String("store", database.StoreName),
			),
		)
	}

	return result, err
}

// QueryContext wraps the inner QueryContext with tracing and metrics.
func (database *InstrumentedDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	start := time.Now()

	// Create span if tracer is available
	if database.Tracer != nil {
		var span trace.Span
		ctx, span = database.Tracer.Start(
			ctx, "postgres.query",
			trace.WithAttributes(
				attribute.String("db.system", "postgresql"),
				attribute.String("db.operation", "query"),
				attribute.String("eshu.store", database.StoreName),
			),
		)
		defer span.End()

		// Execute the query
		rows, err := database.Inner.QueryContext(ctx, query, args...)
		// Record error in span if present
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}

		// Record duration metric if instruments are available
		if database.Instruments != nil {
			duration := time.Since(start).Seconds()
			database.Instruments.PostgresQueryDuration.Record(
				ctx, duration,
				metric.WithAttributes(
					attribute.String("operation", "read"),
					attribute.String("store", database.StoreName),
				),
			)
		}

		return rows, err
	}

	// No tracer, just execute and optionally record metric
	rows, err := database.Inner.QueryContext(ctx, query, args...)

	if database.Instruments != nil {
		duration := time.Since(start).Seconds()
		database.Instruments.PostgresQueryDuration.Record(
			ctx, duration,
			metric.WithAttributes(
				attribute.String("operation", "read"),
				attribute.String("store", database.StoreName),
			),
		)
	}

	return rows, err
}

// CopySearchIndexTerms wraps the optional SQLDB COPY fast path with the same
// tracing and Postgres duration metric shape used by ordinary write queries.
func (database *InstrumentedDB) CopySearchIndexTerms(
	ctx context.Context,
	scopeID string,
	generationID string,
	documentIDs []string,
	terms []string,
	termKeys []string,
	frequencies []int,
) (int64, error) {
	copier, ok := database.Inner.(interface {
		CopySearchIndexTerms(context.Context, string, string, []string, []string, []string, []int) (int64, error)
	})
	if !ok {
		return 0, searchIndexTermCopyUnsupportedError{driver: fmt.Sprintf("%T", database.Inner)}
	}

	start := time.Now()
	if database.Tracer != nil {
		var span trace.Span
		ctx, span = database.Tracer.Start(
			ctx,
			"postgres.copy_from",
			trace.WithAttributes(
				attribute.String("db.system", "postgresql"),
				attribute.String("db.operation", "copy_from"),
				attribute.String("eshu.store", database.StoreName),
			),
		)
		defer span.End()

		copied, err := copier.CopySearchIndexTerms(ctx, scopeID, generationID, documentIDs, terms, termKeys, frequencies)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		if database.Instruments != nil {
			database.Instruments.PostgresQueryDuration.Record(
				ctx,
				time.Since(start).Seconds(),
				metric.WithAttributes(
					attribute.String("operation", "write"),
					attribute.String("store", database.StoreName),
				),
			)
		}
		return copied, err
	}

	copied, err := copier.CopySearchIndexTerms(ctx, scopeID, generationID, documentIDs, terms, termKeys, frequencies)
	if database.Instruments != nil {
		database.Instruments.PostgresQueryDuration.Record(
			ctx,
			time.Since(start).Seconds(),
			metric.WithAttributes(
				attribute.String("operation", "write"),
				attribute.String("store", database.StoreName),
			),
		)
	}
	return copied, err
}
