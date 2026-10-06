// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type fakeResult struct{ affected int64 }

func (r fakeResult) LastInsertId() (int64, error) { return 0, errors.New("unsupported") }
func (r fakeResult) RowsAffected() (int64, error) { return r.affected, nil }

type execCall struct {
	query string
	args  []any
}

type fakeExec struct {
	calls    []execCall
	affected int64
	err      error
}

func (f *fakeExec) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.calls = append(f.calls, execCall{query: query, args: args})
	if f.err != nil {
		return nil, f.err
	}
	return fakeResult{affected: f.affected}, nil
}

type fakeRows struct {
	rows   [][]any
	cursor int
	err    error
}

func (r *fakeRows) Next() bool {
	if r.cursor >= len(r.rows) {
		return false
	}
	r.cursor++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	row := r.rows[r.cursor-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan: %d destinations for %d columns", len(dest), len(row))
	}
	for i, value := range row {
		switch d := dest[i].(type) {
		case *string:
			*d = value.(string)
		case *bool:
			*d = value.(bool)
		case *int:
			*d = value.(int)
		case *float64:
			*d = value.(float64)
		case *time.Time:
			*d = value.(time.Time)
		case *[]byte:
			*d = value.([]byte)
		default:
			return fmt.Errorf("scan: unsupported destination %T", dest[i])
		}
	}
	return nil
}

func (r *fakeRows) Err() error   { return r.err }
func (r *fakeRows) Close() error { return nil }

type fakeQueryer struct {
	calls []execCall
	rows  *fakeRows
	err   error
}

func (f *fakeQueryer) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	f.calls = append(f.calls, execCall{query: query, args: args})
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func validRow() Row {
	return Row{
		ModelKey:      ModelActiveWorkSummary,
		SchemaVersion: SchemaVersion,
		SourceSHA256:  strings.Repeat("ab", 32),
		AsOf:          time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC),
		PassDuration:  412 * time.Millisecond,
		RowCount:      2,
		Entries: []Entry{
			{Section: "queue", Ordinal: 0, JSON: `{"outstanding":1}`},
			{Section: "backlog", Ordinal: 0, JSON: `{"stage":"projector"}`},
		},
	}
}

func TestUpsertReportsWhetherTheGuardAdvanced(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		affected int64
		want     bool
	}{
		{"inserted or advanced", 1, true},
		{"guard rejected an older or equal as_of", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			exec := &fakeExec{affected: tc.affected}
			got, err := Upsert(context.Background(), exec, validRow())
			if err != nil {
				t.Fatalf("Upsert() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("Upsert() = %v, want %v", got, tc.want)
			}
			if len(exec.calls) != 1 {
				t.Fatalf("Upsert() issued %d statements, want 1 (one atomic row write)", len(exec.calls))
			}
		})
	}
}

func TestUpsertBindsEveryColumnInOrder(t *testing.T) {
	t.Parallel()

	row := validRow()
	exec := &fakeExec{affected: 1}
	if _, err := Upsert(context.Background(), exec, row); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	args := exec.calls[0].args
	if len(args) != 7 {
		t.Fatalf("Upsert() bound %d args, want 7", len(args))
	}
	if args[0] != row.ModelKey || args[1] != row.SchemaVersion || args[2] != row.SourceSHA256 {
		t.Fatalf("Upsert() leading args = %v, want key, version, sha", args[:3])
	}
	if got, ok := args[3].(time.Time); !ok || !got.Equal(row.AsOf) {
		t.Fatalf("Upsert() as_of arg = %v, want %v", args[3], row.AsOf)
	}
	if got, ok := args[4].(float64); !ok || got != 412 {
		t.Fatalf("Upsert() pass_duration_ms arg = %v, want 412", args[4])
	}
	if args[5] != row.RowCount {
		t.Fatalf("Upsert() row_count arg = %v, want %d", args[5], row.RowCount)
	}
	want := `[["queue",0,"{\"outstanding\":1}"],["backlog",0,"{\"stage\":\"projector\"}"]]`
	if args[6] != want {
		t.Fatalf("Upsert() rows arg = %v, want %s", args[6], want)
	}
}

func TestUpsertRejectsAnInvalidRowWithoutTouchingTheDatabase(t *testing.T) {
	t.Parallel()

	row := validRow()
	row.RowCount = 5 // does not match the two entries
	exec := &fakeExec{affected: 1}
	_, err := Upsert(context.Background(), exec, row)
	if !errors.Is(err, ErrRowCountMismatch) {
		t.Fatalf("Upsert(mismatched row_count) error = %v, want ErrRowCountMismatch", err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("Upsert(invalid) issued %d statements, want 0", len(exec.calls))
	}
}

func TestUpsertClassifiesUndefinedTable(t *testing.T) {
	t.Parallel()

	missing := fmt.Errorf("exec: %w", &pgconn.PgError{Code: "42P01", Message: `relation "status_summary_snapshots" does not exist`})
	_, err := Upsert(context.Background(), &fakeExec{err: missing}, validRow())
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Upsert(42P01) error = %v, want ErrNotInstalled", err)
	}

	other := &pgconn.PgError{Code: "40001", Message: "serialization failure"}
	_, err = Upsert(context.Background(), &fakeExec{err: other}, validRow())
	if err == nil || errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Upsert(40001) error = %v, want a non-ErrNotInstalled error", err)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
		t.Fatalf("Upsert(40001) error = %v, want the SQLSTATE preserved for the caller", err)
	}
}

func readyRows(row Row) *fakeRows {
	payload, err := EncodeEntries(row.Entries)
	if err != nil {
		panic(err)
	}
	return &fakeRows{rows: [][]any{{
		row.ModelKey, row.SchemaVersion, row.SourceSHA256, row.AsOf,
		row.AsOf.Add(time.Second), float64(row.PassDuration) / float64(time.Millisecond),
		row.RowCount, payload,
	}}}
}

func TestReadReturnsTheStoredRow(t *testing.T) {
	t.Parallel()

	want := validRow()
	q := &fakeQueryer{rows: readyRows(want)}
	got, err := Read(context.Background(), q, ModelActiveWorkSummary)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got.ModelKey != want.ModelKey || got.SchemaVersion != want.SchemaVersion ||
		got.SourceSHA256 != want.SourceSHA256 || !got.AsOf.Equal(want.AsOf) ||
		got.RowCount != want.RowCount || got.PassDuration != want.PassDuration {
		t.Fatalf("Read() = %+v, want %+v", got, want)
	}
	if !got.ComputedAt.Equal(want.AsOf.Add(time.Second)) {
		t.Fatalf("Read() ComputedAt = %v, want %v", got.ComputedAt, want.AsOf.Add(time.Second))
	}
	if len(got.Entries) != 2 || got.Entries[1].Section != "backlog" {
		t.Fatalf("Read() entries = %#v, want the stored entries in order", got.Entries)
	}
	if len(q.calls) != 1 || len(q.calls[0].args) != 1 || q.calls[0].args[0] != ModelActiveWorkSummary {
		t.Fatalf("Read() calls = %#v, want exactly one keyed lookup", q.calls)
	}
}

func TestReadEmptyTableIsNotFound(t *testing.T) {
	t.Parallel()

	_, err := Read(context.Background(), &fakeQueryer{rows: &fakeRows{}}, ModelActiveWorkSummary)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Read(empty) error = %v, want ErrNotFound", err)
	}
}

func TestReadClassifiesUndefinedTable(t *testing.T) {
	t.Parallel()

	missing := &pgconn.PgError{Code: "42P01", Message: "relation does not exist"}
	_, err := Read(context.Background(), &fakeQueryer{err: missing}, ModelActiveWorkSummary)
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Read(42P01 on query) error = %v, want ErrNotInstalled", err)
	}

	// pgx can also surface the error from rows.Err() after QueryContext succeeds.
	_, err = Read(context.Background(), &fakeQueryer{rows: &fakeRows{err: missing}}, ModelActiveWorkSummary)
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Read(42P01 on rows) error = %v, want ErrNotInstalled", err)
	}
}

func TestReadSurfacesRowCountMismatchAndDecodeFailure(t *testing.T) {
	t.Parallel()

	mismatch := validRow()
	mismatch.RowCount = 7
	rows := readyRows(validRow())
	rows.rows[0][6] = 7
	_, err := Read(context.Background(), &fakeQueryer{rows: rows}, ModelActiveWorkSummary)
	if !errors.Is(err, ErrRowCountMismatch) {
		t.Fatalf("Read(row_count 7 vs 2 entries) error = %v, want ErrRowCountMismatch", err)
	}

	rows = readyRows(validRow())
	rows.rows[0][7] = []byte(`{"not":"tuples"}`)
	_, err = Read(context.Background(), &fakeQueryer{rows: rows}, ModelActiveWorkSummary)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("Read(bad payload) error = %v, want ErrDecode", err)
	}
}

func TestReadRejectsBlankModelKeyWithoutQuerying(t *testing.T) {
	t.Parallel()

	q := &fakeQueryer{rows: &fakeRows{}}
	if _, err := Read(context.Background(), q, " "); err == nil {
		t.Fatal("Read(blank key) error = nil, want an error")
	}
	if len(q.calls) != 0 {
		t.Fatalf("Read(blank key) issued %d statements, want 0", len(q.calls))
	}
}

func TestStoreDelegatesToUpsertAndRead(t *testing.T) {
	t.Parallel()

	exec := &fakeExec{affected: 1}
	q := &fakeQueryer{rows: readyRows(validRow())}
	store := NewStore(struct {
		db.Executor
		db.Queryer
	}{exec, q})
	if advanced, err := store.Upsert(context.Background(), validRow()); err != nil || !advanced {
		t.Fatalf("Store.Upsert() = %v, %v, want true, nil", advanced, err)
	}
	if _, err := store.Read(context.Background(), ModelActiveWorkSummary); err != nil {
		t.Fatalf("Store.Read() error = %v", err)
	}
	if len(exec.calls) != 1 || len(q.calls) != 1 {
		t.Fatalf("Store issued %d exec and %d query statements, want 1 and 1", len(exec.calls), len(q.calls))
	}
}
