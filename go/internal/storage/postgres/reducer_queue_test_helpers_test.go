// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
)

// reducerRecordingDB records ExecContext calls for verification.
type reducerRecordingDB struct {
	execCount int
	execs     []reducerRecordedExec
}

type reducerRecordedExec struct {
	query string
	args  []any
}

func (r *reducerRecordingDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r.execCount++
	r.execs = append(r.execs, reducerRecordedExec{
		query: query,
		args:  append([]any(nil), args...),
	})
	return reducerProofResult{}, nil
}

func (r *reducerRecordingDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, nil
}

// reducerProofResult is a minimal sql.Result implementation for testing.
type reducerProofResult struct{}

func (reducerProofResult) LastInsertId() (int64, error) { return 0, nil }
func (reducerProofResult) RowsAffected() (int64, error) { return 1, nil }

type rowsAffectedResult struct {
	rowsAffected int64
}

func (r rowsAffectedResult) LastInsertId() (int64, error) { return 0, nil }
func (r rowsAffectedResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }
