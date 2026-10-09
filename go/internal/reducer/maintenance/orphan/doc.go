// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package orphan marks and deletes aged zero-relationship graph nodes
// under a single-owner partition lease.
//
// [Runner] drains eligible sweep batches beside normal reducer intent
// processing, claiming the graph_orphan_sweep lease before sweeping when
// a [PartitionLeaseManager] is wired so concurrent reducer replicas never
// contend on the same static-label Cypher writes. The lease releases
// through a context that survives the cycle's own cancellation, so
// shutdown never strands it for its full TTL; a failed release only
// warns, since the lease expires server-side. [PartitionLeaseManager] is
// a local mirror of the reducer-root contract (issue #6061), satisfied
// structurally by the same concrete lease store.
package orphan
