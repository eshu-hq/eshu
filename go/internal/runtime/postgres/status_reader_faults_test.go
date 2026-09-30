// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSnapshotStatusReaderBeginFactoryAndReadinessFailures(t *testing.T) {
	beginErr := errors.New("begin unavailable")
	store := snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return nil, beginErr }}
	reader := NewSnapshotStatusReader(store, func(db.Queryer) status.Reader { t.Fatal("factory called after begin failure"); return nil }, nil)
	if _, err := reader.ReadStatusSnapshot(t.Context(), time.Now()); !errors.Is(err, beginErr) {
		t.Fatalf("begin error = %v", err)
	}
	tx := &snapshotTxStub{}
	store.begin = func(context.Context) (db.ReadTransaction, error) { return tx, nil }
	reader = NewSnapshotStatusReader(store, func(db.Queryer) status.Reader { return nil }, nil)
	if _, err := reader.ReadStatusSnapshot(t.Context(), time.Now()); err == nil || tx.rollbacks != 1 {
		t.Fatalf("nil factory result error=%v rollback=%d", err, tx.rollbacks)
	}
	reader = NewSnapshotStatusReader(nil, nil, nil)
	if _, err := reader.ReadStatusSnapshot(t.Context(), time.Now()); err == nil {
		t.Fatal("missing store/factory accepted")
	}
	base := &store
	checker := NewSnapshotStatusReader(base, func(q db.Queryer) status.Reader {
		if q != base {
			t.Fatal("readiness factory did not receive base guarded store")
		}
		return snapshotReaderStub{ready: func(context.Context) error { return beginErr }}
	}, nil).(status.ReadinessChecker)
	if err := checker.CheckStatusReadiness(t.Context()); !errors.Is(err, beginErr) || tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatalf("readiness error=%v commit=%d rollback=%d", err, tx.commits, tx.rollbacks)
	}
	unsupported := NewSnapshotStatusReader(store, func(db.Queryer) status.Reader {
		return snapshotReadOnlyStub{}
	}, nil).(status.ReadinessChecker)
	if err := unsupported.CheckStatusReadiness(t.Context()); err == nil {
		t.Fatal("unsupported readiness silently succeeded")
	}
}

type snapshotReadOnlyStub struct{}

func (snapshotReadOnlyStub) ReadStatusSnapshot(context.Context, time.Time) (status.RawSnapshot, error) {
	return status.RawSnapshot{}, nil
}

func (snapshotReadOnlyStub) ReadStatusSnapshotFiltered(context.Context, time.Time, status.SnapshotSelection) (status.RawSnapshot, error) {
	return status.RawSnapshot{}, nil
}

func TestSnapshotStatusReaderReadScanDecodeAndRollbackErrors(t *testing.T) {
	for _, name := range []string{"query", "scan", "decode"} {
		t.Run(name, func(t *testing.T) {
			primary := errors.New(name)
			closeErr := errors.New("connection close")
			tx := &snapshotTxStub{rollbackErr: errors.Join(sql.ErrTxDone, closeErr)}
			reader := NewSnapshotStatusReader(snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return tx, nil }},
				func(db.Queryer) status.Reader {
					return snapshotReaderStub{read: func(context.Context, status.SnapshotSelection) (status.RawSnapshot, error) {
						return status.RawSnapshot{AsOf: time.Now()}, primary
					}}
				}, nil)
			raw, err := reader.ReadStatusSnapshot(t.Context(), time.Now())
			if !raw.AsOf.IsZero() || !errors.Is(err, primary) || !errors.Is(err, closeErr) || errors.Is(err, sql.ErrTxDone) || tx.commits != 0 || tx.rollbacks != 1 {
				t.Fatalf("raw=%v error=%v commit=%d rollback=%d", raw.AsOf, err, tx.commits, tx.rollbacks)
			}
		})
	}
}

