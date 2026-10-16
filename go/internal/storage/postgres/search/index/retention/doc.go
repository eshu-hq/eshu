// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package indexretention pre-deletes a superseded generation's persisted
// search index rows (eshu_search_index_terms and eshu_search_index_documents)
// in small committed transactions before generation retention prunes the
// generation (#7427). It is the `search/index/retention/` leaf of
// storage/postgres.
//
// Without it those rows go only through the scope_generations foreign-key
// cascade inside the retention prune transaction, while that transaction
// holds the generation's ingestion_scopes row lock. The terms cascade runs on
// the hash-partitioned parent, so its cost follows the whole leaf partition,
// not the generation. Drain removes the rows first, outside any retention
// row lock, so the cascade under the lock finds none. The cascade still runs
// (the foreign keys stay); a later change drops the terms foreign key.
//
// Drain walks one table's primary key in keyset pages of BatchRows rows, each
// page its own transaction, then re-sweeps from the start of the keyspace
// until a from-start page is empty. Every batch statement re-checks, in its
// own snapshot, that the generation is superseded, not the scope's active
// generation, and has no claimed, running, or retrying work item; an
// ineligible generation loses no row. Every batch and the retention prune
// take the same per-generation advisory lock,
// pg_try_advisory_xact_lock(LockClass, hashtext(generation_id)), and never
// wait for it, so a batch and the cascade never delete one generation's rows
// at once. A batch sets lock_timeout below deadlock_timeout and yields to a
// writer instead of waiting.
//
// Pass drains a retention pass's candidates oldest first under one Budget (a
// row cap and a deadline; the first batch always runs) and records which
// generations drained; the retention store admits only those to its prune
// and calls TryLockGenerations inside its prune transaction before its
// re-lock. Rows deleted by committed batches stay deleted whatever happens
// next; that is safe because superseded is terminal and every reader joins
// the scope's active generation.
//
// This package must not import the parent postgres package.
package indexretention
