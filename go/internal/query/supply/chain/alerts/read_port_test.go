// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alerts

import (
	"context"
	"errors"
	"testing"

	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingAlertReader struct {
	calls int
	err   error
}

func (r *rejectingAlertReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.calls++
	return nil, r.err
}

func (r *rejectingAlertReader) QueryRowContext(context.Context, string, ...any) db.Row {
	r.calls++
	return rejectingAlertRow{err: r.err}
}

func (*rejectingAlertReader) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, errors.New("snapshot not needed")
}

type rejectingAlertRow struct{ err error }

func (r rejectingAlertRow) Scan(...any) error { return r.err }

func TestAlertListGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingAlertReader{err: want}
	store := NewPostgresStoreWithReadStore(guard)
	_, err := store.ListSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationFilter{
		CVEID: "CVE-2026-1234", Limit: 1,
	})
	if !errors.Is(err, want) || guard.calls != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one query", err, guard.calls)
	}
}

func TestAlertAggregateGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingAlertReader{err: want}
	store := NewPostgresAggregateStoreWithReadStore(guard)
	_, err := store.CountSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationAggregateFilter{CVEID: "CVE-2026-1234"})
	if !errors.Is(err, want) || guard.calls != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one row", err, guard.calls)
	}
}
