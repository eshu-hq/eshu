// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

var (
	_ db.ReadStore               = fencedQueryer{}
	_ db.ReadSnapshotSetBeginner = fencedQueryer{}
)

var errSnapshotSetMultiHost = errors.New("snapshot sets require one physical reader host")

// MaxReadConnections reports the largest one-member snapshot reservation.
func (q fencedQueryer) MaxReadConnections() int {
	if q.access == nil || q.access.reader == nil {
		return 0
	}
	if len(q.access.readerMembers) > 0 {
		maximum := 0
		for _, member := range q.access.readerMembers {
			if member.maxOpen > maximum {
				maximum = member.maxOpen
			}
		}
		return maximum
	}
	return q.access.reader.Stats().MaxOpenConnections
}

// BeginReadOnlySnapshotSet opens count read-only repeatable-read transactions
// on one exported snapshot. All connections are fenced before any transaction
// begins, and the exporter remains open until the returned set is closed.
func (q fencedQueryer) BeginReadOnlySnapshotSet(ctx context.Context, count int) (set db.ReadSnapshotSet, err error) {
	a := q.access
	if a != nil && a.readerHasFallbacks {
		return nil, privateFailure(failureSnapshotBegin, errSnapshotSetMultiHost)
	}
	if count < 1 || count > q.MaxReadConnections() {
		return nil, privateFailure(failureSnapshotBegin, errors.New("invalid snapshot reader count"))
	}
	if a != nil && len(a.readerMembers) > 0 {
		return a.beginFleetSnapshotSet(ctx, count)
	}
	if a == nil || a.snapshotSetGate == nil {
		return nil, privateFailure(failureSnapshotBegin, errors.New("snapshot set gate unavailable"))
	}
	select {
	case <-a.snapshotSetGate:
	case <-ctx.Done():
		return nil, privateFailure(failureSnapshotBegin, ctx.Err())
	}
	defer func() { a.snapshotSetGate <- struct{}{} }()

	if len(a.readerMembers) == 0 {
		return a.beginSnapshotSetOn(ctx, count, a.reader, nil)
	}
	var result error
	for _, index := range a.memberOrder(count) {
		member := &a.readerMembers[index]
		set, attemptErr := a.beginSnapshotSetOn(ctx, count, member.pool, member)
		if attemptErr == nil {
			return set, nil
		}
		result = errors.Join(result, attemptErr)
		if ctx.Err() != nil {
			break
		}
	}
	return nil, privateFailure(failureSnapshotBegin, result)
}

