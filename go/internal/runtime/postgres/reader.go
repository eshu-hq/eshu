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

type fencedQueryer struct{ access *Access }

var _ db.Queryer = fencedQueryer{}

func (q fencedQueryer) QueryContext(ctx context.Context, statement string, args ...any) (db.Rows, error) {
	a := q.access
	point, ok := ctx.Value(checkpointKey{}).(checkpoint)
	if !ok || point.owner != a {
		return nil, ErrMissingCheckpoint
	}
	// Fence and pool wait are bounded independently of the business query.
	// The business cursor keeps the original request context alive.
	fenceCtx, cancel := context.WithTimeout(ctx, a.replayTimeout)
	defer cancel()
	borrowed := time.Now()
	conn, err := a.reader.Conn(fenceCtx)
	a.observe("reader", StageReaderBorrow, borrowed, err)
	if err != nil {
		return nil, fmt.Errorf("borrow PostgreSQL reader: %w", errors.Join(ErrReaderUnavailable, err))
	}
	if err = a.checkReader(fenceCtx, conn, point); err != nil {
		_ = conn.Close()
		return nil, err
	}
	started := time.Now()
	rows, err := conn.QueryContext(ctx, statement, args...)
	a.observe("reader", StageBusinessQuery, started, err)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return newOwnedRows(ctx, rows, conn), nil
}

func (a *Access) checkReader(ctx context.Context, conn *sql.Conn, point checkpoint) error {
	started := time.Now()
	var readOnly, systemID, database string
	var recovery bool
	err := conn.QueryRowContext(ctx, `SELECT current_setting('default_transaction_read_only'), pg_is_in_recovery(), system_identifier::text, current_database() FROM pg_control_system()`).Scan(&readOnly, &recovery, &systemID, &database)
	if err == nil && (readOnly != "on" || systemID != point.systemID || database != point.database || recovery == a.samePrimary) {
		err = ErrWrongTopology
	}
	a.observe("reader", StageReaderIdentity, started, err)
	if err != nil {
		if errors.Is(err, ErrWrongTopology) {
			return fmt.Errorf("reader identity refused: %w", err)
		}
		return fmt.Errorf("reader identity unavailable: %w", errors.Join(ErrReaderUnavailable, err))
	}
	if a.samePrimary {
		return nil
	}
	replayStarted := time.Now()
	defer func() { a.observe("reader", StageReaderReplay, replayStarted, err) }()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var caughtUp bool
		err = conn.QueryRowContext(ctx, `SELECT COALESCE(pg_last_wal_replay_lsn() >= $1::pg_lsn, false)`, point.lsn).Scan(&caughtUp)
		if err != nil {
			if ctx.Err() != nil {
				err = replayContextError(ctx.Err())
				return err
			}
			err = errors.Join(ErrReaderUnavailable, err)
			return fmt.Errorf("reader replay check failed: %w", err)
		}
		if caughtUp {
			return nil
		}
		select {
		case <-ctx.Done():
			err = replayContextError(ctx.Err())
			return err
		case <-ticker.C:
		}
	}
}

type ownedRows struct {
	rows     *sql.Rows
	conn     *sql.Conn
	once     sync.Once
	stop     func() bool
	closeErr error
}

var _ db.Rows = (*ownedRows)(nil)

func newOwnedRows(ctx context.Context, rows *sql.Rows, conn *sql.Conn) *ownedRows {
	owned := &ownedRows{rows: rows, conn: conn}
	// The callback cannot reference stop: cancellation may run before the
	// AfterFunc return value has been published.
	owned.stop = context.AfterFunc(ctx, func() { _ = owned.finish() })
	return owned
}

func (r *ownedRows) finish() error {
	r.once.Do(func() { r.closeErr = errors.Join(r.rows.Close(), r.conn.Close()) })
	return r.closeErr
}

func (r *ownedRows) Next() bool {
	ok := r.rows.Next()
	if !ok {
		_ = r.Close()
	}
	return ok
}

func (r *ownedRows) Scan(dest ...any) error {
	err := r.rows.Scan(dest...)
	if err != nil {
		_ = r.Close()
	}
	return err
}

func (r *ownedRows) Err() error {
	err := r.rows.Err()
	if err != nil {
		_ = r.Close()
	}
	return err
}

func (r *ownedRows) Close() error {
	if r.stop != nil {
		r.stop()
	}
	return r.finish()
}

func replayContextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.Join(ErrReaderStale, err)
	}
	return err
}
