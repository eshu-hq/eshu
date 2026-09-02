// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import "fmt"

// ScriptedRows returns canned rows for one query and satisfies the
// pgstatus.Rows surface a Postgres-backed store scans. Data holds one slice
// per row, in the column order the store's SELECT lists.
//
// It lives here rather than in a _test.go file because two packages scan
// against it and Go never compiles a package's _test.go files into anything
// another package can import: the semantic-search family moved to
// internal/query/semanticsearch for #6060 and its scope-resolver tests script
// rows, while root package query's admin replay tests script them too.
//
// The zero value is an empty result set, which is a legitimate script: a store
// that must report "no rows" is tested with it.
//
// Not safe for concurrent use. A single Rows value is a cursor with position,
// so two goroutines scanning one instance would interleave rows; give each
// goroutine its own.
type ScriptedRows struct {
	// Data is the scripted result set, one entry per row.
	Data [][]any
	idx  int
}

// Next advances the cursor and reports whether a row is available.
func (r *ScriptedRows) Next() bool {
	if r.idx >= len(r.Data) {
		return false
	}
	r.idx++
	return true
}

// Scan copies the current row into dest.
//
// It fails on an arity or type mismatch rather than leaving a destination at
// its zero value: a store whose SELECT and scan targets have drifted apart
// would otherwise read as a store returning empty strings, which looks like a
// data problem instead of the code problem it is.
func (r *ScriptedRows) Scan(dest ...any) error {
	row := r.Data[r.idx-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan arity %d != %d", len(dest), len(row))
	}
	for i, d := range dest {
		switch target := d.(type) {
		case *string:
			*target = row[i].(string)
		case *int:
			*target = row[i].(int)
		case *[]byte:
			*target = row[i].([]byte)
		default:
			return fmt.Errorf("unsupported scan target %T", d)
		}
	}
	return nil
}

// Err reports no iteration error. A store's mid-scan failure path is covered
// by returning an error from the queryer instead, so this stays a pure
// happy-path script rather than growing a second failure knob.
func (r *ScriptedRows) Err() error { return nil }

// Close is a no-op; there is nothing to release.
func (r *ScriptedRows) Close() error { return nil }
