// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingImpactReader struct {
	queries int
	err     error
}

func (r *rejectingImpactReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.queries++
	return nil, r.err
}

func (r *rejectingImpactReader) QueryRowContext(context.Context, string, ...any) db.Row {
	r.queries++
	return rejectingImpactRow{err: r.err}
}

type rejectingImpactRow struct{ err error }

func (r rejectingImpactRow) Scan(...any) error { return r.err }

func (*rejectingImpactReader) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, errors.New("snapshot not needed")
}

func TestImpactFindingsGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingImpactReader{err: want}
	store := NewPostgresFindingStoreWithReadStore(guard, true)
	_, err := store.ListSupplyChainImpactFindings(t.Context(), FindingFilter{CVEID: "CVE-2026-1234", Limit: 1})
	if !errors.Is(err, want) || guard.queries != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one query", err, guard.queries)
	}
}

func TestImpactAggregatesGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &rejectingImpactReader{err: want}
	store := NewPostgresAggregateStoreWithReadStore(guard)
	_, err := store.CountSupplyChainImpactFindings(t.Context(), AggregateFilter{CVEID: "CVE-2026-1234"})
	if !errors.Is(err, want) || guard.queries != 1 {
		t.Fatalf("error %v, guarded queries %d; want stale reader and one row", err, guard.queries)
	}
}
