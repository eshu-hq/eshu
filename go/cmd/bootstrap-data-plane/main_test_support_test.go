// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

type fakeBootstrapDB struct {
	execCalls int
	closed    bool
	closeErr  error
	execs     []fakeBootstrapCall
	queries   []fakeBootstrapCall
	queryRows []fakeBootstrapRows
}

func (f *fakeBootstrapDB) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.execCalls++
	f.execs = append(f.execs, fakeBootstrapCall{query: query, args: args})
	return fakeBootstrapResult{}, nil
}

func (f *fakeBootstrapDB) QueryContext(
	_ context.Context,
	query string,
	args ...any,
) (postgres.Rows, error) {
	f.queries = append(f.queries, fakeBootstrapCall{query: query, args: args})
	if len(f.queryRows) == 0 {
		return &fakeBootstrapRows{}, nil
	}
	rows := f.queryRows[0]
	f.queryRows = f.queryRows[1:]
	return &rows, nil
}

func (f *fakeBootstrapDB) Close() error {
	f.closed = true
	return f.closeErr
}

type fakeBootstrapCall struct {
	query string
	args  []any
}

type fakeBootstrapResult struct{}

func (fakeBootstrapResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeBootstrapResult) RowsAffected() (int64, error) { return 0, nil }

type fakeBootstrapRows struct {
	rows  [][]any
	index int
	err   error
}

func (r *fakeBootstrapRows) Next() bool {
	return r.index < len(r.rows)
}

func (r *fakeBootstrapRows) Scan(dest ...any) error {
	if r.index >= len(r.rows) {
		return errors.New("scan called without row")
	}
	row := r.rows[r.index]
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(row))
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *bool:
			value, ok := row[i].(bool)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want bool", i, row[i])
			}
			*target = value
		case *string:
			value, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want string", i, row[i])
			}
			*target = value
		case *[]byte:
			value, ok := row[i].([]byte)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want []byte", i, row[i])
			}
			*target = value
		default:
			return fmt.Errorf("scan dest[%d] type = %T", i, dest[i])
		}
	}
	r.index++
	return nil
}

func (r *fakeBootstrapRows) Err() error {
	return r.err
}

func (r *fakeBootstrapRows) Close() error {
	return nil
}

type fakeNeo4jExecutor struct {
	calls   int
	cyphers []string
}

func (f *fakeNeo4jExecutor) ExecuteCypher(_ context.Context, statement graph.CypherStatement) error {
	f.calls++
	f.cyphers = append(f.cyphers, statement.Cypher)
	return nil
}

type fakeGraphSchemaInspector struct {
	names             map[string]struct{}
	err               error
	calls             int
	sawDeadline       bool
	deadlineRemaining time.Duration
}

func (f *fakeGraphSchemaInspector) GraphSchemaObjectNames(ctx context.Context) (map[string]struct{}, error) {
	f.calls++
	deadline, ok := ctx.Deadline()
	if ok {
		f.sawDeadline = true
		f.deadlineRemaining = time.Until(deadline)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.names, nil
}

type deadlineRecordingExecutor struct {
	sawDeadline       bool
	deadlineRemaining time.Duration
}

func (d *deadlineRecordingExecutor) ExecuteCypher(ctx context.Context, _ graph.CypherStatement) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil
	}
	d.sawDeadline = true
	d.deadlineRemaining = time.Until(deadline)
	return nil
}
