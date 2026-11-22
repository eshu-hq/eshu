// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Recorder is a db.ReadStore that captures every statement a store issues and
// answers each with an empty result, so a probe can drive a production store
// through a branch and read back the exact SQL and bind arguments it chose,
// with no database. The captured text comes from the same package variables
// and builders the store uses in production, which is why a recorder sees what
// a source scan of the query text cannot: strings assembled at init, and fact
// kinds that arrive only as bind arguments.
//
// Rows, when set, supplies rows for a statement instead of an empty cursor, so
// a probe can walk a multi-step handler past its first read.
type Recorder struct {
	// Rows optionally returns the cursor for one statement.
	Rows func(sql string) db.Rows

	mu         sync.Mutex
	statements []Statement
}

var (
	_ db.ReadStore       = (*Recorder)(nil)
	_ db.ReadTransaction = (*snapshot)(nil)
)

// Statements returns a copy of the statements captured so far, in order.
func (r *Recorder) Statements() []Statement {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Statement(nil), r.statements...)
}

// Reset drops the captured statements.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statements = nil
}

func (r *Recorder) record(query string, args []any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statements = append(r.statements, Statement{SQL: query, Args: append([]any(nil), args...)})
}

// QueryContext records the statement and returns an empty cursor.
func (r *Recorder) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	r.record(query, args)
	if r.Rows != nil {
		if rows := r.Rows(query); rows != nil {
			return rows, nil
		}
	}
	return emptyRows{}, nil
}

// QueryRowContext records the statement and returns a row with no data.
func (r *Recorder) QueryRowContext(_ context.Context, query string, args ...any) db.Row {
	r.record(query, args)
	return emptyRow{}
}

// BeginReadOnlySnapshot returns a transaction whose reads record through r.
func (r *Recorder) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return &snapshot{recorder: r}, nil
}

type snapshot struct{ recorder *Recorder }

func (s *snapshot) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return s.recorder.QueryContext(ctx, query, args...)
}

func (s *snapshot) QueryRowContext(ctx context.Context, query string, args ...any) db.Row {
	return s.recorder.QueryRowContext(ctx, query, args...)
}

func (*snapshot) Commit() error   { return nil }
func (*snapshot) Rollback() error { return nil }

type emptyRows struct{}

func (emptyRows) Next() bool        { return false }
func (emptyRows) Scan(...any) error { return errors.New("audit: empty cursor") }
func (emptyRows) Err() error        { return nil }
func (emptyRows) Close() error      { return nil }

type emptyRow struct{}

func (emptyRow) Scan(...any) error { return sql.ErrNoRows }

// StaticRows returns a cursor over fixed rows, so a probe can answer a
// watermark or existence read and let a multi-step store reach its next
// statement. Scan assigns each value to the matching destination pointer, which
// must have the value's exact type.
func StaticRows(rows ...[]any) db.Rows { return &staticRows{rows: rows, index: -1} }

type staticRows struct {
	rows  [][]any
	index int
}

func (s *staticRows) Next() bool { s.index++; return s.index < len(s.rows) }

func (s *staticRows) Scan(dest ...any) error {
	row := s.rows[s.index]
	if len(dest) != len(row) {
		return fmt.Errorf("audit: scan %d destinations from %d values", len(dest), len(row))
	}
	for i, value := range row {
		target := reflect.ValueOf(dest[i])
		if target.Kind() != reflect.Pointer || target.IsNil() {
			return fmt.Errorf("audit: destination %d is not a pointer", i)
		}
		source := reflect.ValueOf(value)
		if !source.Type().AssignableTo(target.Elem().Type()) {
			return fmt.Errorf("audit: cannot assign %s to %s", source.Type(), target.Elem().Type())
		}
		target.Elem().Set(source)
	}
	return nil
}

func (*staticRows) Err() error   { return nil }
func (*staticRows) Close() error { return nil }
