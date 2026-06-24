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

func TestPackageRegistryIdentityLockerLocksUniqueSortedPackageIDsInTransaction(t *testing.T) {
	t.Parallel()

	tx := &recordingPackageRegistryIdentityLockTx{}
	locker := PackageRegistryIdentityLocker{DB: &recordingPackageRegistryIdentityLockDB{tx: tx}}

	var callbackCalled bool
	err := locker.WithPackageRegistryIdentityLocks(
		context.Background(),
		[]string{" package://npm/z ", "", "package://npm/a", "package://npm/a"},
		func(context.Context) error {
			callbackCalled = true
			if tx.committed {
				t.Fatal("transaction committed before callback")
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("WithPackageRegistryIdentityLocks() error = %v, want nil", err)
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
		packageRegistryIdentityAdvisoryLockKey("package://npm/a"),
		packageRegistryIdentityAdvisoryLockKey("package://npm/z"),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lock keys = %v, want %v", got, want)
	}
}

func TestPackageRegistryIdentityLockerRollsBackOnCallbackError(t *testing.T) {
	t.Parallel()

	tx := &recordingPackageRegistryIdentityLockTx{}
	locker := PackageRegistryIdentityLocker{DB: &recordingPackageRegistryIdentityLockDB{tx: tx}}
	wantErr := errors.New("canonical write failed")

	err := locker.WithPackageRegistryIdentityLocks(
		context.Background(),
		[]string{"package://npm/a"},
		func(context.Context) error {
			return wantErr
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithPackageRegistryIdentityLocks() error = %v, want %v", err, wantErr)
	}
	if tx.committed {
		t.Fatal("transaction committed after callback error")
	}
	if !tx.rolledBack {
		t.Fatal("transaction was not rolled back")
	}
}

type recordingPackageRegistryIdentityLockDB struct {
	tx *recordingPackageRegistryIdentityLockTx
}

func (db *recordingPackageRegistryIdentityLockDB) Begin(context.Context) (Transaction, error) {
	return db.tx, nil
}

type recordingPackageRegistryIdentityLockTx struct {
	lockKeys   []int64
	committed  bool
	rolledBack bool
}

func (tx *recordingPackageRegistryIdentityLockTx) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	if query != packageRegistryIdentityAdvisoryLockQuery {
		return nil, errors.New("unexpected query")
	}
	key, ok := args[0].(int64)
	if !ok {
		return nil, errors.New("unexpected key type")
	}
	tx.lockKeys = append(tx.lockKeys, key)
	return fakePackageRegistryIdentityLockResult{}, nil
}

func (tx *recordingPackageRegistryIdentityLockTx) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, errors.New("unexpected query")
}

func (tx *recordingPackageRegistryIdentityLockTx) Commit() error {
	tx.committed = true
	return nil
}

func (tx *recordingPackageRegistryIdentityLockTx) Rollback() error {
	if !tx.committed {
		tx.rolledBack = true
	}
	return nil
}

type fakePackageRegistryIdentityLockResult struct{}

func (fakePackageRegistryIdentityLockResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (fakePackageRegistryIdentityLockResult) RowsAffected() (int64, error) {
	return 0, nil
}
