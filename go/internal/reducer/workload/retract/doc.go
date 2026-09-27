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
// Delete counts come from the backend write summary when the executor chain
// implements [CountingExecutor]. [Observe] records them on
// eshu_dp_reconciliation_drift_retractions_total under the bounded
// domain [MetricDomain] and logs every run.
package retract
