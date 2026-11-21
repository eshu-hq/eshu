// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package reduceradmission wraps a reducer intent writer with the reducer
// admission gate the ingester and the standalone projector share
// (eshu-hq/eshu#10882 decision 3): the global database-protection
// backstop (total-depth high-water, graph-write-pressure, retrying
// ceiling) plus the per-source slowed leg that defers intents whose
// source is latched behind on the reducer queue.
//
// The gate paces when new work is admitted per source; it never reduces
// workers, batch sizes, or concurrency. A slowed verdict defers (poll,
// heartbeat, bounded wait, retryable timeout) but never drops: every
// intent lands by the final round or the wait fails as a retryable
// admission error. Consult failures fail open: an unavailable signal is
// not evidence of a backlog.
//
// Callers pass their own inner writer and keep deployment-profile
// bypasses; a fully disabled gate returns the inner writer unwrapped.
package reduceradmission
