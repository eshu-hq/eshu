// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fake_test

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
)

func TestExecQueryerBeginReadOnlyRepeatableReadTracksCallsAndDelegates(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{{Data: [][]any{{"a"}}}},
	}
	var beginner db.ReadOnlyRepeatableReadBeginner = database

	tx, err := beginner.BeginReadOnlyRepeatableRead(context.Background())
	if err != nil {
		t.Fatalf("BeginReadOnlyRepeatableRead() error = %v, want nil", err)
	}
	if got, want := database.BeginReadOnlyRepeatableReadCalls, 1; got != want {
		t.Fatalf("BeginReadOnlyRepeatableReadCalls = %d, want %d", got, want)
	}

	if _, err := tx.QueryContext(context.Background(), "SELECT 1"); err != nil {
		t.Fatalf("tx.QueryContext() error = %v, want nil", err)
	}
	if _, err := tx.ExecContext(context.Background(), "UPDATE t SET x = 1"); err != nil {
		t.Fatalf("tx.ExecContext() error = %v, want nil", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() error = %v, want nil", err)
	}

	if got, want := database.TransactionQueryCalls, 1; got != want {
		t.Fatalf("TransactionQueryCalls = %d, want %d", got, want)
	}
	if got, want := database.TransactionExecCalls, 1; got != want {
		t.Fatalf("TransactionExecCalls = %d, want %d", got, want)
	}
	if got, want := database.TransactionCommitCalls, 1; got != want {
		t.Fatalf("TransactionCommitCalls = %d, want %d", got, want)
	}
	// The queued query response was actually delegated to the parent, not
	// answered independently, so it drains the same FIFO queue.
	if got, want := len(database.Queries), 1; got != want {
		t.Fatalf("len(Queries) = %d, want %d", got, want)
	}
}

func TestExecQueryerBeginReadOnlyRepeatableReadHonorsInjectedError(t *testing.T) {
	t.Parallel()

	boom := errors.New("begin boom")
	database := &fake.ExecQueryer{BeginReadOnlyRepeatableReadErr: boom}
	if _, err := database.BeginReadOnlyRepeatableRead(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("BeginReadOnlyRepeatableRead() error = %v, want %v", err, boom)
	}
}

func TestTransactionRollbackTracksCallsAndHonorsInjectedError(t *testing.T) {
	t.Parallel()

	boom := errors.New("rollback boom")
	database := &fake.ExecQueryer{TransactionRollbackErr: boom}
	tx, err := database.BeginReadOnlyRepeatableRead(context.Background())
	if err != nil {
		t.Fatalf("BeginReadOnlyRepeatableRead() error = %v, want nil", err)
	}
	if err := tx.Rollback(); !errors.Is(err, boom) {
		t.Fatalf("tx.Rollback() error = %v, want %v", err, boom)
	}
	if got, want := database.TransactionRollbackCalls, 1; got != want {
		t.Fatalf("TransactionRollbackCalls = %d, want %d", got, want)
	}
}
