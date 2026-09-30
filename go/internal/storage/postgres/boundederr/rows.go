// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package boundederr

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
)

// rows wraps a pgx result set. Next is where a server error arrives after the
// query was accepted, so it is bounded like a call error. io.EOF, the end of the
// result set, passes through untouched.
//
// failed holds the bounded error Next returned. database/sql closes a result set
// after a Next error, and pgx's Rows.Close returns the same raw error again, so
// Close answers with the saved value instead of bounding and logging it twice.
type rows struct {
	inner  driver.Rows
	query  string
	obs    *observer
	failed *Error
}

func newRows(inner driver.Rows, query string, obs *observer) driver.Rows {
	return &rows{inner: inner, query: query, obs: obs}
}

var (
	_ driver.Rows                           = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeLength           = (*rows)(nil)
	_ driver.RowsColumnTypePrecisionScale   = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
)

// Columns returns the column names.
func (r *rows) Columns() []string { return r.inner.Columns() }

// Close ends the result set. An error that repeats the one Next already
// reported returns that bounded value without a second log record.
func (r *rows) Close() error {
	err := r.inner.Close()
	if r.failed != nil && errors.Is(err, r.failed.cause) {
		return r.failed
	}
	return r.obs.bound(context.Background(), opRows, r.query, err)
}

// Next advances to the next row and remembers a bounded failure for Close.
func (r *rows) Next(dest []driver.Value) error {
	err := r.obs.bound(context.Background(), opRows, r.query, r.inner.Next(dest))
	var bounded *Error
	if errors.As(err, &bounded) {
		r.failed = bounded
	}
	return err
}

// ColumnTypeDatabaseTypeName forwards the column's database type name, or ""
// when the wrapped rows do not report one, as database/sql does.
func (r *rows) ColumnTypeDatabaseTypeName(index int) string {
	if typed, ok := r.inner.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return typed.ColumnTypeDatabaseTypeName(index)
	}
	return ""
}

// ColumnTypeLength forwards the column's length, or (0, false) when unreported.
func (r *rows) ColumnTypeLength(index int) (int64, bool) {
	if typed, ok := r.inner.(driver.RowsColumnTypeLength); ok {
		return typed.ColumnTypeLength(index)
	}
	return 0, false
}

// ColumnTypePrecisionScale forwards precision and scale, or (0, 0, false) when
// unreported.
func (r *rows) ColumnTypePrecisionScale(index int) (precision, scale int64, ok bool) {
	if typed, has := r.inner.(driver.RowsColumnTypePrecisionScale); has {
		return typed.ColumnTypePrecisionScale(index)
	}
	return 0, 0, false
}

// ColumnTypeScanType forwards the Go scan type, or the empty interface type
// database/sql uses when the driver does not report one.
func (r *rows) ColumnTypeScanType(index int) reflect.Type {
	if typed, ok := r.inner.(driver.RowsColumnTypeScanType); ok {
		return typed.ColumnTypeScanType(index)
	}
	return reflect.TypeOf(new(any)).Elem()
}
