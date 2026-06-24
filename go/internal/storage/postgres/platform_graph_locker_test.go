// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
)

func TestPlatformGraphLockerLocksUniqueSortedPlatformIDsInTransaction(t *testing.T) {
	t.Parallel()

	tx := &recordingPlatformGraphLockTx{}
	locker := PlatformGraphLocker{DB: &recordingPlatformGraphLockDB{tx: tx}}

	var callbackCalled bool
	err := locker.WithPlatformLocks(
		context.Background(),
		[]string{" platform:z ", "", "platform:a", "platform:a"},
		func(context.Context) error {
			callbackCalled = true
			if got, want := tx.committed, false; got != want {
				t.Fatalf("committed before callback = %v, want %v", got, want)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("WithPlatformLocks() error = %v, want nil", err)
	}
	if !callbackCalled {
		t.Fatal("callback was not called")
	}
	if !tx.committed {
		t.Fatal("transaction was not committed")
	}
	if tx.rolledBack {
		t.Fatal("transaction rolled back after successful callback")
	}
	if got, want := tx.lockKeys, []int64{
		platformGraphAdvisoryLockKey("platform:a"),
		platformGraphAdvisoryLockKey("platform:z"),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lock keys = %v, want %v", got, want)
	}
}

func TestPlatformGraphLockerRollsBackOnCallbackError(t *testing.T) {
	t.Parallel()

	tx := &recordingPlatformGraphLockTx{}
	locker := PlatformGraphLocker{DB: &recordingPlatformGraphLockDB{tx: tx}}
	wantErr := errors.New("graph write failed")

	err := locker.WithPlatformLocks(
		context.Background(),
		[]string{"platform:a"},
		func(context.Context) error {
			return wantErr
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithPlatformLocks() error = %v, want %v", err, wantErr)
	}
	if tx.committed {
		t.Fatal("transaction committed after callback error")
	}
	if !tx.rolledBack {
		t.Fatal("transaction was not rolled back")
	}
}

type recordingPlatformGraphLockDB struct {
	tx *recordingPlatformGraphLockTx
}

func (db *recordingPlatformGraphLockDB) Begin(context.Context) (Transaction, error) {
	return db.tx, nil
}

type recordingPlatformGraphLockTx struct {
	lockKeys   []int64
	committed  bool
	rolledBack bool
}

func (tx *recordingPlatformGraphLockTx) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	if query != platformGraphAdvisoryLockQuery {
		return nil, errors.New("unexpected query")
	}
	key, ok := args[0].(int64)
	if !ok {
		return nil, errors.New("unexpected key type")
	}
	tx.lockKeys = append(tx.lockKeys, key)
	return fakePlatformGraphLockResult{}, nil
}

func (tx *recordingPlatformGraphLockTx) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, errors.New("unexpected query")
}

func (tx *recordingPlatformGraphLockTx) Commit() error {
	tx.committed = true
	return nil
}

func (tx *recordingPlatformGraphLockTx) Rollback() error {
	if !tx.committed {
		tx.rolledBack = true
	}
	return nil
}

type fakePlatformGraphLockResult struct{}

func (fakePlatformGraphLockResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (fakePlatformGraphLockResult) RowsAffected() (int64, error) {
	return 0, nil
}
