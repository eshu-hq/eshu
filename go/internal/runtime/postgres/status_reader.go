// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const statusSnapshotLimit = 5 * time.Second

// statusSnapshotJITOffSQL disables PostgreSQL JIT for the rest of one status
// snapshot transaction (#7009). SET LOCAL ends with the transaction on both
// Commit and Rollback, so no pooled connection keeps the setting.
const statusSnapshotJITOffSQL = "SET LOCAL jit = off"

type snapshotStatusReader struct {
	store   db.ReadStore
	factory func(db.Queryer) status.Reader
	tracer  trace.Tracer
}

var (
	_ status.Reader           = snapshotStatusReader{}
	_ status.ReadinessChecker = snapshotStatusReader{}
)

// NewSnapshotStatusReader confines each full or filtered status read to one
// guarded repeatable-read snapshot. The caller must provide a checkpoint first.
func NewSnapshotStatusReader(store db.ReadStore, factory func(db.Queryer) status.Reader, tracer trace.Tracer) status.Reader {
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("postgres.status_snapshot")
	}
	return snapshotStatusReader{store: store, factory: factory, tracer: tracer}
}

// ReadStatusSnapshot loads all status sections from one bounded transaction.
func (r snapshotStatusReader) ReadStatusSnapshot(ctx context.Context, asOf time.Time) (status.RawSnapshot, error) {
	return r.read(ctx, asOf, status.FullSnapshotSelection())
}

// ReadStatusSnapshotFiltered loads selected status sections from one bounded transaction.
func (r snapshotStatusReader) ReadStatusSnapshotFiltered(ctx context.Context, asOf time.Time, selection status.SnapshotSelection) (status.RawSnapshot, error) {
	return r.read(ctx, asOf, selection)
}

func (r snapshotStatusReader) read(ctx context.Context, asOf time.Time, selection status.SnapshotSelection) (raw status.RawSnapshot, err error) {
	if err := selection.Validate(); err != nil {
		return status.RawSnapshot{}, err
	}
	if r.store == nil || r.factory == nil {
		return status.RawSnapshot{}, errors.New("snapshot status reader is not configured")
	}
	bounded, cancel := context.WithTimeout(ctx, statusSnapshotLimit)
	defer cancel()
	bounded, span := r.tracer.Start(bounded, StatusSnapshotSpanName)
	phase := "begin"
	panicked := false
	defer func() {
		outcome := statusSnapshotOutcome(err)
		if panicked {
			outcome = "panic"
		}
		span.SetAttributes(attribute.String(statusSnapshotPhaseKey, phase), attribute.String(statusSnapshotOutcomeKey, outcome))
		if outcome != "ok" {
			span.SetStatus(codes.Error, outcome)
		}
		span.End()
	}()

	tx, beginErr := r.store.BeginReadOnlySnapshot(bounded)
	if beginErr != nil {
		return status.RawSnapshot{}, beginErr
	}
	if tx == nil {
		return status.RawSnapshot{}, errors.New("snapshot status transaction is unavailable")
	}
	finished := false
	defer func() {
		if finished {
			return
		}
		cleanupErr := withoutExpectedTxDone(tx.Rollback())
		if recovered := recover(); recovered != nil {
			panicked = true
			phase = "rollback"
			if cleanupErr != nil {
				span.RecordError(cleanupErr)
			}
			panic(recovered)
		}
		if cleanupErr != nil {
			phase = "rollback"
			raw = status.RawSnapshot{}
			err = errors.Join(err, cleanupErr)
		}
	}()

	phase = statusSnapshotPhaseJIT
	if err = disableStatusSnapshotJIT(bounded, tx); err != nil {
		return status.RawSnapshot{}, err
	}
	span.SetAttributes(attribute.String(statusSnapshotJITKey, statusSnapshotJITOff))

	phase = "read"
	reader := r.factory(tx)
	if reader == nil {
		return status.RawSnapshot{}, errors.New("snapshot status factory returned no reader")
	}
	raw, err = reader.ReadStatusSnapshotFiltered(bounded, asOf, selection)
	if err != nil {
		return status.RawSnapshot{}, err
	}
	if err = bounded.Err(); err != nil {
		return status.RawSnapshot{}, err
	}
	phase = "commit"
	err = tx.Commit()
	finished = true
	if err != nil {
		return status.RawSnapshot{}, err
	}
	if err = bounded.Err(); err != nil {
		return status.RawSnapshot{}, err
	}
	return raw, nil
}

// readTransactionControl is implemented only by this package's guarded
// readTransaction. It runs transaction control SQL without a reader
// query-start event or a business_query stage observation.
type readTransactionControl interface {
	execControl(ctx context.Context, statement string) error
}

// disableStatusSnapshotJIT applies statusSnapshotJITOffSQL to tx. The guarded
// reader sends it as control SQL so per-request business query counts stay
// unchanged; any other ReadTransaction receives it through QueryContext.
func disableStatusSnapshotJIT(ctx context.Context, tx db.ReadTransaction) error {
	if control, ok := tx.(readTransactionControl); ok {
		return control.execControl(ctx, statusSnapshotJITOffSQL)
	}
	rows, err := tx.QueryContext(ctx, statusSnapshotJITOffSQL)
	if err != nil {
		return err
	}
	return errors.Join(rows.Err(), rows.Close())
}

// CheckStatusReadiness uses one guarded query without holding a status snapshot.
func (r snapshotStatusReader) CheckStatusReadiness(ctx context.Context) error {
	if r.store == nil || r.factory == nil {
		return errors.New("snapshot status reader is not configured")
	}
	reader := r.factory(r.store)
	checker, ok := reader.(status.ReadinessChecker)
	if !ok || checker == nil {
		return errors.New("status reader does not support readiness checks")
	}
	bounded, cancel := context.WithTimeout(ctx, statusSnapshotLimit)
	defer cancel()
	return checker.CheckStatusReadiness(bounded)
}

func statusSnapshotOutcome(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case err != nil:
		return "error"
	default:
		return "ok"
	}
}

// withoutExpectedTxDone removes only database/sql's already-finished marker
// from cleanup; a joined driver or connection-close error remains visible.
func withoutExpectedTxDone(err error) error {
	if err == nil || err == sql.ErrTxDone {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var kept []error
		for _, member := range joined.Unwrap() {
			if item := withoutExpectedTxDone(member); item != nil {
				kept = append(kept, item)
			}
		}
		return errors.Join(kept...)
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok && wrapped.Unwrap() != nil {
		if withoutExpectedTxDone(wrapped.Unwrap()) == nil {
			return nil
		}
	}
	return err
}