func (a *Access) beginSnapshotSetOn(ctx context.Context, count int, pool *sql.DB, member *physicalReaderMember) (set db.ReadSnapshotSet, err error) {
	connections := make([]*readerConnection, 0, count)
	transactions := make([]*readTransaction, 0, count)
	ready := false
	capacityTimeout := false
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
		if cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		} else if capacityTimeout && ctx.Err() == nil {
			err = errors.Join(err, db.ErrSnapshotReservationCapacity)
		}
	}()

	// Keep the gate through the complete reservation so concurrent sets cannot
	// each hold a partial pool reservation while waiting for the other.
	for range count {
		conn, borrowErr := a.borrowFreshFrom(ctx, pool, member)
		if borrowErr != nil {
			capacityTimeout = errors.Is(borrowErr, errReaderPermitTimeout)
			return nil, privateFailure(failureSnapshotBegin, borrowErr)
		}
		connections = append(connections, conn)
	}
	exporter, err := beginReadTransaction(ctx, connections[0], a)
	if err != nil {
		return nil, privateFailure(failureSnapshotBegin, err)
	}
	transactions = append(transactions, exporter)
	var snapshotID string
	if err := exporter.queryControlRowContext(ctx, "SELECT pg_export_snapshot()").Scan(&snapshotID); err != nil {
		return nil, privateFailure(failureSnapshotBegin, err)
	}
	snapshotLiteral, err := snapshotSQLLiteral(snapshotID)
	if err != nil {
		return nil, privateFailure(failureSnapshotBegin, err)
	}
	for i := 1; i < len(connections); i++ {
		worker, err := beginReadTransaction(ctx, connections[i], a)
		if err != nil {
			return nil, privateFailure(failureSnapshotBegin, err)
		}
		transactions = append(transactions, worker)
		started := time.Now()
		_, err = worker.tx.ExecContext(ctx, "SET TRANSACTION SNAPSHOT "+snapshotLiteral)
		a.observe("reader", StageBusinessQuery, started, err)
		if err != nil {
			return nil, privateFailure(failureSnapshotBegin, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, privateFailure(failureSnapshotBegin, err)
	}
	set = &readSnapshotSet{readers: transactions}
	ready = true
	return set, nil
}

func beginReadTransaction(ctx context.Context, conn *readerConnection, access *Access) (*readTransaction, error) {
	identity := readerBackendIdentity{}
	if access.readerQueryStartObserver(ctx) != nil {
		identity = captureReaderBackendIdentity(conn.Conn)
	}
	started := time.Now()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	access.observe("reader", StageBusinessQuery, started, err)
	if err != nil {
		return nil, err
	}
	owned := newReadTransaction(ctx, tx, conn, access)
	owned.identity = identity
	return owned, nil
}

func newReadTransaction(ctx context.Context, tx *sql.Tx, conn interface{ Close() error }, access *Access) *readTransaction {
	owned := &readTransaction{tx: tx, conn: conn, access: access}
	if lease, ok := conn.(*readerConnection); ok && lease.member != nil {
		owned.fleet = true
	}
	owned.stop = context.AfterFunc(ctx, func() { _ = owned.finish(false) })
	return owned
}

func snapshotSQLLiteral(snapshotID string) (string, error) {
	if snapshotID == "" || strings.IndexByte(snapshotID, 0) >= 0 {
		return "", errors.New("invalid exported snapshot identifier")
	}
	return "'" + strings.ReplaceAll(snapshotID, "'", "''") + "'", nil
}

type readSnapshotSet struct {
	mu      sync.Mutex
	once    sync.Once
	readers []*readTransaction
	closed  bool
	err     error
}

var _ db.ReadSnapshotSet = (*readSnapshotSet)(nil)

func (s *readSnapshotSet) Reader(index int) (db.Queryer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, sql.ErrTxDone
	}
	if index < 0 || index >= len(s.readers) {
		return nil, fmt.Errorf("snapshot reader index %d out of range", index)
	}
	return s.readers[index], nil
}

func (s *readSnapshotSet) Close() error {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		readers := append([]*readTransaction(nil), s.readers...)
		s.mu.Unlock()
		// Roll back importers before the exporter so its snapshot remains valid
		// throughout caller assembly and all importer cleanup.
		for i := len(readers) - 1; i >= 0; i-- {
			s.err = errors.Join(s.err, readers[i].finishSnapshotSet())
		}
	})
	return s.err
}

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
		return privateFailure(failureReaderRows, r.err)
	}
	for _, value := range dest {
		if _, raw := value.(*sql.RawBytes); raw {
			return privateFailure(failureRawBytes, errors.Join(errors.New("sql: RawBytes isn't allowed on Row.Scan"), r.rows.Close()))
		}
	}
	if !r.rows.Next() {
		err := r.rows.Err()
		_ = r.rows.Close()
		if err != nil {
			return privateFailure(failureReaderRows, err)
		}
		return sql.ErrNoRows
	}
	if err := r.rows.Scan(dest...); err != nil {
		_ = r.rows.Close()
		return privateFailure(failureReaderRows, err)
	}
	return privateFailure(failureReaderRows, r.rows.Close())
}

// BeginReadOnlySnapshot fences the borrowed connection before starting a
// repeatable-read transaction. The transaction owns the connection thereafter.
func (q fencedQueryer) BeginReadOnlySnapshot(ctx context.Context) (db.ReadTransaction, error) {
	conn, err := q.access.borrowFresh(ctx)
	if err != nil {
		return nil, err
	}
	identity := readerBackendIdentity{}
	if q.access.readerQueryStartObserver(ctx) != nil {
		identity = captureReaderBackendIdentity(conn.Conn)
	}
	started := time.Now()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	q.access.observe("reader", StageBusinessQuery, started, err)
	if err != nil {
		_ = conn.Close()
		return nil, privateFailure(failureSnapshotBegin, err)
	}
	owned := newReadTransaction(ctx, tx, conn, q.access)
	owned.identity = identity
	return owned, nil
}

type readTransaction struct {
	tx       *sql.Tx
	conn     interface{ Close() error }
	access   *Access
	identity readerBackendIdentity
	fleet    bool
	once     sync.Once
	stop     func() bool
	err      error
}

var _ db.ReadTransaction = (*readTransaction)(nil)

func (r *readTransaction) QueryContext(ctx context.Context, statement string, args ...any) (db.Rows, error) {
	r.access.recordReaderQueryStart(ctx, r.identity)
	return r.queryContext(ctx, statement, args...)
}

// queryContext retains query timing and cleanup for business and control SQL.
func (r *readTransaction) queryContext(ctx context.Context, statement string, args ...any) (db.Rows, error) {
	started := time.Now()
	rows, err := r.tx.QueryContext(ctx, statement, args...)
	r.access.observe("reader", StageBusinessQuery, started, err)
	if err != nil {
		return nil, privateFailure(failureReaderQuery, memberQueryFailure(err, r.fleet))
	}
	return &txRows{rows: rows, fleet: r.fleet}, nil
}

func (r *readTransaction) QueryRowContext(ctx context.Context, statement string, args ...any) db.Row {
	rows, err := r.QueryContext(ctx, statement, args...)
	return &fencedRow{rows: rows, err: err}
}

// queryControlRowContext excludes internal snapshot control SQL from business events.
func (r *readTransaction) queryControlRowContext(ctx context.Context, statement string, args ...any) db.Row {
	rows, err := r.queryContext(ctx, statement, args...)
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
		r.err = privateFailure(failureSnapshotTerminal, memberQueryFailure(errors.Join(r.err, r.conn.Close()), r.fleet))
	})
	if !executed {
		if r.err == nil {
			return sql.ErrTxDone
		}
		return privateFailure(failureSnapshotTerminal, errors.Join(sql.ErrTxDone, r.err))
	}
	return r.err
}

func (r *readTransaction) finishSnapshotSet() error {
	r.stop()
	r.once.Do(func() {
		r.err = r.tx.Rollback()
		r.err = privateFailure(failureSnapshotTerminal, memberQueryFailure(errors.Join(r.err, r.conn.Close()), r.fleet))
	})
	return r.err
}

func (r *readTransaction) Commit() error   { r.stop(); return r.finish(true) }
func (r *readTransaction) Rollback() error { r.stop(); return r.finish(false) }

type txRows struct {
	rows  *sql.Rows
	fleet bool
}

func (r *txRows) Next() bool {
	ok := r.rows.Next()
	if !ok {
		_ = r.rows.Close()
	}
	return ok
}

func (r *txRows) Scan(dest ...any) error {
	for _, value := range dest {
		if _, raw := value.(*sql.RawBytes); raw {
			return privateFailure(failureRawBytes, errors.Join(errors.New("sql: RawBytes is unsupported on read-only snapshot rows; use *[]byte"), r.Close()))
		}
	}
	err := r.rows.Scan(dest...)
	if err != nil {
		_ = r.Close()
	}
	return privateFailure(failureReaderRows, memberQueryFailure(err, r.fleet))
}

func (r *txRows) Err() error {
	err := r.rows.Err()
	if err != nil {
		_ = r.rows.Close()
	}
	return privateFailure(failureReaderRows, memberQueryFailure(err, r.fleet))
}

func (r *txRows) Close() error {
	return privateFailure(failureReaderRows, memberQueryFailure(r.rows.Close(), r.fleet))
}
