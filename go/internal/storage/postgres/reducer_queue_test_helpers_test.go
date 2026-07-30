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
	// Simulate a real "no conflicts" batch INSERT: every bound row is a fresh
	// admission, so RowsAffected equals the row count the args slice encodes
	// (len(args)/columnsPerReducerEnqueue), not a fixed constant. Every caller
	// of this fake only issues the reducer batch-enqueue INSERT (never a
	// differently-shaped Fail/Ack/Heartbeat exec), so this division is exact.
	return rowsAffectedResult{rowsAffected: int64(len(args) / columnsPerReducerEnqueue)}, nil
}

func (r *reducerRecordingDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, nil
}

type rowsAffectedResult struct {
	rowsAffected int64
}

func (r rowsAffectedResult) LastInsertId() (int64, error) { return 0, nil }
func (r rowsAffectedResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }
