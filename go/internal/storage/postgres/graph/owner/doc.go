// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ownerstore is the Postgres-atomic owner ledger for #5007
// cross-scope same-uid graph node ownership.
//
// GraphNodeOwnerStore (migration 056_graph_node_owner.sql) resolves which
// contributor currently owns a canonical graph node uid when two ingestion
// scopes project the same resource identity and race to write its
// scope-derived properties: NornicDB does not reliably detect concurrent
// property-write conflicts on a shared existing node (#5062), so the graph
// write alone cannot pick a deterministic winner. ResolveOwnedUIDs runs the
// per-uid critical section against a caller-supplied transaction: it
// acquires every entry's per-uid transaction-scoped advisory lock in one
// sorted, deadlock-free statement, batch-upserts the graph_node_owner ledger
// keeping the max (observed_at, source_fact_id) order key, reads the winning
// order key back, and returns the uid set the batch currently owns plus a
// contended-lost count. The caller (go/internal/graphowner) writes only the
// owned rows to the graph and commits to release the locks. LockUIDs is the
// #5062 lock-only companion for writers that are not order-resolved
// contributors (RDS/EC2/S3 posture and internet-exposure property writers):
// it acquires the IDENTICAL advisory lock keyspace with no ledger upsert, so
// their graph write can never overlap a concurrent ResolveOwnedUIDs critical
// section on the same uid. ReleaseOwnedUIDs deletes ledger rows so a later
// contribution is not permanently blocked by a stale winner after the
// #6887 generation-diff retract proves every uid dead.
//
// GraphNodeOwnerBackfillStore (migration
// 074_graph_node_owner_backfill_state.sql) is the one-time upgrade seam: it
// copies existing CloudResource graph rows into the ledger in bounded,
// transactional chunks using the same sorted per-uid locks and monotonic
// max-upsert ResolveOwnedUIDs uses, keyed to a minimum order key
// (GraphNodeOwnerBackfillMinimumOrderKeyPrefix) so it can seed an empty
// ledger but never displace a real reducer contribution. The completion
// marker is idempotent and is written only after every graph page commits.
//
// GraphNodeOwnerSchemaSQL, GraphNodeOwnerUpsertSuffix,
// GraphNodeOwnerAcquireLocksSQL, GraphNodeOwnerAdvisoryKey, and
// DedupeOwnerEntries are exported even though every current caller lives in
// this package: the root #6693 SPLIT test
// (postgres.graph_node_owner_store_test.go) still asserts against them
// directly because it also compares against root-private
// packageRegistryIdentityAdvisoryLockKey, and cannot fully move into this
// package until that root-private lock-namespace family gets its own
// #6693 leaf. Production code outside this package must not depend on them.
//
// This package must not import the parent postgres package.
package ownerstore
