// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package linksfreshnessstore persists the changed-since link ledger (#7127):
// an activation journal, a per-scope key-state table, and one link (with its
// delta rows and bucket counts) per activation, so a changed-since read can
// be answered from O(changes) rows instead of a two-generation diff.
//
// LinkWriter links one activation per transaction. The transaction takes,
// in order, the scope's cursor row (FOR UPDATE SKIP LOCKED, the per-scope
// writer fence; it never waits), the activating generation (FOR KEY SHARE
// SKIP LOCKED after a plain existence read, which keeps generation retention
// off it), for a full link the state generation it links from (the same
// fence), and one of Slots advisory slots
// (pg_try_advisory_xact_lock(SlotLockClass, slot); it never waits). A miss on
// any of them rolls back and returns a non-counting *RetryError; the cursor
// does not move. The generation locks are bounded, not non-blocking: SKIP
// LOCKED does not cover PostgreSQL's update-chain walk, so they run under a
// 250 ms transaction-local lock_timeout, and a timeout is the non-counting
// generation_lock_timeout. A ledger row is written only while every
// generation it names is locked, so retention's delete of a pruned
// generation's rows is complete.
// A failure once the link statement ran returns a counting *FailureError;
// RecordFailure counts it on the cursor row with backoff and, at the limit,
// turns the activation into a link_poisoned chain break. A full generation links as root (no state yet), incremental
// (state at an earlier generation), or rebase (the state's generation was
// pruned: the state moves by the same diff and a root link is recorded with
// a prior_pruned break) in one statement that reads the activating
// generation once. A delta generation, a pruned generation, or a
// delta whose prior is unknown is a chain break: the cursor advances and the
// state is kept. The overlay link for delta generations is not shipped yet.
//
// JournalStore journals activations the Ack path has not (the sweeper, with
// an unknown prior), backfills retained history for scopes with no journal
// rows (each row inserted through its locked generation row), reads the
// backlog, the ledger size and the orphan probe for the gauges, and removes
// the rows of deleted scopes.
//
// PrunedGenerationRowCounts and DeletePrunedGenerationRows run inside
// generation retention's prune transaction (#7127 ruling 2.8): they count and
// delete the links whose generation or prior generation is pruned, with their
// deltas and bucket counts, and the activation rows of the pruned
// generations. The state table and the cursor are never pruned. The counts
// feed BatchRowLimit, so a batch's ledger deletes are bounded with its other
// deletes.
//
// Every digest is computed in SQL from PayloadDigestInput; Go never hashes a
// payload. The parent postgres package aliases PayloadDigestInput and
// ExcludeReducerDerivedKinds for the changed-since read statement. This
// package must not import the parent postgres package from non-test code.
package linksfreshnessstore
