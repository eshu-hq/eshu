// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfstate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fixtures"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
)

// This file carries the test-only doubles the tfstate family moved with
// from the postgres root (issue #8361). They mirror the root's
// work_queue_lifecycle_test.go fakes pared down to what this family's tests
// stage: a FIFO response queue plus query recording. The root fake's
// backfill/content routing is intentionally absent — no tfstate fixture
// stages those queries.

// fakeExecQueryer answers QueryContext from a staged FIFO queue and records
// every call for SQL-shape assertions. A query with no staged response is
// an error, so a test that issues a query it did not stage fails loudly.
type fakeExecQueryer struct {
	mu             sync.Mutex
	execs          []fakeExecCall
	queries        []fakeQueryCall
	queryResponses []queueFakeRows
}

type fakeExecCall struct {
	query string
	args  []any
}

type fakeQueryCall struct {
	query string
	args  []any
}

func (f *fakeExecQueryer) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs = append(f.execs, fakeExecCall{query: query, args: args})
	return fakeResult{}, nil
}

func (f *fakeExecQueryer) QueryContext(
	_ context.Context,
	query string,
	args ...any,
) (db.Rows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, fakeQueryCall{query: query, args: args})
	if len(f.queryResponses) == 0 {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	rows := f.queryResponses[0]
	f.queryResponses = f.queryResponses[1:]
	if rows.err != nil {
		return nil, rows.err
	}
	return &rows, nil
}

type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) { return 0, nil }

func (fakeResult) RowsAffected() (int64, error) { return 1, nil }

// queueFakeRows is one staged query response: rows of column values, or err
// to fail the query. Mirrors the root fake's row type so fixtures moved
// unchanged.
type queueFakeRows struct {
	rows  [][]any
	err   error
	index int
}

func (r *queueFakeRows) Next() bool {
	return r.index < len(r.rows)
}

func (r *queueFakeRows) Scan(dest ...any) error {
	if r.index >= len(r.rows) {
		return errors.New("scan called without row")
	}
	row := fixtures.PadQueueFakeRow(r.rows[r.index], len(dest))
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(row))
	}

	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			value, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want string", i, row[i])
			}
			*target = value
		case *bool:
			value, ok := row[i].(bool)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want bool", i, row[i])
			}
			*target = value
		case *[]byte:
			value, ok := row[i].([]byte)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want []byte", i, row[i])
			}
			*target = value
		case *time.Time:
			value, ok := row[i].(time.Time)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want time.Time", i, row[i])
			}
			*target = value
		case *int:
			value, ok := row[i].(int)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want int", i, row[i])
			}
			*target = value
		case *int64:
			value, ok := row[i].(int64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want int64", i, row[i])
			}
			*target = value
		case *float64:
			value, ok := row[i].(float64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want float64", i, row[i])
			}
			*target = value
		case *[]float64:
			value, ok := row[i].([]float64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want []float64", i, row[i])
			}
			*target = append((*target)[:0], value...)
		case *array.Float64Array:
			value, ok := row[i].([]float64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want []float64", i, row[i])
			}
			*target = append((*target)[:0], value...)
		case *sql.NullString:
			value, ok := row[i].(sql.NullString)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want sql.NullString", i, row[i])
			}
			*target = value
		case *sql.NullBool:
			value, ok := row[i].(sql.NullBool)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want sql.NullBool", i, row[i])
			}
			*target = value
		case *sql.NullInt64:
			value, ok := row[i].(sql.NullInt64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want sql.NullInt64", i, row[i])
			}
			*target = value
		case *sql.NullTime:
			value, ok := row[i].(sql.NullTime)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want sql.NullTime", i, row[i])
			}
			*target = value
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}

	r.index++
	return nil
}

func (r *queueFakeRows) Err() error { return nil }

func (r *queueFakeRows) Close() error { return nil }

// Row padding for the fakes above is fixtures.PadQueueFakeRow (issue
// #8552): the reducer-claim branches are no-ops for tfstate shapes but keep
// Scan byte-faithful to the root fake through the single shared copy.

// tfstateSQLDB adapts a *sql.DB to db.Queryer for the family's live-Postgres
// tests. It replaces the root's SQLDB adapter, which stays in the parent
// package with the rest of the ingestion write path.
type tfstateSQLDB struct {
	db *sql.DB
}

func (s tfstateSQLDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return s.db.QueryContext(ctx, query, args...)
}

// tfstateMigrationSQL returns the shipped bootstrap SQL for a migration def
// name, sourced from the migrations package's embed.FS — the same bytes the
// root's MigrationSQL serves, without importing the parent package.
func tfstateMigrationSQL(t *testing.T, name string) string {
	t.Helper()
	for _, definition := range migrations.BootstrapDefinitions() {
		if definition.Name == name {
			return definition.SQL
		}
	}
	t.Fatalf("migration not found: %q", name)
	return ""
}
