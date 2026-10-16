// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// scriptedDB is a db.Beginner whose transactions answer each statement from
// answer, so the drain loop runs without Postgres. It records every
// statement and every transaction outcome in order.
type scriptedDB struct {
	answer     func(query string, args []any) ([][]any, error)
	statements []string
	commits    int
	rollbacks  int
}

func (d *scriptedDB) Begin(context.Context) (db.Transaction, error) { return &scriptedTx{db: d}, nil }

// QueryContext answers a statement outside a transaction, as the census runs.
func (d *scriptedDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return (&scriptedTx{db: d}).QueryContext(ctx, query, args...)
}

type scriptedTx struct{ db *scriptedDB }

func (tx *scriptedTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	tx.db.statements = append(tx.db.statements, query)
	rows, err := tx.db.answer(query, args)
	if err != nil {
		return nil, err
	}
	return &scriptedRows{rows: rows}, nil
}

func (tx *scriptedTx) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	tx.db.statements = append(tx.db.statements, query)
	return nil, nil
}

func (tx *scriptedTx) Commit() error   { tx.db.commits++; return nil }
func (tx *scriptedTx) Rollback() error { tx.db.rollbacks++; return nil }

type scriptedRows struct {
	rows [][]any
	at   int
}

func (r *scriptedRows) Next() bool { r.at++; return r.at <= len(r.rows) }

func (r *scriptedRows) Scan(dest ...any) error {
	row := r.rows[r.at-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan %d columns into %d", len(row), len(dest))
	}
	for i, value := range row {
		switch target := dest[i].(type) {
		case *bool:
			*target = value.(bool)
		case *int64:
			*target = value.(int64)
		case *string:
			*target = value.(string)
		case *sql.NullString:
			if value == nil {
				*target = sql.NullString{}
			} else {
				*target = sql.NullString{String: value.(string), Valid: true}
			}
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}
	return nil
}

func (r *scriptedRows) Err() error   { return nil }
func (r *scriptedRows) Close() error { return nil }

// page answers one batch statement: eligible, rows selected, rows deleted,
// and the page's last key.
func page(eligible bool, selected, deleted int64, first, second string) [][]any {
	return [][]any{{eligible, selected, deleted, first, second}}
}

// isTermsBatch reports the terms batch statement.
func isTermsBatch(query string) bool { return strings.Contains(query, "terms pre-delete batch") }

// isDocumentsBatch reports the documents batch statement.
func isDocumentsBatch(query string) bool {
	return strings.Contains(query, "documents pre-delete batch")
}

// isTryLock reports the per-batch advisory try lock.
func isTryLock(query string) bool { return query == tryLockQuery }
