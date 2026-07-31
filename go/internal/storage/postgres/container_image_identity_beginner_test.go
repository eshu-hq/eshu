// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
)

func TestContainerImageIdentityBeginnerAdaptsPostgresTransaction(t *testing.T) {
	t.Parallel()

	innerTx := &containerImageIdentityAdapterTx{}
	inner := &containerImageIdentityAdapterBeginner{tx: innerTx}
	adapter := ContainerImageIdentityBeginner{Beginner: inner}

	tx, err := adapter.BeginContainerImageIdentityTx(context.Background())
	if err != nil {
		t.Fatalf("BeginContainerImageIdentityTx() error = %v", err)
	}
	if _, err := tx.ExecContext(context.Background(), "SELECT $1::text", "synthetic"); err != nil {
		t.Fatalf("ExecContext() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if got, want := inner.calls, 1; got != want {
		t.Fatalf("inner begin calls = %d, want %d", got, want)
	}
	if !innerTx.committed {
		t.Fatal("inner transaction was not committed")
	}
}

type containerImageIdentityAdapterBeginner struct {
	tx    Transaction
	calls int
}

func (b *containerImageIdentityAdapterBeginner) Begin(context.Context) (Transaction, error) {
	b.calls++
	return b.tx, nil
}

type containerImageIdentityAdapterTx struct {
	committed bool
}

func (*containerImageIdentityAdapterTx) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, nil
}

func (*containerImageIdentityAdapterTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return containerImageIdentityAdapterResult(0), nil
}

func (tx *containerImageIdentityAdapterTx) Commit() error {
	tx.committed = true
	return nil
}

func (*containerImageIdentityAdapterTx) Rollback() error {
	return nil
}

type containerImageIdentityAdapterResult int64

func (r containerImageIdentityAdapterResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (r containerImageIdentityAdapterResult) RowsAffected() (int64, error) {
	return int64(r), nil
}