func TestSnapshotStatusReaderEarlierDeadlineAndPanicRollback(t *testing.T) {
	parent, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	deadline, _ := parent.Deadline()
	tx := &snapshotTxStub{}
	store := snapshotBeginStub{begin: func(ctx context.Context) (db.ReadTransaction, error) {
		got, _ := ctx.Deadline()
		if got.After(deadline) {
			t.Fatalf("status deadline %v extended parent %v", got, deadline)
		}
		return tx, nil
	}}
	reader := NewSnapshotStatusReader(store, func(db.Queryer) status.Reader {
		return snapshotReaderStub{read: func(ctx context.Context, _ status.SnapshotSelection) (status.RawSnapshot, error) {
			<-ctx.Done()
			return status.RawSnapshot{}, ctx.Err()
		}}
	}, nil)
	if _, err := reader.ReadStatusSnapshot(parent, time.Now()); !errors.Is(err, context.DeadlineExceeded) || tx.rollbacks != 1 {
		t.Fatalf("deadline error=%v rollback=%d", err, tx.rollbacks)
	}
	panicked := &snapshotTxStub{}
	reader = NewSnapshotStatusReader(snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return panicked, nil }},
		func(db.Queryer) status.Reader { panic("decode panic") }, nil)
	func() {
		defer func() {
			if recovered := recover(); recovered != "decode panic" {
				t.Fatalf("panic = %v", recovered)
			}
		}()
		_, _ = reader.ReadStatusSnapshot(t.Context(), time.Now())
	}()
	if panicked.rollbacks != 1 || panicked.commits != 0 {
		t.Fatalf("panic cleanup commit=%d rollback=%d", panicked.commits, panicked.rollbacks)
	}
}

func TestSnapshotStatusReaderConcurrentIndependentTransactions(t *testing.T) {
	const n = 8
	var mu sync.Mutex
	transactions := make([]*snapshotTxStub, 0, n)
	store := snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) {
		tx := &snapshotTxStub{}
		mu.Lock()
		transactions = append(transactions, tx)
		mu.Unlock()
		return tx, nil
	}}
	reader := NewSnapshotStatusReader(store, func(q db.Queryer) status.Reader {
		tx := q.(*snapshotTxStub)
		return snapshotReaderStub{read: func(context.Context, status.SnapshotSelection) (status.RawSnapshot, error) {
			if tx.commits != 0 {
				return status.RawSnapshot{}, errors.New("premature commit")
			}
			return status.RawSnapshot{AsOf: time.Now()}, nil
		}}
	}, nil)
	var wg sync.WaitGroup
	failures := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := reader.ReadStatusSnapshot(t.Context(), time.Now()); failures <- err }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(transactions) != n {
		t.Fatalf("transactions=%d, want %d", len(transactions), n)
	}
	for i, tx := range transactions {
		if tx.commits != 1 || tx.rollbacks != 0 {
			t.Fatalf("tx %d commit=%d rollback=%d", i, tx.commits, tx.rollbacks)
		}
	}
}

func TestSnapshotStatusReaderSpanReportsTerminalPhase(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		readErr, commitErr, rollbackErr error
		phase, outcome                  string
	}{
		{"ok", nil, nil, nil, "commit", "ok"},
		{"read", errors.New("read failed"), nil, nil, "read", "error"},
		{"deadline", context.DeadlineExceeded, nil, nil, "read", "deadline"},
		{"rollback", errors.New("read failed"), nil, errors.New("rollback failed"), "rollback", "error"},
		{"commit", nil, errors.New("commit failed"), nil, "commit", "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			tx := &snapshotTxStub{commitErr: tc.commitErr, rollbackErr: tc.rollbackErr}
			reader := NewSnapshotStatusReader(snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return tx, nil }},
				func(db.Queryer) status.Reader {
					return snapshotReaderStub{read: func(context.Context, status.SnapshotSelection) (status.RawSnapshot, error) {
						return status.RawSnapshot{}, tc.readErr
					}}
				},
				provider.Tracer("test"))
			_, _ = reader.ReadStatusSnapshot(t.Context(), time.Now())
			ended := recorder.Ended()
			if len(ended) != 1 || ended[0].Name() != StatusSnapshotSpanName {
				t.Fatalf("spans=%v", ended)
			}
			attrs := map[attribute.Key]string{}
			for _, a := range ended[0].Attributes() {
				attrs[a.Key] = a.Value.AsString()
			}
			if attrs[attribute.Key(statusSnapshotPhaseKey)] != tc.phase || attrs[attribute.Key(statusSnapshotOutcomeKey)] != tc.outcome {
				t.Fatalf("span attributes=%v, want %s/%s", attrs, tc.phase, tc.outcome)
			}
		})
	}
}
