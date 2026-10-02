// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import (
	"context"
	"errors"
)

// ErrSnapshotReservationCapacity means a snapshot set could not reserve its
// guarded reader permits before its internal wait expired. No transaction or
// business query started, and all partial reservations were released.
var ErrSnapshotReservationCapacity = errors.New("snapshot reader reservation capacity unavailable")

// Row scans one result without exposing the underlying SQL connection.
type Row interface {
	Scan(...any) error
}

// RowQueryer reads one row on the same guarded connection as Queryer.
type RowQueryer interface {
	QueryRowContext(context.Context, string, ...any) Row
}

// ReadTransaction shares one read-only repeatable-read snapshot until Commit,
// Rollback, or cancellation. Its snapshot cursor rejects *sql.RawBytes
// destinations before scanning; use *[]byte for copied byte values. It has
// no write or raw transaction surface.
type ReadTransaction interface {
	Queryer
	RowQueryer
	Commit() error
	Rollback() error
}

// ReadSnapshotBeginner opens a guarded read-only repeatable-read transaction.
type ReadSnapshotBeginner interface {
	BeginReadOnlySnapshot(context.Context) (ReadTransaction, error)
}

// ReadSnapshotSet holds multiple query-only readers on one exported snapshot.
// Reader indexes are stable for the lifetime of the set; Close releases every
// transaction and connection owned by it.
type ReadSnapshotSet interface {
	Reader(index int) (Queryer, error)
	Close() error
}

// ReadSnapshotSetBeginner opens a group whose members share one read-only
// repeatable-read snapshot. Count includes the exporting reader.
type ReadSnapshotSetBeginner interface {
	MaxReadConnections() int
	BeginReadOnlySnapshotSet(context.Context, int) (ReadSnapshotSet, error)
}

// ReadStore combines guarded cursor, row, and snapshot reads.
type ReadStore interface {
	Queryer
	RowQueryer
	ReadSnapshotBeginner
}
