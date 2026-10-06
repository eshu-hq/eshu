// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"go.opentelemetry.io/otel/trace/noop"
)

type snapshotBeginStub struct {
	db.ReadStore
	begin func(context.Context) (db.ReadTransaction, error)
}

func (s snapshotBeginStub) BeginReadOnlySnapshot(ctx context.Context) (db.ReadTransaction, error) {
	return s.begin(ctx)
}

type snapshotTxStub struct {
	db.ReadTransaction
	commits, rollbacks     int
	commitErr, rollbackErr error
	// statements records each statement sent through QueryContext, which is
	// how the status reader applies SET LOCAL jit = off to a non-guarded
	// transaction; queryErr fails every such statement.
	statements []string
	queryErr   error
}

func (s *snapshotTxStub) QueryContext(_ context.Context, statement string, _ ...any) (db.Rows, error) {
	s.statements = append(s.statements, statement)
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	return snapshotEmptyRows{}, nil
}

type snapshotEmptyRows struct{}

func (snapshotEmptyRows) Next() bool        { return false }
func (snapshotEmptyRows) Scan(...any) error { return errors.New("no rows") }
func (snapshotEmptyRows) Err() error        { return nil }
func (snapshotEmptyRows) Close() error      { return nil }

func (s *snapshotTxStub) Commit() error   { s.commits++; return s.commitErr }
func (s *snapshotTxStub) Rollback() error { s.rollbacks++; return s.rollbackErr }

type snapshotReaderStub struct {
	read  func(context.Context, status.SnapshotSelection) (status.RawSnapshot, error)
	ready func(context.Context) error
}

func (s snapshotReaderStub) ReadStatusSnapshot(ctx context.Context, _ time.Time) (status.RawSnapshot, error) {
	return s.read(ctx, status.FullSnapshotSelection())
}

func (s snapshotReaderStub) ReadStatusSnapshotFiltered(ctx context.Context, _ time.Time, selection status.SnapshotSelection) (status.RawSnapshot, error) {
	return s.read(ctx, selection)
}

func (s snapshotReaderStub) CheckStatusReadiness(ctx context.Context) error {
	if s.ready == nil {
		return nil
	}
	return s.ready(ctx)
}

func TestSnapshotStatusReaderCommitsBeforeReturningFullAndFiltered(t *testing.T) {
	for _, selection := range []status.SnapshotSelection{status.FullSnapshotSelection(), {}, status.SemanticOnlySnapshotSelection(), {SkipTerraformStateEvidence: true}} {
		tx := &snapshotTxStub{}
		begins, factories := 0, 0
		store := snapshotBeginStub{begin: func(ctx context.Context) (db.ReadTransaction, error) {
			begins++
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 5*time.Second || time.Until(deadline) < 4*time.Second {
				t.Fatalf("status deadline = %v", deadline)
			}
			return tx, nil
		}}
		asOf := time.Now()
		reader := NewSnapshotStatusReader(store, func(q db.Queryer) status.Reader {
			factories++
			if q != tx {
				t.Fatal("status factory did not receive the snapshot transaction")
			}
			return snapshotReaderStub{read: func(_ context.Context, got status.SnapshotSelection) (status.RawSnapshot, error) {
				if got != selection {
					t.Fatalf("selection = %+v, want %+v", got, selection)
				}
				if tx.commits != 0 {
					t.Fatal("committed before status read")
				}
				return status.RawSnapshot{AsOf: asOf}, nil
			}}
		}, noop.NewTracerProvider().Tracer("test"))
		var raw status.RawSnapshot
		var err error
		if selection == status.FullSnapshotSelection() {
			raw, err = reader.ReadStatusSnapshot(context.Background(), asOf)
		} else {
			raw, err = reader.ReadStatusSnapshotFiltered(context.Background(), asOf, selection)
		}
		if err != nil || !raw.AsOf.Equal(asOf) || begins != 1 || factories != 1 || tx.commits != 1 || tx.rollbacks != 0 {
			t.Fatalf("raw=%v err=%v begin=%d factory=%d commit=%d rollback=%d", raw.AsOf, err, begins, factories, tx.commits, tx.rollbacks)
		}
	}
}

