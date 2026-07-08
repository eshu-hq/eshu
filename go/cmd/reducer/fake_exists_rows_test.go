// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"
	"fmt"
)

// fakeExistsRows returns a single boolean row, modeling a `SELECT EXISTS(...)`
// query result. It is used by fakeReducerDB.QueryContext (main_test.go) to
// answer the CodeValueFlowBackfillStateStore.IsComplete check the
// ProjectedSourceEdgeBackfiller issues at buildReducerService startup.
type fakeExistsRows struct {
	value bool
	read  bool
}

func (r *fakeExistsRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}

func (r *fakeExistsRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return fmt.Errorf("scan: got %d dest, want 1", len(dest))
	}
	b, ok := dest[0].(*bool)
	if !ok {
		return fmt.Errorf("unsupported scan dest type %T", dest[0])
	}
	*b = r.value
	return nil
}

func (r *fakeExistsRows) Err() error   { return nil }
func (r *fakeExistsRows) Close() error { return nil }

// fakeGenerationRows returns a single string row, modeling the
// active_generation_id lookup on ingestion_scopes that the generation-freshness
// guard issues. value is the generation id the fake DB reports as active.
type fakeGenerationRows struct {
	value *string
	read  bool
}

func (r *fakeGenerationRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}

func (r *fakeGenerationRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return fmt.Errorf("scan: got %d dest, want 1", len(dest))
	}
	switch d := dest[0].(type) {
	case *sql.NullString:
		if r.value != nil {
			d.Valid = true
			d.String = *r.value
		} else {
			d.Valid = false
		}
	case *string:
		if r.value != nil {
			*d = *r.value
		}
	default:
		return fmt.Errorf("unsupported scan dest type %T", dest[0])
	}
	return nil
}

func (r *fakeGenerationRows) Err() error   { return nil }
func (r *fakeGenerationRows) Close() error { return nil }
