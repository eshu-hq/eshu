// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// This file is the transitional compatibility surface for the reducer fact
// batch writers that moved to [factwrite] (issue #6061). Each entry is deleted
// once its last reducer-root caller has moved into a family subpackage.

// workloadIdentityExecer is the minimal ExecContext surface the batch writers
// need.
type workloadIdentityExecer = factwrite.Execer

// reducerFactRow is one row of a reducer-owned fact batch insert.
type reducerFactRow = factwrite.Row

// reducerFactVersionedRow is one row of a versioned reducer fact batch insert.
type reducerFactVersionedRow = factwrite.VersionedRow

// Batch-insert statement fragments and the chunk size the writers use.
const (
	reducerFactBatchInsertPrefix   = factwrite.BatchInsertPrefix
	reducerFactBatchInsertSource   = factwrite.BatchInsertSource
	reducerFactBatchInsertConflict = factwrite.BatchInsertConflict
	reducerFactBatchInsertQuery    = factwrite.BatchInsertQuery
	reducerFactBatchSize           = factwrite.BatchSize

	reducerFactBatchInsertVersionedQuery = factwrite.BatchInsertVersionedQuery

	// canonicalReducerFactInsertQuery is the canonical single-row upsert every
	// reducer-owned fact writer uses. See [factwrite.SingleInsertQuery].
	canonicalReducerFactInsertQuery = factwrite.SingleInsertQuery
)

// reducerBatchInsertFacts forwards to [factwrite.BatchInsertFacts].
func reducerBatchInsertFacts(ctx context.Context, db workloadIdentityExecer, rows []reducerFactRow) error {
	return factwrite.BatchInsertFacts(ctx, db, rows)
}

// reducerBatchInsertVersionedFacts forwards to
// [factwrite.BatchInsertVersionedFacts].
func reducerBatchInsertVersionedFacts(
	ctx context.Context,
	db workloadIdentityExecer,
	rows []reducerFactVersionedRow,
) error {
	return factwrite.BatchInsertVersionedFacts(ctx, db, rows)
}

// execReducerFactChunk forwards to [factwrite.ExecChunk].
func execReducerFactChunk(ctx context.Context, db workloadIdentityExecer, chunk []reducerFactRow) error {
	return factwrite.ExecChunk(ctx, db, chunk)
}

// reducerFactChunkArgs forwards to [factwrite.ChunkArgs].
func reducerFactChunkArgs(chunk []reducerFactRow) []any {
	return factwrite.ChunkArgs(chunk)
}

// dedupeReducerFactRowsByFactID forwards to [factwrite.DedupeRowsByFactID]. It
// stays generic: a function-valued variable cannot carry a type parameter, and
// a func statement keeps the call inlinable.
func dedupeReducerFactRowsByFactID[T any](rows []T, factID func(T) string) []T {
	return factwrite.DedupeRowsByFactID(rows, factID)
}

// reducerWriterNow forwards to [factwrite.Now].
func reducerWriterNow(now func() time.Time) time.Time {
	return factwrite.Now(now)
}

// reducerFactCollectorKind forwards to [factwrite.CollectorKind].
func reducerFactCollectorKind(sourceSystem string) string {
	return factwrite.CollectorKind(sourceSystem)
}
