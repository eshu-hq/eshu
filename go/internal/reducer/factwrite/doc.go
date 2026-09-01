// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package factwrite holds the reducer's batched fact-write path: the row shapes,
// the INSERT statements, chunking, and the per-fact-ID deduplication applied
// before a batch reaches Postgres.
//
// Deduplication is last-write-wins by fact ID within a batch. Two rows carrying
// the same fact ID in one call would otherwise collide on the ON CONFLICT
// target and fail the whole chunk, so the writer keeps the last occurrence and
// drops the earlier ones. That is a correctness requirement, not an
// optimization.
//
// Chunking bounds the statement size: a batch is split into runs of BatchSize
// rows so a large generation cannot build a single statement past what the
// driver and server will accept.
//
// Now and CollectorKind are the two small primitives every fact writer needs
// before it can build a row: the UTC-normalized write timestamp and the
// normalized collector-kind column value. They live here rather than beside any
// one writer because a domain family that writes facts needs them without
// importing the reducer root.
//
// Execer is the minimal database surface these writers need, so a caller can
// pass a pool, a connection, or a transaction, and a test can substitute a
// recorder without a live database.
package factwrite
