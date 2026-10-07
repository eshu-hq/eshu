// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package summary runs the reducer-owned periodic writer of the status
// summary read model (#7009).
//
// The status routes' active-work statement reads tens of thousands of buffers
// on a deep backlog. Runner computes it on a fixed cadence and stores the
// whole result as one row in status_summary_snapshots, so a reader can serve
// one primary-key lookup instead. Every Interval (10 s by default, never less
// than MinInterval, 5 s) a pass runs one READ COMMITTED transaction on the
// primary, pinned whatever the cluster default: SET LOCAL jit = off, the
// transaction-scoped advisory try-lock (store.WriterLockKey), the database
// clock as as_of plus a check that the table exists, the statement with as_of
// as $1, and one guarded single-row upsert (store.Upsert).
//
// Any number of reducer replicas may run the loop. The lock makes exactly one
// compute per tick and the others report OutcomeSkippedLock; a crashed holder
// frees the lock with its backend. The upsert's as_of guard keeps an older
// pass from overwriting a newer row. Passes never overlap or queue: the next
// pass starts on the first interval boundary after the previous one ends, and
// a pass longer than the interval is counted as an overrun. A pass is bounded
// by a deadline of two intervals. A missing table (the migration has not
// applied yet) is a skip, an error is counted and retried on the next tick,
// and only an invalid configuration makes Run return an error.
//
// The statement comes from the storage package through Statement, so this
// package holds no status SQL; only the transaction control statements live
// here. The runner owns its telemetry: the
// eshu_dp_status_summary_writer_* metrics, the reducer.status_summary.pass
// span, and structured logs.
package summary
