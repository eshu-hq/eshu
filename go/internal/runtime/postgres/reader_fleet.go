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
	"github.com/jackc/pgx/v5/pgconn"
)

// memberLocalTopology distinguishes a replaced physical member from a
// writer/checkpoint or shared configuration mismatch.
type memberLocalTopology struct{}

func (memberLocalTopology) Error() string { return "PostgreSQL physical reader member changed" }
func (memberLocalTopology) Unwrap() error { return ErrWrongTopology }

// snapshotSetupTimeout retains the original operation and cleanup errors while
// classifying a proved setup deadline as retryable on another member.
type snapshotSetupTimeout struct{ cause error }

func (e snapshotSetupTimeout) Error() string   { return context.DeadlineExceeded.Error() }
func (e snapshotSetupTimeout) Unwrap() []error { return []error{context.DeadlineExceeded, e.cause} }

func fleetFailureClass(err error) readerFailureKind {
	if err == nil {
		return readerFailureNeutral
	}
	if _, ok := err.(snapshotSetupTimeout); ok {
		return readerFailureTransient
	}
	if _, ok := err.(memberLocalTopology); ok {
		return readerFailureNeutral
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		kind := readerFailureNeutral
		for _, cause := range joined.Unwrap() {
			child := fleetFailureClass(cause)
			if child == readerFailureFatal {
				return child
			}
			if child == readerFailureCanceled {
				kind = child
			} else if child == readerFailureTransient && kind == readerFailureNeutral {
				kind = child
			}
		}
		return kind
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return fleetFailureClass(wrapped.Unwrap())
	}
	return readerFailureClass(err)
}

func expectedCanceledSetupCleanup(err error) bool {
	if err == nil {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if !expectedCanceledSetupCleanup(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return expectedCanceledSetupCleanup(wrapped.Unwrap())
	}
	return errors.Is(err, sql.ErrTxDone) || errors.Is(err, sql.ErrConnDone) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || pgconn.SafeToRetry(err)
}

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
	if errors.Is(err, ErrMissingCheckpoint) {
		return true
	}
	var local memberLocalTopology
	if errors.Is(err, ErrWrongTopology) {
		return !errors.As(err, &local) || fleetFailureClass(err) == readerFailureFatal
	}
	return !retryableFleetFailure(parent, err)
}

func retryableFleetFailure(parent context.Context, err error) bool {
	return parent.Err() == nil && fleetFailureClass(err) == readerFailureTransient
}

// fleetAttemptContext shares the remaining replay window across eligible
// members without extending the caller's deadline after a failed attempt.
func fleetAttemptContext(parent context.Context, eligible int) (context.Context, context.CancelFunc) {
	if eligible > 1 {
		deadline, ok := parent.Deadline()
		if !ok {
			return context.WithCancel(parent)
		}
		return context.WithTimeout(parent, time.Until(deadline)/time.Duration(eligible))
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
		if bounded.Err() != nil {
			reservation.Release()
			break
		}
		tryCtx, stop := fleetAttemptContext(bounded, len(eligible))
		if tryCtx.Err() != nil {
			failures = errors.Join(failures, ErrReaderUnavailable, tryCtx.Err())
			stop()
			reservation.Release()
			tried[reservation.member] = true
			continue
		}
		result, attemptErr := attempt(tryCtx, ctx, reservation, point)
		stop()
		access.observeMemberAttempt(access.readerMembers[reservation.member].ordinal, attemptErr)
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
	txCtx, cancelTx, detachSetup := bridgeSnapshotSetup(setupCtx, ownerCtx)
	connections := make([]*readerConnection, 0, count)
	transactions := make([]*readTransaction, 0, count)
	ready := false
	defer func() {
		if ready {
			return
		}
		cancelTx(setupCtx.Err())
		detachSetup()
		attemptErr := err
		var cleanupErr error
		for i := len(transactions) - 1; i >= 0; i-- {
			cleanupErr = errors.Join(cleanupErr, transactions[i].Rollback())
		}
		for i := len(transactions); i < len(connections); i++ {
			cleanupErr = errors.Join(cleanupErr, connections[i].Close())
		}
		if ownerCtx.Err() == nil && errors.Is(setupCtx.Err(), context.DeadlineExceeded) && errors.Is(context.Cause(txCtx), context.DeadlineExceeded) &&
			(fleetFailureClass(attemptErr) == readerFailureCanceled || fleetFailureClass(attemptErr) == readerFailureTransient) && expectedCanceledSetupCleanup(cleanupErr) {
			err = snapshotSetupTimeout{cause: errors.Join(attemptErr, cleanupErr)}
		} else {
			err = errors.Join(attemptErr, cleanupErr)
		}
	}()
	for range count {
		conn, borrowErr := a.borrowReserved(setupCtx, point, reservation)
		if borrowErr != nil {
			return nil, borrowErr
		}
		connections = append(connections, conn)
	}
	exporter, err := beginReadTransactionOwned(txCtx, connections[0], a)
	if err != nil {
		return nil, err
	}
	transactions = append(transactions, exporter)
	var snapshotID string
	if err := exporter.queryControlRowContext(setupCtx, "SELECT pg_export_snapshot()").Scan(&snapshotID); err != nil {
		return nil, err
	}
	literal, err := snapshotSQLLiteral(snapshotID)
	if err != nil {
		return nil, err
	}
	for i := 1; i < count; i++ {
		worker, beginErr := beginReadTransactionOwned(txCtx, connections[i], a)
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
	detachSetup()
	if err := setupCtx.Err(); err != nil {
		return nil, err
	}
	if err := txCtx.Err(); err != nil {
		return nil, err
	}
	ready = true
	return &readSnapshotSet{readers: transactions, cancel: cancelTx}, nil
}

// bridgeSnapshotSetup bounds database/sql's transaction lifetime during setup
// without letting a successful set inherit the short attempt deadline.
func bridgeSnapshotSetup(setupCtx, ownerCtx context.Context) (context.Context, context.CancelCauseFunc, func()) {
	txCtx, cancelTx := context.WithCancelCause(ownerCtx)
	done := make(chan struct{})
	stop := context.AfterFunc(setupCtx, func() {
		cancelTx(setupCtx.Err())
		close(done)
	})
	var once sync.Once
	detach := func() {
		once.Do(func() {
			if !stop() {
				<-done
			}
		})
	}
	return txCtx, cancelTx, detach
}

func beginReadTransactionOwned(txCtx context.Context, conn *readerConnection, access *Access) (*readTransaction, error) {
	identity := readerBackendIdentity{}
	if access.readerQueryStartObserver(txCtx) != nil {
		identity = captureReaderBackendIdentity(conn.Conn)
	}
	started := time.Now()
	tx, err := conn.BeginTx(txCtx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	access.observe("reader", StageBusinessQuery, started, err)
	if err != nil {
		return nil, err
	}
	owned := newReadTransaction(txCtx, tx, conn, access)
	owned.identity = identity
	return owned, nil
}
