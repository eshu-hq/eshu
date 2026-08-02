// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Begin proxies to the inner database if it implements Beginner, wrapping the
// returned transaction so its ExecContext/QueryContext calls still record
// eshu_dp_postgres_query_duration_seconds (#5848 adjacent fix).
//
// Before this, InstrumentedDB.Begin returned the RAW inner transaction
// unwrapped: every statement issued inside it bypassed InstrumentedDB's
// instrumentation entirely, silently. That was latent as long as no
// production writer used a transaction over an InstrumentedDB-wrapped
// connection; aws_cloud_runtime_drift's insert-admission/insert/retire
// transaction (#5848) is the first to route real per-write Postgres
// statements through Begin, and go/internal/replay/costcounting's committed
// cost budget for that domain caught the resulting silent drop to zero
// observations before this shipped (a false-green histogram is the exact
// shape the cost-counting gate's "observations == 0" guard exists to catch).
// PostgresServiceMaterializationWriter (#1943) already routes its commits
// through this same unwrapped path in production; this fix closes the gap for
// it too, though no committed cost budget exercises it today.
func (db *InstrumentedDB) Begin(ctx context.Context) (Transaction, error) {
	beginner, ok := db.Inner.(Beginner)
	if !ok {
		return nil, fmt.Errorf("inner database does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return db.instrumentTransaction(tx), nil
}

// BeginReadOnlyRepeatableRead proxies the snapshot transaction capability and
// preserves query instrumentation inside the returned transaction.
func (db *InstrumentedDB) BeginReadOnlyRepeatableRead(ctx context.Context) (Transaction, error) {
	beginner, ok := db.Inner.(ReadOnlyRepeatableReadBeginner)
	if !ok {
		return nil, fmt.Errorf("inner database does not support read-only repeatable-read transactions")
	}
	tx, err := beginner.BeginReadOnlyRepeatableRead(ctx)
	if err != nil {
		return nil, err
	}
	return db.instrumentTransaction(tx), nil
}

func (db *InstrumentedDB) instrumentTransaction(tx Transaction) Transaction {
	return &instrumentedTransaction{tx: tx, instruments: db.Instruments, storeName: db.StoreName}
}

// instrumentedTransaction wraps a Transaction so its ExecContext/QueryContext
// calls record the same eshu_dp_postgres_query_duration_seconds histogram
// InstrumentedDB records for non-transactional calls.
type instrumentedTransaction struct {
	tx          Transaction
	instruments *telemetry.Instruments
	storeName   string
}

// ExecContext wraps the inner transaction's ExecContext with the same
// duration metric InstrumentedDB.ExecContext records.
func (t *instrumentedTransaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	start := time.Now()
	result, err := t.tx.ExecContext(ctx, query, args...)
	t.record(ctx, "write", start)
	return result, err
}

// QueryContext wraps the inner transaction's QueryContext with the same
// duration metric InstrumentedDB.QueryContext records.
func (t *instrumentedTransaction) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	start := time.Now()
	rows, err := t.tx.QueryContext(ctx, query, args...)
	t.record(ctx, "read", start)
	return rows, err
}

func (t *instrumentedTransaction) record(ctx context.Context, operation string, start time.Time) {
	if t.instruments == nil {
		return
	}
	t.instruments.PostgresQueryDuration.Record(
		ctx, time.Since(start).Seconds(),
		metric.WithAttributes(
			attribute.String("operation", operation),
			attribute.String("store", t.storeName),
		),
	)
}

// Commit commits the wrapped transaction.
func (t *instrumentedTransaction) Commit() error { return t.tx.Commit() }

// Rollback rolls back the wrapped transaction.
func (t *instrumentedTransaction) Rollback() error { return t.tx.Rollback() }
