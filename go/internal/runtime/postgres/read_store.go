// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

var _ db.ReadStore = fencedQueryer{}

// QueryRowContext runs one guarded read and defers its errors to Scan.
func (q fencedQueryer) QueryRowContext(ctx context.Context, statement string, args ...any) db.Row {
	rows, err := q.QueryContext(ctx, statement, args...)
	return &fencedRow{rows: rows, err: err}
}

type fencedRow struct {
	rows db.Rows
	err  error
}

func (r *fencedRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for _, value := range dest {
		if _, raw := value.(*sql.RawBytes); raw {
			_ = r.rows.Close()
			return errors.New("sql: RawBytes isn't allowed on Row.Scan")
		}
	}
	if !r.rows.Next() {
		err := r.rows.Err()
		_ = r.rows.Close()
		if err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := r.rows.Scan(dest...); err != nil {
		_ = r.rows.Close()
		return err
	}
	return r.rows.Close()
}

// BeginReadOnlySnapshot fences the borrowed connection before starting a
// repeatable-read transaction. The transaction owns the connection thereafter.
func (q fencedQueryer) BeginReadOnlySnapshot(ctx context.Context) (db.ReadTransaction, error) {
	conn, err := q.access.borrowFresh(ctx)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	q.access.observe("reader", StageBusinessQuery, started, err)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("begin reader snapshot: %w", err)
	}
	owned := &readTransaction{tx: tx, conn: conn, access: q.access}
	owned.stop = context.AfterFunc(ctx, func() { _ = owned.finish(false) })
	return owned, nil
}

type readTransaction struct {
	tx     *sql.Tx
	conn   *sql.Conn
	access *Access
	once   sync.Once
	stop   func() bool
	err    error
}

var _ db.ReadTransaction = (*readTransaction)(nil)

func (r *readTransaction) QueryContext(ctx context.Context, statement string, args ...any) (db.Rows, error) {
	started := time.Now()
	rows, err := r.tx.QueryContext(ctx, statement, args...)
	r.access.observe("reader", StageBusinessQuery, started, err)
	if err != nil {
		return nil, err
	}
	return &txRows{rows: rows}, nil
}

func (r *readTransaction) QueryRowContext(ctx context.Context, statement string, args ...any) db.Row {
	rows, err := r.QueryContext(ctx, statement, args...)
	return &fencedRow{rows: rows, err: err}
}

func (r *readTransaction) finish(commit bool) error {
	executed := false
	r.once.Do(func() {
		executed = true
		if commit {
			r.err = r.tx.Commit()
		} else {
			r.err = r.tx.Rollback()
		}
		r.err = errors.Join(r.err, r.conn.Close())
	})
	if !executed {
		return sql.ErrTxDone
	}
	return r.err
}

func (r *readTransaction) Commit() error   { r.stop(); return r.finish(true) }
func (r *readTransaction) Rollback() error { r.stop(); return r.finish(false) }

type txRows struct{ rows *sql.Rows }

func (r *txRows) Next() bool {
	ok := r.rows.Next()
	if !ok {
		_ = r.rows.Close()
	}
	return ok
}

func (r *txRows) Scan(dest ...any) error {
	err := r.rows.Scan(dest...)
	if err != nil {
		_ = r.rows.Close()
	}
	return err
}

func (r *txRows) Err() error {
	err := r.rows.Err()
	if err != nil {
		_ = r.rows.Close()
	}
	return err
}
func (r *txRows) Close() error { return r.rows.Close() }
