// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// errQueryer fails every query with err.
type errQueryer struct{ err error }

func (q errQueryer) QueryContext(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	return nil, q.err
}

// errExecQueryer fails every query and exec with err.
type errExecQueryer struct{ err error }

func (q errExecQueryer) QueryContext(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	return nil, q.err
}

func (q errExecQueryer) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, q.err
}

// boolRows yields one boolean row.
type boolRows struct {
	value bool
	done  bool
}

func (r *boolRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}

func (r *boolRows) Scan(dest ...any) error {
	ptr, ok := dest[0].(*bool)
	if !ok {
		return errors.New("boolRows scans only into *bool")
	}
	*ptr = r.value
	return nil
}

func (r *boolRows) Err() error { return nil }

func (r *boolRows) Close() error { return nil }

// valueQueryer answers every query with one boolean row.
type valueQueryer struct{ value bool }

func (q valueQueryer) QueryContext(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	return &boolRows{value: q.value}, nil
}

// TestBackfillMarkerName pins the completion marker row: renaming it orphans
// installs whose backfill already recorded the old name, so a new row shape
// needs a new marker added beside this test, never a rename.
func TestBackfillMarkerName(t *testing.T) {
	t.Parallel()

	if BackfillMarker == "" {
		t.Fatal("BackfillMarker is blank")
	}
	if BackfillMarker != "config_artifact_paths_v1" {
		t.Fatalf("BackfillMarker = %q, want config_artifact_paths_v1", BackfillMarker)
	}
}

// TestBackfillCompleteReadsTheMarker pins the marker check: true reads true,
// false reads false, a query failure is an error, and a missing marker table
// (SQLSTATE 42P01, migrations not applied yet) is "not complete" with no
// error so readers stay on the live path.
func TestBackfillCompleteReadsTheMarker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	complete, err := BackfillComplete(ctx, valueQueryer{value: true})
	if err != nil || !complete {
		t.Fatalf("BackfillComplete(true row) = (%v, %v), want (true, nil)", complete, err)
	}
	complete, err = BackfillComplete(ctx, valueQueryer{value: false})
	if err != nil || complete {
		t.Fatalf("BackfillComplete(false row) = (%v, %v), want (false, nil)", complete, err)
	}
	boom := errors.New("connection refused")
	if _, err := BackfillComplete(ctx, errQueryer{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("BackfillComplete(query error) = %v, want the query error", err)
	}
	complete, err = BackfillComplete(ctx, errQueryer{err: &pgconn.PgError{Code: "42P01"}})
	if err != nil || complete {
		t.Fatalf("BackfillComplete(missing table) = (%v, %v), want (false, nil)", complete, err)
	}
}

// TestRecordBackfillMarkerReportsExecFailure pins the failure path: a refused
// marker write is an error, so the walker never believes a backfill completed
// that did not record.
func TestRecordBackfillMarkerReportsExecFailure(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection refused")
	if err := RecordBackfillMarker(context.Background(), errExecQueryer{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("RecordBackfillMarker(exec error) = %v, want the exec error", err)
	}
}