func TestSnapshotStatusReaderPreservesReadAndTerminalFaults(t *testing.T) {
	readErr, rollbackErr, commitErr := errors.New("read"), errors.New("rollback"), errors.New("commit")
	for _, tc := range []struct {
		name                            string
		selection                       status.SnapshotSelection
		readErr, commitErr, rollbackErr error
		commits, rollbacks              int
	}{
		{"read", status.FullSnapshotSelection(), readErr, nil, rollbackErr, 0, 1},
		{"commit", status.FullSnapshotSelection(), nil, commitErr, nil, 1, 0},
		{"read_repository_detail", status.SnapshotSelection{SkipTerraformStateEvidence: true}, readErr, nil, rollbackErr, 0, 1},
		{"commit_repository_detail", status.SnapshotSelection{SkipTerraformStateEvidence: true}, nil, commitErr, nil, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &snapshotTxStub{commitErr: tc.commitErr, rollbackErr: tc.rollbackErr}
			reader := NewSnapshotStatusReader(snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return tx, nil }},
				func(db.Queryer) status.Reader {
					return snapshotReaderStub{read: func(context.Context, status.SnapshotSelection) (status.RawSnapshot, error) {
						return status.RawSnapshot{AsOf: time.Now()}, tc.readErr
					}}
				},
				noop.NewTracerProvider().Tracer("test"))
			raw, err := reader.ReadStatusSnapshotFiltered(context.Background(), time.Now(), tc.selection)
			if !raw.AsOf.IsZero() || !errors.Is(err, tc.readErr) && tc.readErr != nil || !errors.Is(err, tc.commitErr) && tc.commitErr != nil || !errors.Is(err, tc.rollbackErr) && tc.rollbackErr != nil || tx.commits != tc.commits || tx.rollbacks != tc.rollbacks {
				t.Fatalf("raw=%v err=%v commit=%d rollback=%d", raw.AsOf, err, tx.commits, tx.rollbacks)
			}
		})
	}
}

func TestSnapshotStatusReaderCancelCannotReturnSuccess(t *testing.T) {
	for _, tc := range []struct {
		name      string
		selection status.SnapshotSelection
	}{
		{"full", status.FullSnapshotSelection()},
		{"filtered", status.SnapshotSelection{}},
		{"semantic", status.SemanticOnlySnapshotSelection()},
		{"repository_detail", status.SnapshotSelection{SkipTerraformStateEvidence: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &snapshotTxStub{}
			parent, cancel := context.WithCancel(context.Background())
			reader := NewSnapshotStatusReader(snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return tx, nil }},
				func(db.Queryer) status.Reader {
					return snapshotReaderStub{read: func(context.Context, status.SnapshotSelection) (status.RawSnapshot, error) {
						cancel()
						return status.RawSnapshot{AsOf: time.Now()}, nil
					}}
				},
				noop.NewTracerProvider().Tracer("test"))
			raw, err := reader.ReadStatusSnapshotFiltered(parent, time.Now(), tc.selection)
			if !errors.Is(err, context.Canceled) || !raw.AsOf.IsZero() || tx.commits != 0 || tx.rollbacks != 1 {
				t.Fatalf("raw=%v err=%v commit=%d rollback=%d", raw.AsOf, err, tx.commits, tx.rollbacks)
			}
		})
	}
}

type cancelOnCommitSnapshotTx struct {
	*snapshotTxStub
	cancel context.CancelFunc
}

func (s cancelOnCommitSnapshotTx) Commit() error {
	err := s.snapshotTxStub.Commit()
	s.cancel()
	return err
}

func TestSnapshotStatusReaderCancelAfterCommitCannotReturnSuccess(t *testing.T) {
	for _, tc := range []struct {
		name      string
		selection status.SnapshotSelection
	}{
		{"full", status.FullSnapshotSelection()},
		{"filtered", status.SnapshotSelection{}},
		{"semantic", status.SemanticOnlySnapshotSelection()},
		{"repository_detail", status.SnapshotSelection{SkipTerraformStateEvidence: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			tx := cancelOnCommitSnapshotTx{snapshotTxStub: &snapshotTxStub{}, cancel: cancel}
			reader := NewSnapshotStatusReader(snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return tx, nil }},
				func(db.Queryer) status.Reader {
					return snapshotReaderStub{read: func(context.Context, status.SnapshotSelection) (status.RawSnapshot, error) {
						return status.RawSnapshot{AsOf: time.Now()}, nil
					}}
				}, nil)
			raw, err := reader.ReadStatusSnapshotFiltered(parent, time.Now(), tc.selection)
			if !errors.Is(err, context.Canceled) || !raw.AsOf.IsZero() || tx.commits != 1 || tx.rollbacks != 0 {
				t.Fatalf("raw=%v err=%v commit=%d rollback=%d", raw.AsOf, err, tx.commits, tx.rollbacks)
			}
		})
	}
}
