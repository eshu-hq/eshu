// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package postgres provides API and MCP runtimes with separate PostgreSQL
// writer and fenced reader pools. Native host candidates share each pool's
// total connection cap. Writer candidates must reach one frozen physical
// primary incarnation; reader candidates must be its streaming standbys, or
// the same primary when both DSNs are exactly equal. Startup and every new
// physical connection validate role and identity before use.
//
// After caller authorization, ContextWithCheckpoint captures a writer WAL
// insertion point. Each cursor or row read checks freshness on its own
// borrowed reader connection. BeginReadOnlySnapshot checks freshness before
// starting a read-only repeatable-read transaction that retains that same
// connection until Commit, Rollback, or cancellation. No failed fence falls
// back to the writer or executes business SQL.
//
// This package does not qualify promotion, timeline forks, split brain,
// Aurora, proxy routing, or automatic writer restart acceptance. Recreate
// Access explicitly after a primary restart.
package postgres
