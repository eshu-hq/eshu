// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import "context"

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

// ReadStore combines guarded cursor, row, and snapshot reads.
type ReadStore interface {
	Queryer
	RowQueryer
	ReadSnapshotBeginner
}
