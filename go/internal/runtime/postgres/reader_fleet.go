// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/jackc/pgx/v5/pgconn"
)

const fleetAttemptQuantum = 100 * time.Millisecond

// memberLocalTopology distinguishes a replaced physical member from a
// writer/checkpoint or shared configuration mismatch.
type memberLocalTopology struct{}

func (memberLocalTopology) Error() string { return "PostgreSQL physical reader member changed" }
func (memberLocalTopology) Unwrap() error { return ErrWrongTopology }

func fleetCheckpoint(ctx context.Context, access *Access) (checkpoint, error) {
	point, ok := ctx.Value(checkpointKey{}).(checkpoint)
	if !ok || point.owner != access {
		return checkpoint{}, ErrMissingCheckpoint
	}
	if point.lsn == "" || point.systemID != access.identity.systemID || point.database != access.identity.database || point.incarnation != access.identity.incarnation {
		return checkpoint{}, ErrWrongTopology
	}
	return point, nil
}

func sharedFleetFailure(parent context.Context, err error) bool {
	var local memberLocalTopology
	if errors.As(err, &local) {
		return false
	}
	if errors.Is(err, ErrWrongTopology) || errors.Is(err, ErrMissingCheckpoint) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return len(pgErr.Code) >= 2 && pgErr.Code[:2] == "28" || pgErr.Code == "42501" || pgErr.Code == "3D000"
	}
	return !retryableFleetFailure(parent, err)
}

func retryableFleetFailure(parent context.Context, err error) bool {
	return transientReaderMemberFailure(parent, err) || errors.Is(err, io.EOF)
}

func fleetAttemptContext(parent context.Context, alternative bool) (context.Context, context.CancelFunc) {
	if alternative {
		return context.WithTimeout(parent, fleetAttemptQuantum)
	}
	return context.WithCancel(parent)
}

// runFleet reserves a complete operation on one member, and retries only
// before returning a connection, transaction, or snapshot set to the caller.
func runFleet[T any](access *Access, ctx context.Context, count int, attempt func(context.Context, context.Context, *readerReservation, checkpoint) (T, error)) (T, error) {
	var zero T
	point, err := fleetCheckpoint(ctx, access)
	if err != nil {
		return zero, err
	}
	bounded, cancel := context.WithTimeout(ctx, access.replayTimeout)
	defer cancel()
	order := access.memberOrder(count)
	excluded := make(map[int]bool)
	tried := make(map[int]bool)
	var failures error
	for bounded.Err() == nil {
		eligible := make([]int, 0, len(order))
		for _, member := range order {
			if !excluded[member] && !tried[member] {
				eligible = append(eligible, member)
			}
		}
		if len(eligible) == 0 {
			if len(excluded) == len(order) {
				break
			}
			clear(tried)
			select {
			case <-bounded.Done():
			case <-time.After(20 * time.Millisecond):
			}
			continue
		}
		borrowed := time.Now()
		reservation, reserveErr := access.allocator.reserve(bounded, eligible, count)
		access.observe("reader", StageReaderBorrow, borrowed, reserveErr)
		if reserveErr != nil {
			failures = errors.Join(failures, reserveErr)
			break
		}
		tryCtx, stop := fleetAttemptContext(bounded, len(eligible) > 1)
		result, attemptErr := attempt(tryCtx, ctx, reservation, point)
		stop()
		if attemptErr == nil {
			return result, nil
		}
		reservation.Release()
		failures = errors.Join(failures, attemptErr)
		if sharedFleetFailure(bounded, attemptErr) {
			return zero, attemptErr
		}
		var local memberLocalTopology
		if errors.As(attemptErr, &local) {
			excluded[reservation.member] = true
			continue
		}
		if !retryableFleetFailure(bounded, attemptErr) {
			return zero, attemptErr
		}
		// Try every other member before revisiting a transiently failed one.
		// The next cycle permits a sole lagged member to catch up.
		tried[reservation.member] = true
	}
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	if failures == nil {
		failures = ErrReaderUnavailable
	}
	if errors.Is(bounded.Err(), context.DeadlineExceeded) {
		failures = errors.Join(failures, replayContextError(bounded.Err()))
	}
	return zero, failures
}

func (a *Access) borrowReserved(ctx context.Context, point checkpoint, reservation *readerReservation) (*readerConnection, error) {
	member := &a.readerMembers[reservation.member]
	borrowed := time.Now()
	conn, err := member.pool.Conn(ctx)
	a.observe("reader", StageReaderBorrow, borrowed, err)
	if err != nil {
		reservation.ReleaseOne()
		return nil, privateFailure(failureReaderBorrow, errors.Join(ErrReaderUnavailable, err))
	}
	if err := a.checkReader(ctx, conn, point, member); err != nil {
		if errors.Is(err, ErrWrongTopology) {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
		reservation.ReleaseOne()
		return nil, err
	}
	return &readerConnection{Conn: conn, reservation: reservation, member: member}, nil
}

func (a *Access) borrowFleet(ctx context.Context) (*readerConnection, error) {
	conn, err := runFleet(a, ctx, 1, func(tryCtx, _ context.Context, reservation *readerReservation, point checkpoint) (*readerConnection, error) {
		return a.borrowReserved(tryCtx, point, reservation)
	})
	if err != nil {
		return nil, privateFailure(failureReaderBorrow, err)
	}
	return conn, nil
}

func (a *Access) beginFleetSnapshotSet(ctx context.Context, count int) (db.ReadSnapshotSet, error) {
	set, err := runFleet(a, ctx, count, func(tryCtx, ownerCtx context.Context, reservation *readerReservation, point checkpoint) (db.ReadSnapshotSet, error) {
		return a.beginSnapshotSetReserved(tryCtx, ownerCtx, count, reservation, point)
	})
	if err != nil {
		return nil, privateFailure(failureSnapshotBegin, err)
	}
	return set, nil
}

func (a *Access) beginSnapshotSetReserved(setupCtx, ownerCtx context.Context, count int, reservation *readerReservation, point checkpoint) (set db.ReadSnapshotSet, err error) {
	connections := make([]*readerConnection, 0, count)
	transactions := make([]*readTransaction, 0, count)
	ready := false
	defer func() {
		if ready {
			return
		}
		var cleanupErr error
		for i := len(transactions) - 1; i >= 0; i-- {
			cleanupErr = errors.Join(cleanupErr, transactions[i].Rollback())
		}
		for i := len(transactions); i < len(connections); i++ {
			cleanupErr = errors.Join(cleanupErr, connections[i].Close())
		}
		err = errors.Join(err, cleanupErr)
	}()
	for range count {
		conn, borrowErr := a.borrowReserved(setupCtx, point, reservation)
		if borrowErr != nil {
			return nil, borrowErr
		}
		connections = append(connections, conn)
	}
	exporter, err := beginReadTransactionOwned(setupCtx, ownerCtx, connections[0], a)
	if err != nil {
		return nil, err
	}
	transactions = append(transactions, exporter)
	var snapshotID string
	if err := exporter.QueryRowContext(setupCtx, "SELECT pg_export_snapshot()").Scan(&snapshotID); err != nil {
		return nil, err
	}
	literal, err := snapshotSQLLiteral(snapshotID)
	if err != nil {
		return nil, err
	}
	for i := 1; i < count; i++ {
		worker, beginErr := beginReadTransactionOwned(setupCtx, ownerCtx, connections[i], a)
		if beginErr != nil {
			return nil, beginErr
		}
		transactions = append(transactions, worker)
		started := time.Now()
		_, execErr := worker.tx.ExecContext(setupCtx, "SET TRANSACTION SNAPSHOT "+literal)
		a.observe("reader", StageBusinessQuery, started, execErr)
		if execErr != nil {
			return nil, execErr
		}
	}
	if err := setupCtx.Err(); err != nil {
		return nil, err
	}
	ready = true
	return &readSnapshotSet{readers: transactions}, nil
}

func beginReadTransactionOwned(_ context.Context, ownerCtx context.Context, conn *readerConnection, access *Access) (*readTransaction, error) {
	started := time.Now()
	// database/sql rolls a transaction back when BeginTx's context is
	// canceled. The setup quantum must not own a successfully returned set.
	tx, err := conn.BeginTx(ownerCtx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	access.observe("reader", StageBusinessQuery, started, err)
	if err != nil {
		return nil, err
	}
	return newReadTransaction(ownerCtx, tx, conn, access), nil
}
