// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fake

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

var _ db.Rows = (*Rows)(nil)

// Rows is an in-memory db.Rows: a fixed set of rows a caller stages ahead of
// time, served in order by Next and Scan.
//
// A Rows value staged in ExecQueryer.QueryResponses, or returned by a Route,
// with FailWith set is never handed back to the caller: ExecQueryer.
// QueryContext returns FailWith as the call's error instead, so a test can
// simulate a query that fails outright as easily as one that succeeds with
// rows.
type Rows struct {
	// Data holds one []any per row, in the same column order Scan's
	// destinations are given in.
	Data [][]any
	// FailWith, when non-nil, is returned as QueryContext's error instead
	// of this Rows value.
	FailWith error
	// Adapt, when non-nil, reshapes each row before Scan checks it against
	// the caller's destination count. ExecQueryer.QueryContext fills this in
	// from its own Adapt when a handed-out Rows leaves it nil, so a caller
	// can set it once on the ExecQueryer instead of on every Rows.
	Adapt RowAdapter

	index int
}

// Next reports whether another row is available and, if so, advances Scan
// to it.
func (r *Rows) Next() bool {
	return r.index < len(r.Data)
}

// Scan copies the current row's columns into dest, in order, converting
// each column to the concrete type dest[i] points at. It supports the same
// destination types database/sql callers in this package use: the Go
// primitives, []byte, time.Time, array.Float64Array and its underlying
// []float64, and the sql.Null* wrapper types. An unsupported destination
// type, a column/destination count mismatch, or a Scan call before Next
// each return a descriptive error rather than panicking.
func (r *Rows) Scan(dest ...any) error {
	if r.index >= len(r.Data) {
		return errors.New("fake: Scan called without a row; call Next first")
	}
	row := r.Data[r.index]
	if r.Adapt != nil {
		row = r.Adapt(len(dest), row)
	}
	if len(dest) != len(row) {
		return fmt.Errorf("fake: scan destination count = %d, want %d", len(dest), len(row))
	}

	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			value, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("fake: row[%d] type = %T, want string", i, row[i])
			}
			*target = value
		case *bool:
			value, ok := row[i].(bool)
			if !ok {
				return fmt.Errorf("fake: row[%d] type = %T, want bool", i, row[i])
			}
			*target = value
		case *[]byte:
			value, ok := row[i].([]byte)
			if !ok {
				return fmt.Errorf("fake: row[%d] type = %T, want []byte", i, row[i])
			}
			*target = value
		case *time.Time:
			value, ok := row[i].(time.Time)
			if !ok {
				return fmt.Errorf("fake: row[%d] type = %T, want time.Time", i, row[i])
			}
			*target = value
		case *int:
			value, ok := row[i].(int)
			if !ok {
				return fmt.Errorf("fake: row[%d] type = %T, want int", i, row[i])
			}
			*target = value
		case *int64:
			value, ok := row[i].(int64)
			if !ok {
				return fmt.Errorf("fake: row[%d] type = %T, want int64", i, row[i])
			}
			*target = value
		case *float64:
			value, ok := row[i].(float64)
			if !ok {
				return fmt.Errorf("fake: row[%d] type = %T, want float64", i, row[i])
			}
			*target = value
		case *[]float64:
			value, ok := row[i].([]float64)
			if !ok {
				return fmt.Errorf("fake: row[%d] type = %T, want []float64", i, row[i])
			}
			*target = append((*target)[:0], value...)
		case *array.Float64Array:
			value, ok := row[i].([]float64)
			if !ok {
				return fmt.Errorf("fake: row[%d] type = %T, want []float64", i, row[i])
			}
			*target = append((*target)[:0], value...)
		case *sql.NullString:
			value, ok := row[i].(sql.NullString)
			if !ok {
				return fmt.Errorf("fake: row[%d] type = %T, want sql.NullString", i, row[i])
			}
			*target = value
		case *sql.NullBool:
			value, ok := row[i].(sql.NullBool)
			if !ok {
				return fmt.Errorf("fake: row[%d] type = %T, want sql.NullBool", i, row[i])
			}
			*target = value
		case *sql.NullInt64:
			value, ok := row[i].(sql.NullInt64)
			if !ok {
				return fmt.Errorf("fake: row[%d] type = %T, want sql.NullInt64", i, row[i])
			}
			*target = value
		case *sql.NullTime:
			value, ok := row[i].(sql.NullTime)
			if !ok {
				return fmt.Errorf("fake: row[%d] type = %T, want sql.NullTime", i, row[i])
			}
			*target = value
		default:
			return fmt.Errorf("fake: unsupported scan target %T", dest[i])
		}
	}

	r.index++
	return nil
}

// Err always returns nil: Rows reports a staged failure by having
// ExecQueryer.QueryContext return FailWith at call time rather than by
// failing after row iteration, matching the db.Rows contract for a fake
// that never partially fails mid-scan.
func (r *Rows) Err() error { return nil }

// Close is a no-op: Rows holds no external resource to release.
func (r *Rows) Close() error { return nil }

// Result is an in-memory sql.Result: it reports exactly one row affected and
// no last-insert id, the shape every ExecContext call in this package's
// callers expects for a successful single-row write.
type Result struct{}

// LastInsertId always returns 0: no caller in this package's tests relies on
// a generated id.
func (Result) LastInsertId() (int64, error) { return 0, nil }

// RowsAffected always returns 1, matching a successful single-row write.
func (Result) RowsAffected() (int64, error) { return 1, nil }
