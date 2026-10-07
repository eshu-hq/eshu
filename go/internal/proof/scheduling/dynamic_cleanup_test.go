// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeDynamicTransaction struct {
	deadline time.Time
	rolled   bool
	err      error
}

type fakeSnapshotRow struct {
	id  string
	err error
}

func (row fakeSnapshotRow) Scan(dst ...any) error {
	if row.err != nil {
		return row.err
	}
	*dst[0].(*string) = row.id
	return nil
}

type fakeSnapshotTransaction struct {
	pgx.Tx
	queryCount int
	commands   []string
	execErr    error
}

func (tx *fakeSnapshotTransaction) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.queryCount++
	if sql != "SELECT pg_export_snapshot()" {
		return fakeSnapshotRow{err: errors.New("unexpected query")}
	}
	return fakeSnapshotRow{id: "00000004-00000A1B-1"}
}

func (tx *fakeSnapshotTransaction) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.commands = append(tx.commands, sql)
	return pgconn.CommandTag{}, tx.execErr
}

func TestDynamicSnapshotSharedAcrossFourReaders(t *testing.T) {
	readers := []*fakeSnapshotTransaction{{}, {}, {}, {}}
	txs := make([]pgx.Tx, len(readers))
	for index, reader := range readers {
		txs[index] = reader
	}
	if err := shareDynamicSnapshot(context.Background(), txs); err != nil {
		t.Fatal(err)
	}
	if readers[0].queryCount != 1 || len(readers[0].commands) != 2 {
		t.Fatalf("exporter query=%d commands=%v", readers[0].queryCount, readers[0].commands)
	}
	for index, reader := range readers[1:] {
		if len(reader.commands) != 3 || reader.commands[0] != "SET TRANSACTION SNAPSHOT '00000004-00000A1B-1'" {
			t.Fatalf("reader %d snapshot/limits = %v", index+1, reader.commands)
		}
		if reader.commands[1] != "SET LOCAL statement_timeout = '5s'" || reader.commands[2] != "SET LOCAL lock_timeout = '1s'" {
			t.Fatalf("reader %d limits = %v", index+1, reader.commands)
		}
	}
}

func TestDynamicSnapshotStopsAtFirstErrorAndCancellation(t *testing.T) {
	readers := []*fakeSnapshotTransaction{{}, {execErr: errors.New("import failed")}, {}, {}}
	txs := make([]pgx.Tx, len(readers))
	for index, reader := range readers {
		txs[index] = reader
	}
	if err := shareDynamicSnapshot(context.Background(), txs); err == nil || !strings.Contains(err.Error(), "import") {
		t.Fatalf("first import error = %v", err)
	}
	if readers[2].queryCount != 0 || len(readers[2].commands) != 0 {
		t.Fatal("continued after first failed import")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := shareDynamicSnapshot(canceled, txs); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestDynamicCaseRequiresExactlyFourConnections(t *testing.T) {
	if err := runDynamicCase(context.Background(), nil, dynamicWorkload{}, true); err == nil {
		t.Fatal("missing readers accepted")
	}
	if err := runDynamicCase(context.Background(), make([]*pgx.Conn, 5), dynamicWorkload{}, true); err == nil {
		t.Fatal("fifth connection accepted")
	}
}

func (tx *fakeDynamicTransaction) Rollback(ctx context.Context) error {
	tx.rolled = true
	tx.deadline, _ = ctx.Deadline()
	return tx.err
}

func TestRollbackDynamicTransactionsJoinsFailures(t *testing.T) {
	firstErr := errors.New("first rollback failed")
	secondErr := errors.New("second rollback failed")
	first := &fakeDynamicTransaction{err: firstErr}
	second := &fakeDynamicTransaction{err: secondErr}
	got := rollbackDynamicTransactions([]*fakeDynamicTransaction{first, second})
	if !errors.Is(got, firstErr) || !errors.Is(got, secondErr) {
		t.Fatalf("rollback failures lost: %v", got)
	}
	for _, tx := range []*fakeDynamicTransaction{first, second} {
		if !tx.rolled {
			t.Fatal("rollback skipped")
		}
		remaining := time.Until(tx.deadline)
		if remaining < 9*time.Second || remaining > 10*time.Second {
			t.Fatalf("rollback deadline remaining = %s", remaining)
		}
	}
}
