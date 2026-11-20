// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// recordedSnapshotFakeDB is the minimal db.ExecQueryer the recorded
// snapshot point select needs: one staged result, no transaction.
type recordedSnapshotFakeDB struct {
	rows db.Rows
	err  error
}

func (f *recordedSnapshotFakeDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (f *recordedSnapshotFakeDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected exec")
}

// recordedSnapshotFakeRows serves one staged row through the real
// array.StringArray scanner, so the test pins the production parse path.
type recordedSnapshotFakeRows struct {
	values [][]any
	index  int
}

func (r *recordedSnapshotFakeRows) Next() bool { return r.index < len(r.values) }

func (r *recordedSnapshotFakeRows) Scan(dest ...any) error {
	if r.index >= len(r.values) {
		return errors.New("scan called without row")
	}
	row := r.values[r.index]
	r.index++
	if len(dest) != len(row) {
		return errors.New("destination count mismatch")
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *array.StringArray:
			if err := target.Scan(row[i]); err != nil {
				return err
			}
		case *time.Time:
			value, ok := row[i].(time.Time)
			if !ok {
				return errors.New("want time.Time")
			}
			*target = value
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

func (r *recordedSnapshotFakeRows) Err() error   { return nil }
func (r *recordedSnapshotFakeRows) Close() error { return nil }

// TestLoadAWSDriftRecordedSnapshotReturnsRecordedMap pins the per-run
// plan's "previous" half: the recorded parallel arrays zip back into the
// ARN-to-payload-identity map plus the record time (the audit clock).
func TestLoadAWSDriftRecordedSnapshotReturnsRecordedMap(t *testing.T) {
	t.Parallel()
	recordedAt := time.Now().UTC().Truncate(time.Second)
	store := NewIngestionStore(&recordedSnapshotFakeDB{
		rows: &recordedSnapshotFakeRows{values: [][]any{
			{`{"arn:1","arn:2"}`, `{"h1","h2"}`, recordedAt},
		}},
	})
	prev, got, ok, err := store.LoadAWSDriftRecordedSnapshot(context.Background(), "work-1")
	if err != nil {
		t.Fatalf("LoadAWSDriftRecordedSnapshot() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("ok = false, want true for a recorded row")
	}
	if len(prev) != 2 || prev["arn:1"] != "h1" || prev["arn:2"] != "h2" {
		t.Fatalf("prev = %v, want map[arn:1:h1 arn:2:h2]", prev)
	}
	if !got.Equal(recordedAt) {
		t.Fatalf("recordedAt = %v, want %v", got, recordedAt)
	}
}

// TestLoadAWSDriftRecordedSnapshotMissingRowReopens pins absence:
// no recorded row reports ok=false without an error, so the planner takes
// the missing-snapshot full run, never a skip.
func TestLoadAWSDriftRecordedSnapshotMissingRowReopens(t *testing.T) {
	t.Parallel()
	store := NewIngestionStore(&recordedSnapshotFakeDB{
		rows: &recordedSnapshotFakeRows{},
	})
	prev, _, ok, err := store.LoadAWSDriftRecordedSnapshot(context.Background(), "work-new")
	if err != nil {
		t.Fatalf("LoadAWSDriftRecordedSnapshot() error = %v, want nil", err)
	}
	if ok {
		t.Fatal("ok = true, want false for a missing row")
	}
	if len(prev) != 0 {
		t.Fatalf("prev = %v, want empty on missing row", prev)
	}
}

// TestLoadAWSDriftRecordedSnapshotLengthMismatchFails pins corrupt-row
// handling: parallel arrays of different lengths error (the planner fails
// open) instead of silently truncating the diff.
func TestLoadAWSDriftRecordedSnapshotLengthMismatchFails(t *testing.T) {
	t.Parallel()
	store := NewIngestionStore(&recordedSnapshotFakeDB{
		rows: &recordedSnapshotFakeRows{values: [][]any{
			{`{"arn:1","arn:2"}`, `{"h1"}`, time.Now().UTC()},
		}},
	})
	if _, _, _, err := store.LoadAWSDriftRecordedSnapshot(context.Background(), "work-1"); err == nil {
		t.Fatal("LoadAWSDriftRecordedSnapshot() error = nil, want length-mismatch error")
	}
}

// TestLoadAWSDriftRecordedSnapshotQueryErrorFails pins fail-open: an
// unreadable record errors (the planner plans a forced full run).
func TestLoadAWSDriftRecordedSnapshotQueryErrorFails(t *testing.T) {
	t.Parallel()
	store := NewIngestionStore(&recordedSnapshotFakeDB{err: errors.New("connection refused")})
	if _, _, _, err := store.LoadAWSDriftRecordedSnapshot(context.Background(), "work-1"); err == nil {
		t.Fatal("LoadAWSDriftRecordedSnapshot() error = nil, want query error")
	}
}
