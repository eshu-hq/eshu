// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingCICDReader struct {
	calls int
	err   error
}

func (r *rejectingCICDReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.calls++
	return nil, r.err
}

func (r *rejectingCICDReader) QueryRowContext(context.Context, string, ...any) db.Row {
	r.calls++
	return rejectingCICDRow{err: r.err}
}

func (*rejectingCICDReader) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, errors.New("snapshot not needed")
}

type rejectingCICDRow struct{ err error }

func (r rejectingCICDRow) Scan(...any) error { return r.err }

func TestCICDListGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingCICDReader{err: want}
	store := NewPostgresRunCorrelationStoreWithReadStore(guard)
	_, err := store.ListCICDRunCorrelations(t.Context(), querycontract.CICDRunCorrelationFilter{ScopeID: "scope", Limit: 1})
	if !errors.Is(err, want) || guard.calls != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one query", err, guard.calls)
	}
}

func TestCICDAggregateGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingCICDReader{err: want}
	store := NewPostgresRunCorrelationAggregateStoreWithReadStore(guard)
	_, err := store.CountRunCorrelations(t.Context(), RunCorrelationAggregateFilter{ScopeID: "scope"})
	if !errors.Is(err, want) || guard.calls != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one row", err, guard.calls)
	}
}
