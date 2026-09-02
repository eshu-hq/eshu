// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factwritetest

import (
	"context"
	"database/sql"
)

// FakeExecer is a [factwrite.Execer] that records every ExecContext call
// instead of executing it, so a writer test can assert on the recorded SQL
// and its positional arguments.
type FakeExecer struct {
	Execs []ExecCall
}

// ExecCall is one recorded ExecContext invocation.
type ExecCall struct {
	Query string
	Args  []any
}

// ExecContext implements [factwrite.Execer] by recording the call and
// returning a no-op, always-successful [sql.Result].
func (f *FakeExecer) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.Execs = append(f.Execs, ExecCall{Query: query, Args: args})
	return fakeResult{}, nil
}

// fakeResult is the [sql.Result] FakeExecer.ExecContext returns. Both methods
// report success: 0 for a never-used auto-increment id, 1 affected row per
// call, which is what every current caller of FakeExecer asserts against.
type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeResult) RowsAffected() (int64, error) { return 1, nil }
