// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"fmt"
	"time"
)

// affectedResult is a sql.Result whose RowsAffected is configurable, unlike
// the shared zero-valued result helper. Shared across the admin identity and
// provider-config fake-DB test doubles in this package.
type affectedResult struct {
	affected int64
}

func (r affectedResult) LastInsertId() (int64, error) { return 0, nil }
func (r affectedResult) RowsAffected() (int64, error) { return r.affected, nil }

// scalarRows is a minimal Rows fake supporting *int, *string, *bool scans
// plus nullable time/string columns scanned as sql.NullTime/sql.NullString or
// *time.Time. Shared across the admin identity and provider-config fake-DB
// test doubles in this package.
type scalarRows struct {
	data [][]any
	idx  int
}

func (r *scalarRows) Next() bool {
	if r.idx == 0 && r.data == nil {
		return false
	}
	r.idx++
	return r.idx <= len(r.data)
}

func (r *scalarRows) Scan(dest ...any) error {
	row := r.data[r.idx-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan: got %d dest, have %d cols", len(dest), len(row))
	}
	for i, val := range row {
		switch d := dest[i].(type) {
		case *int:
			*d = val.(int)
		case *string:
			if val == nil {
				*d = ""
				continue
			}
			*d = val.(string)
		case *bool:
			*d = val.(bool)
		case *time.Time:
			if val == nil {
				*d = time.Time{}
				continue
			}
			*d = val.(time.Time)
		case *sql.NullTime:
			if val == nil {
				*d = sql.NullTime{}
				continue
			}
			*d = sql.NullTime{Time: val.(time.Time), Valid: true}
		case *sql.NullString:
			if val == nil {
				*d = sql.NullString{}
				continue
			}
			*d = sql.NullString{String: val.(string), Valid: true}
		default:
			return fmt.Errorf("unsupported scan dest type %T", dest[i])
		}
	}
	return nil
}

func (r *scalarRows) Err() error   { return nil }
func (r *scalarRows) Close() error { return nil }
