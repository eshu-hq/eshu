// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package lines owns the bulk-load lifecycle of the
// content_file_secret_lines side table behind the hardcoded-secret
// investigation (#7125).
//
// Migration 131 keeps the table current with statement-level triggers on
// content_files, which costs about 0.6 ms of Postgres time per new file. A bulk
// load (bootstrap-index) cannot absorb that on its critical path, so its
// sessions run DeferredSessionSQL, the triggers skip them, and this package
// brackets the load: BeginDeferral turns readiness off durably before the first
// deferred write, and Finalize rebuilds the table repository by repository
// after the last one and publishes ready. Ready is what the investigation read
// checks; while it is false the read runs the legacy content scan instead of
// answering from an incomplete table.
//
// The readiness row (content_file_secret_lines_state) follows the
// content_substring_index_state precedent and adds an epoch: every
// BeginDeferral increments it and Finalize publishes ready only for the epoch it
// was given, so a finalizer that outlives its epoch (a crashed run, then a
// rerun) can never mark a newer load's skipped writes complete. The epoch alone
// does not stop two loads that overlap between BeginDeferral and Finalize: the
// first to finalize would publish ready over rows the other is still writing
// without derivation. AcquireBulkLoadLock closes that: a session advisory lock
// (5318,1) on a pinned connection, held by bootstrap-index from before
// BeginDeferral until after Finalize, makes a second load wait (bounded) and
// then fail naming the holder. The lock dies with its session, so a killed run
// does not leave it behind.
//
// Finalize is idempotent and restart-safe. Each batch locks its files FOR SHARE
// in primary-key order, deletes exactly those files' rows, re-derives them from
// the locked content, and commits. Steady-state writers keep their triggers and
// run concurrently; the finalizer sets a lock_timeout shorter than the server's
// deadlock_timeout and retries a timed-out batch, so the finalizer, never a
// writer, is the side that gives way and it never forms a wait cycle. A writer
// blocked on a row a batch holds still waits until that batch commits, which is
// the lock, the delete, and the derivation of up to BatchSize files (hundreds of
// milliseconds at 500) and tunable by BatchSize. Each batch pages by a
// non-locking key window and then locks those keys, because a locking range read
// returns a concurrently moved row out of order and would skip files.
//
// DeferredSessionSQL is a session setting, so a transaction-mode pooler can
// carry it from a bootstrap-index connection to another binary's client, whose
// writes would then skip derivation while the state is ready. Run
// bootstrap-index without such a pooler.
package lines
