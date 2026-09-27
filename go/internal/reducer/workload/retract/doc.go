// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package retract removes workload materialization's stale repository edges
// (#7285).
//
// The canonical projector used to DETACH DELETE the Repository node on every
// non-delta attempt, which wiped every edge on it: stale ones, and also every
// reducer and cross-scope edge a retry never rebuilds. The projector now MERGEs
// the node in place, so the owning domain retracts its own stale edges.
// [RepositoryEdges] deletes the DEFINES and repository-side EXPOSES_ENDPOINT
// edges carrying one evidence source whose target is not in the repository's
// [KeepList]. [FullGenerationRepositoryIDs] restricts that to repositories
// whose generation is a full snapshot, never a delta.
//
// With a [Reader] wired, [RepositoryEdges] first reads each repository's
// current targets and deletes only the stale ones, by id, so a steady-state run
// sends no DELETE: on NornicDB a zero-row relationship DELETE costs
// proportional to store size (NornicDB#296). With no reader, or when the read
// fails, it fails toward deleting and runs the keep-list statements
// unconditionally; a skipped retract would leave stale edges permanently.
//
// Delete counts come from the backend write summary when the executor chain
// implements [CountingExecutor]. [Observe] records them on
// eshu_dp_reconciliation_drift_retractions_total under the bounded
// domain [MetricDomain] and logs every run.
package retract
