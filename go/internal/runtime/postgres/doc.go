// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package postgres provides API and MCP runtimes with separate PostgreSQL
// writer and fenced reader pools. Native host candidates share each pool's
// total connection cap; an opt-in direct-member inventory instead splits that
// reader cap across individually qualified physical standby pools. Writer
// candidates must reach one frozen physical
// primary incarnation; reader candidates must be its streaming standbys, or
// the same primary when both DSNs are exactly equal. Startup and every new
// physical connection validate role and identity before use.
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
// This package does not qualify promotion, timeline forks, split brain,
// Aurora, proxy routing, or automatic writer restart acceptance. Recreate
// Access explicitly after a primary restart.
package postgres
