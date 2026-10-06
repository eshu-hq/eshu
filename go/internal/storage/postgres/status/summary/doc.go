// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package summary stores and reads the reducer-owned status summary read model
// in status_summary_snapshots (#7009).
//
// The table holds one row per model key. The row's rows payload is the whole
// result of a status statement, encoded as
// [[section, ordinal, section_json_text], ...] in live order (see Entry and
// EncodeEntries). The tuples feed the production decoder unchanged:
// TestActiveWorkSummaryDecodesStoredSummaryRowsUnchanged in the parent postgres
// package proves the round trip through activeWorkSummary.add, and the status
// reader slice proves it against the live read. The writer replaces the whole
// row with Upsert, one guarded single-row statement: the conflict branch applies only when the stored as_of
// is strictly older than the incoming one, so an older pass can never overwrite
// a newer one, a replay with an equal as_of rewrites nothing, and a reader sees
// either the whole previous answer or the whole new one.
//
// Upsert reports whether the row advanced. Read returns one row by model key
// with a primary-key lookup and classifies a missing table (ErrNotInstalled), a
// missing row (ErrNotFound), an undecodable payload (ErrDecode), and a stored
// row_count that disagrees with the payload (ErrRowCountMismatch). The caller
// owns the other fences: schema version, source digest, and age.
//
// WriterLockKey and TryLock give the writer its single-writer-per-tick
// guarantee through a transaction-scoped advisory lock that a crashed holder
// releases with its backend. Store, Reader, and Writer are the small interfaces
// the periodic writer and the status reader use.
//
// Select is the status reader's decision. On the status snapshot transaction it
// reads the database clock and whether the table exists, then the keyed row,
// and serves the row only when its schema version, source digest, row count, and
// decoding hold and its age (database clock minus as_of) is within the configured
// limit. AddAge advances the stored ages by that age. Anything else is a
// SourceLiveFallback with a typed Reason, and the caller runs the live statement
// for the whole answer. Flight shares one live statement per process among
// concurrent fallbacks, LoadReadConfig reads the reader's two environment
// settings, and Observe records the read counter, the served-age histogram, the
// span attributes, and a rate-limited fallback warning.
//
// The package depends only on the storage db contracts, never on the parent
// postgres package, so the status store can import it without a cycle.
package summary
