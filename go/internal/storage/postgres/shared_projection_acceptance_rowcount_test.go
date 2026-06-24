// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestAcceptanceRowCountReturnsEstimate(t *testing.T) {
	t.Parallel()
	q := &fakeRowCountQueryer{rows: &fakeCountRows{value: 42}}
	store := NewSharedProjectionAcceptanceStore(q)

	got, err := store.AcceptanceRowCount(context.Background())
	if err != nil {
		t.Fatalf("AcceptanceRowCount() error = %v", err)
	}
	if got != 42 {
		t.Fatalf("AcceptanceRowCount() = %d, want 42", got)
	}
	for _, want := range []string{"pg_class", "reltuples", "shared_projection_acceptance"} {
		if !strings.Contains(q.query, want) {
			t.Fatalf("row-estimate query missing %q:\n%s", want, q.query)
		}
	}
}

func TestAcceptanceRowCountClampsNegativeEstimate(t *testing.T) {
	t.Parallel()
	// A never-analyzed table reports reltuples = -1 in modern PostgreSQL; the
	// gauge must never publish a negative row count.
	store := NewSharedProjectionAcceptanceStore(&fakeRowCountQueryer{rows: &fakeCountRows{value: -1}})
	got, err := store.AcceptanceRowCount(context.Background())
	if err != nil {
		t.Fatalf("AcceptanceRowCount() error = %v", err)
	}
	if got != 0 {
		t.Fatalf("AcceptanceRowCount() = %d, want 0 for a never-analyzed table", got)
	}
}

func TestAcceptanceRowCountNoRowsIsZero(t *testing.T) {
	t.Parallel()
	store := NewSharedProjectionAcceptanceStore(&fakeRowCountQueryer{rows: &fakeCountRows{empty: true}})
	got, err := store.AcceptanceRowCount(context.Background())
	if err != nil {
		t.Fatalf("AcceptanceRowCount() error = %v", err)
	}
	if got != 0 {
		t.Fatalf("AcceptanceRowCount() = %d, want 0 when the catalog row is absent", got)
	}
}

func TestAcceptanceRowCountPropagatesQueryError(t *testing.T) {
	t.Parallel()
	store := NewSharedProjectionAcceptanceStore(&fakeRowCountQueryer{err: errors.New("boom")})
	if _, err := store.AcceptanceRowCount(context.Background()); err == nil {
		t.Fatal("AcceptanceRowCount() error = nil, want query error")
	}
}

type fakeRowCountQueryer struct {
	query string
	rows  *fakeCountRows
	err   error
}

func (f *fakeRowCountQueryer) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	f.query = query
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (f *fakeRowCountQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

type fakeCountRows struct {
	value   int64
	empty   bool
	done    bool
	scanErr error
}

func (r *fakeCountRows) Next() bool {
	if r.empty || r.done {
		return false
	}
	r.done = true
	return true
}

func (r *fakeCountRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if len(dest) > 0 {
		if p, ok := dest[0].(*int64); ok {
			*p = r.value
		}
	}
	return nil
}

func (r *fakeCountRows) Err() error   { return nil }
func (r *fakeCountRows) Close() error { return nil }
