// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package lines owns the bulk-load lifecycle of the
// content_file_secret_lines side table behind the hardcoded-secret
// investigation (#7125).
//
// Migration 138 keeps the table current with statement-level triggers on
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
// the lock, the delete, and the derivation of up to BatchSize files and up to
// MaxBatchBytes of content (500 files and 4 MiB, at least one file): the cost
// tracks bytes, and 500 large generated sources took 11.3 s. Each batch pages by a
// non-locking key window and then locks those keys, because a locking range read
// returns a concurrently moved row out of order and would skip files.
//
// Repairer is the steady-state path to ready (#8833): the reducer runs it, and
// whenever the state is not ready (a migration that changes the derivation
// function moved it to not_built, or a bootstrap finalize failed or died) it
// takes the run lock (5318,1) and then the deferred-writer lock (5318,2)
// exclusive, both with non-waiting tries on a dedicated one-connection pool,
// re-reads the state, and runs Finalize for the current epoch. The run lock
// comes before the claim because the claim also takes a building row; two
// unlocked finalizers would collide on the side table's primary key. (5318,2) is
// held shared by every deferred-writer session from birth (RunFence.Hook), so the
// Repairer cannot take it while any bootstrap-index connection is alive: the
// pinned (5318,1) session can die while the pool sessions keep writing, and
// (5318,1) alone would then let the Repairer publish ready over underived rows.
// RunFence also refuses a session born after the run's pinned session is gone,
// and the pinned session sends a keep-alive against idle_session_timeout.
//
// DeferredSessionSQL is a session setting, so a transaction-mode pooler can
// carry it from a bootstrap-index connection to another binary's client, whose
// writes would then skip derivation while the state is ready. Run
// bootstrap-index without such a pooler.
package lines
