// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package summary stores and reads the reducer-owned status summary read model
// in status_summary_snapshots (#7009).
//
// The table holds one row per model key. The row's rows payload is the whole
// result of a status statement, encoded as
// [[section, ordinal, section_json_text], ...] in live order (see Entry and
// EncodeEntries). The tuples are shaped to feed the production decoder
// unchanged; the status reader slice proves the round trip through it. The writer replaces the whole row with Upsert, one guarded
// single-row statement: the conflict branch applies only when the stored as_of
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
// The package depends only on the storage db contracts, never on the parent
// postgres package, so the status store can import it without a cycle.
package summary
