// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package nornicdb provides NornicDB-specific canonical graph adapters above
// the backend-neutral storage/cypher contracts.
//
// PhaseGroupExecutor commits dependency phases in order, bounds transaction
// size, preserves configured entity-chunk concurrency, and deliberately omits
// cypher.GroupExecutor so callers cannot select the unsupported
// whole-materialization atomic route. Command wiring owns live drivers,
// environment validation, server/client transaction timeouts, retries,
// instrumentation, and the process-wide inner graph-write gate. Non-empty
// writes fail when the inner executor is missing; partial phase commits must be
// idempotent and converge exactly when a caller retries.
package nornicdb
