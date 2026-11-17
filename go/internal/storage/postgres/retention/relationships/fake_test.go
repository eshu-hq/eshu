// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// call is one statement the scripted database received.
type call struct {
	tx    int
	query string
	args  []any
}

// reply is the scripted answer to one statement.
type reply struct {
	rows     [][]any
	affected int64
	err      error
}

// scriptedDB is a db.Beginner whose transactions answer every statement from
// handler, so the census and the batch loop run without Postgres. It records
// every statement with its transaction number, and every commit and rollback.
type scriptedDB struct {
	handler   func(c call) reply
	calls     []call
	begins    int
	commits   int
	rollbacks int
	commitErr error
}

func (d *scriptedDB) Begin(context.Context) (db.Transaction, error) {
	d.begins++
	return &scriptedTx{db: d, id: d.begins}, nil
}

// queriesIn returns the statements of one transaction, in order.
func (d *scriptedDB) queriesIn(tx int) []string {
	var queries []string
	for _, c := range d.calls {
		if c.tx == tx {
			queries = append(queries, c.query)
		}
	}
	return queries
}

// callsFor returns every call of one statement, across transactions.
func (d *scriptedDB) callsFor(query string) []call {
	var matched []call
	for _, c := range d.calls {
		if c.query == query {
			matched = append(matched, c)
		}
	}
	return matched
}

type scriptedTx struct {
	db *scriptedDB
	id int
}

func (tx *scriptedTx) answer(query string, args []any) reply {
	c := call{tx: tx.id, query: query, args: args}
	tx.db.calls = append(tx.db.calls, c)
	return tx.db.handler(c)
}

func (tx *scriptedTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	r := tx.answer(query, args)
	if r.err != nil {
		return nil, r.err
	}
	return &scriptedRows{rows: r.rows}, nil
}

func (tx *scriptedTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r := tx.answer(query, args)
	if r.err != nil {
		return nil, r.err
	}
	return affected(r.affected), nil
}

func (tx *scriptedTx) Commit() error {
	tx.db.commits++
	return tx.db.commitErr
}

func (tx *scriptedTx) Rollback() error {
	tx.db.rollbacks++
	return nil
}

// affected is a sql.Result that reports a scripted row count.
type affected int64

func (affected) LastInsertId() (int64, error) { return 0, errors.New("not supported") }

func (a affected) RowsAffected() (int64, error) { return int64(a), nil }

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
		case *string:
			*target = value.(string)
		case *bool:
			*target = value.(bool)
		case *int64:
			*target = value.(int64)
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}
	return nil
}

func (r *scriptedRows) Err() error   { return nil }
func (r *scriptedRows) Close() error { return nil }

// idRows turns ids into single-column rows.
func idRows(ids ...string) [][]any {
	rows := make([][]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, []any{id})
	}
	return rows
}

// idsArg reads the generation id array a statement was called with.
func idsArg(c call) []string {
	ids, _ := c.args[0].([]string)
	return ids
}
