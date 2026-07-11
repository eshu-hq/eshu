// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package graphowner gates canonical graph node writes on the #5007 Postgres
// owner ledger so a shared cross-scope node's scope-derived properties resolve
// deterministically to the max-(observed_at, source_fact_id) contributor,
// independent of commit order or reducer worker count.
//
// NornicDB does not reliably detect concurrent property-write conflicts on a
// shared existing node (#5062), so the graph write alone cannot pick the winner
// deterministically. This package wraps each reducer node-write batch in the
// per-uid critical section proven safe by
// docs/internal/design/5007-cross-scope-node-ownership.md: open a Postgres
// transaction, acquire all per-uid advisory locks in one sorted statement,
// batch-upsert the owner ledger (postgres.GraphNodeOwnerStore) keeping the max
// order key, and write to the graph ONLY the uids this batch currently owns —
// using this batch's OWN Go-typed rows, never a value round-tripped out of the
// ledger (which would mangle []string/int64 types and break byte-identity for
// non-contended nodes). A batch that lost a uid to a higher-order-key
// contributor skips that uid's graph write; the winning contributor writes it
// under the same lock, so the final graph node is always the max contributor's
// own row. The transaction commit releases the locks after the graph write.
//
// Gate is family-agnostic; CloudResourceGatedWriter, EC2InstanceGatedWriter, and
// KubernetesWorkloadGatedWriter adapt it to the three reducer node-writer
// consumer interfaces. A nil or ledger-less Gate writes through unchanged, so a
// backend without Postgres keeps prior behavior (cross-scope determinism then
// depends on the ledger being wired, which the Postgres-backed reducer does).
package graphowner
