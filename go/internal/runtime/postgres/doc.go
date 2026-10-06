// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package postgres provides API and MCP runtimes with separate PostgreSQL
// writer and fenced reader pools. Native host candidates share each pool's
// total connection cap; an opt-in direct-member inventory instead splits that
// reader cap across individually qualified physical standby pools. Writer
// candidates must reach the published physical primary identity; reader
// candidates must be its streaming standbys, or the same primary when both
// DSNs are exactly equal. Startup and every new physical connection validate
// role and identity before use. A same-cluster primary restart is
// re-bootstrapped in place: a new postmaster incarnation is published only
// when the system identifier, database, and insert timeline match and the
// flushed WAL position is at or past the highest flushed position this Access
// observed. A timeline or watermark failure latches the Access to
// ErrWrongTopology until the process restarts.
//
// After caller authorization, ContextWithCheckpoint captures a writer WAL
// insertion point. Each cursor or row read checks freshness on its own
// borrowed reader connection. BeginReadOnlySnapshot checks freshness before
// starting a read-only repeatable-read transaction that retains that same
// connection until Commit, Rollback, or cancellation. The optional guarded
// snapshot-set surface is offered with one physical reader host or an explicit
// direct-member inventory. It reserves and fences every connection on one
// selected member before beginning transactions, exports one snapshot, and
// imports it into all other set connections. Fleet reservations account for
// every guarded read against per-member and aggregate caps and protect a
// waiting complete set from later singles on the same member. Fleet setup can
// retry a different member only after the entire failed attempt is released. Native
// multi-host readers without an inventory retain guarded reads and
// single-connection snapshots without advertising snapshot sets because
// exported snapshots are server-local.
// No failed fence falls back to the writer or executes business SQL.
//
// NewSnapshotStatusReader confines each status read to one such snapshot and
// runs SET LOCAL jit = off in it before any status statement, as control SQL
// that adds no business query event. The setting ends with the transaction;
// BeginReadOnlySnapshot itself never changes session settings.
//
// This package does not qualify promotion, timeline forks, split brain,
// Aurora, or proxy routing; a promoted or restored primary is refused by
// design. A restarted direct reader member stays ineligible until the process
// restarts.
package postgres
