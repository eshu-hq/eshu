// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type fencedQueryer struct{ access *Access }

var errReaderPermitTimeout = errors.New("guarded reader permit wait timed out")

// readerConnection returns its permit only after database/sql has returned the
// physical connection to the pool. Every guarded read owns exactly one lease.
type readerConnection struct {
	*sql.Conn
	permits     chan struct{}
	reservation *readerReservation
	member      *physicalReaderMember
	once        sync.Once
	err         error
}

func (c *readerConnection) Close() error {
	c.once.Do(func() {
		c.err = c.Conn.Close()
		if c.reservation != nil {
			c.reservation.ReleaseOne()
		} else if c.permits != nil {
			c.permits <- struct{}{}
		}
	})
	return c.err
}

var _ db.Queryer = fencedQueryer{}

func (q fencedQueryer) QueryContext(ctx context.Context, statement string, args ...any) (db.Rows, error) {
	a := q.access
	conn, err := a.borrowFresh(ctx)
	if err != nil {
		return nil, err
	}
	a.startReaderQuery(ctx, conn.Conn)
	started := time.Now()
	rows, err := conn.QueryContext(ctx, statement, args...)
	a.observe("reader", StageBusinessQuery, started, err)
	if err != nil {
		_ = conn.Close()
		return nil, privateFailure(failureReaderQuery, err)
	}
	return newOwnedRows(ctx, rows, conn), nil
}

func (a *Access) borrowFresh(ctx context.Context) (*readerConnection, error) {
	if len(a.readerMembers) > 0 {
		return a.borrowFleet(ctx)
	}
	return a.borrowFreshFrom(ctx, a.reader, nil)
}

func (a *Access) borrowFreshFrom(ctx context.Context, pool *sql.DB, member *physicalReaderMember) (*readerConnection, error) {
	point, ok := ctx.Value(checkpointKey{}).(checkpoint)
	if !ok || point.owner != a {
		return nil, ErrMissingCheckpoint
	}
	fenceCtx, cancel := context.WithTimeout(ctx, a.replayTimeout)
	defer cancel()
	borrowed := time.Now()
	if a.readerPermits != nil {
		select {
		case <-a.readerPermits:
		case <-fenceCtx.Done():
			borrowErr := fenceCtx.Err()
			if ctx.Err() == nil && errors.Is(borrowErr, context.DeadlineExceeded) {
				borrowErr = errors.Join(errReaderPermitTimeout, borrowErr)
			}
			a.observe("reader", StageReaderBorrow, borrowed, borrowErr)
			return nil, privateFailure(failureReaderBorrow, errors.Join(ErrReaderUnavailable, borrowErr))
		}
	}
	conn, err := pool.Conn(fenceCtx)
	a.observe("reader", StageReaderBorrow, borrowed, err)
	if err != nil {
		if a.readerPermits != nil {
			a.readerPermits <- struct{}{}
		}
		return nil, privateFailure(failureReaderBorrow, errors.Join(ErrReaderUnavailable, err))
	}
	if err := a.checkReader(fenceCtx, conn, point, member); err != nil {
		if errors.Is(err, ErrWrongTopology) {
			// sql.Conn.Close returns healthy connections to the pool. This
			// physical connection failed its borrowed-session identity check.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
		if a.readerPermits != nil {
			a.readerPermits <- struct{}{}
		}
		return nil, err
	}
	return &readerConnection{Conn: conn, permits: a.readerPermits, member: member}, nil
}

func (a *Access) checkReader(ctx context.Context, conn *sql.Conn, point checkpoint, member *physicalReaderMember) error {
	started := time.Now()
	var readOnly, systemID, database string
	var recovery bool
	var err error
	var incarnation, serverAddress string
	if member == nil {
		err = conn.QueryRowContext(ctx, `SELECT current_setting('default_transaction_read_only'), pg_is_in_recovery(), system_identifier::text, current_database() FROM pg_control_system()`).Scan(&readOnly, &recovery, &systemID, &database)
	} else {
		err = conn.QueryRowContext(ctx, `SELECT current_setting('default_transaction_read_only'), pg_is_in_recovery(), system_identifier::text, current_database(), (extract(epoch from pg_postmaster_start_time())*1000000)::bigint::text, host(inet_server_addr()) FROM pg_control_system()`).Scan(&readOnly, &recovery, &systemID, &database, &incarnation, &serverAddress)
	}
	if err == nil && (readOnly != "on" || systemID != point.systemID || database != point.database || systemID != a.identity.systemID || database != a.identity.database || recovery == a.samePrimary) {
		err = ErrWrongTopology
	}
	if err == nil && member != nil && (incarnation != member.incarnation || !addressMatches(serverAddress, member.addresses)) {
		err = memberLocalTopology{}
	}
	a.observe("reader", StageReaderIdentity, started, err)
	if err != nil {
		if errors.Is(err, ErrWrongTopology) {
			return privateFailure(failureReaderIdentity, err)
		}
		return privateFailure(failureReaderIdentity, errors.Join(ErrReaderUnavailable, err))
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
			return privateFailure(failureReaderReplay, err)
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
	conn     interface{ Close() error }
	once     sync.Once
	stop     func() bool
	closeErr error
}

var _ db.Rows = (*ownedRows)(nil)

func newOwnedRows(ctx context.Context, rows *sql.Rows, conn interface{ Close() error }) *ownedRows {
	owned := &ownedRows{rows: rows, conn: conn}
	// The callback cannot reference stop: cancellation may run before the
	// AfterFunc return value has been published.
	owned.stop = context.AfterFunc(ctx, func() { _ = owned.finish() })
	return owned
}

func (r *ownedRows) finish() error {
	r.once.Do(func() { r.closeErr = privateFailure(failureReaderRows, errors.Join(r.rows.Close(), r.conn.Close())) })
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
	return privateFailure(failureReaderRows, err)
}

func (r *ownedRows) Err() error {
	err := r.rows.Err()
	if err != nil {
		_ = r.Close()
	}
	return privateFailure(failureReaderRows, err)
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
