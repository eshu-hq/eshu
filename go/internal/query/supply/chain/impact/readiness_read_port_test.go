// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingReadinessSnapshotStore struct {
	begins  int
	queries int
	err     error
}

func (s *rejectingReadinessSnapshotStore) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	s.queries++
	return nil, errors.New("read bypassed snapshot")
}

func (*rejectingReadinessSnapshotStore) QueryRowContext(context.Context, string, ...any) db.Row {
	return nil
}

func (s *rejectingReadinessSnapshotStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	s.begins++
	return nil, s.err
}

func TestReadinessGuardRejectsBeforeBusinessSQL(t *testing.T) {
	for _, query := range []ReadinessQuery{{CVEID: "CVE-2026-1234"}, {RepositoryID: "repo"}} {
		want := errors.New("reader is stale")
		guard := &rejectingReadinessSnapshotStore{err: want}
		store := NewPostgresReadinessStoreWithReadStore(guard)
		_, err := store.ReadSupplyChainImpactReadiness(t.Context(), query)
		if !errors.Is(err, want) || guard.begins != 1 || guard.queries != 0 {
			t.Fatalf("query %+v: error %v, begin %d, business queries %d; want stale reader before SQL", query, err, guard.begins, guard.queries)
		}
	}
}

type failingReadinessTransaction struct {
	queries   int
	rollbacks int
	err       error
}

func (tx *failingReadinessTransaction) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	tx.queries++
	return nil, tx.err
}

func (*failingReadinessTransaction) QueryRowContext(context.Context, string, ...any) db.Row {
	return nil
}

func (*failingReadinessTransaction) Commit() error { return nil }

func (tx *failingReadinessTransaction) Rollback() error {
	tx.rollbacks++
	return nil
}

type startedReadinessSnapshotStore struct {
	rejectingReadinessSnapshotStore
	transaction *failingReadinessTransaction
}

func (s *startedReadinessSnapshotStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	s.begins++
	return s.transaction, nil
}

func TestReadinessQueriesOnlyWithinGuardedSnapshot(t *testing.T) {
	want := errors.New("snapshot query failed")
	transaction := &failingReadinessTransaction{err: want}
	guard := &startedReadinessSnapshotStore{transaction: transaction}
	store := NewPostgresReadinessStoreWithReadStore(guard)
	_, err := store.ReadSupplyChainImpactReadiness(t.Context(), ReadinessQuery{RepositoryID: "repo"})
	if !errors.Is(err, want) || guard.begins != 1 || guard.queries != 0 || transaction.queries != 1 || transaction.rollbacks != 1 {
		t.Fatalf("error %v, begins %d, direct queries %d, transaction queries %d, rollbacks %d; want only one transaction query", err, guard.begins, guard.queries, transaction.queries, transaction.rollbacks)
	}
}
