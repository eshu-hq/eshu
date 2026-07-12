// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package graphowner gates canonical graph node writes on the #5007 Postgres
// owner ledger so a shared cross-scope node's scope-derived properties resolve
// deterministically to the max-(observed_at, source_fact_id) contributor,
// independent of commit order or reducer worker count.
//
// NornicDB does not reliably detect concurrent property-write conflicts on a
// shared existing node (#5062), so the graph write alone cannot pick the winner
// deterministically. This package wraps each reducer node-write batch — an
// entire materialization intent's rows, unbounded in size — in the per-uid
// critical section proven safe by
// docs/internal/design/5007-cross-scope-node-ownership.md, processed in
// chunks of at most lockChunkSize (cypher.DefaultBatchSize) distinct uids so
// no single transaction's advisory-lock count can exhaust Postgres's shared
// lock table (#5007 P2-1: an unbounded transaction failed with "out of shared
// memory" at ~20000 uids). Per chunk: open a Postgres transaction, acquire
// that chunk's per-uid advisory locks in one sorted statement, batch-upsert
// the owner ledger (postgres.GraphNodeOwnerStore) keeping the max order key,
// and write to the graph ONLY the uids this chunk currently owns —
// using this chunk's OWN Go-typed rows, never a value round-tripped out of the
// ledger (which would mangle []string/int64 types and break byte-identity for
// non-contended nodes). A chunk that lost a uid to a higher-order-key
// contributor skips that uid's graph write; the winning contributor writes it
// under the same lock, so the final graph node is always the max contributor's
// own row. The transaction commit releases the locks after the graph write.
//
// Gate is family-agnostic; CloudResourceGatedWriter, EC2InstanceGatedWriter, and
// KubernetesWorkloadGatedWriter adapt it to the three reducer node-writer
// consumer interfaces. A nil or ledger-less Gate writes through unchanged, so a
// backend without Postgres keeps prior behavior (cross-scope determinism then
// depends on the ledger being wired, which the Postgres-backed reducer does).
//
// LockOnlyGate is the #5062 P1 companion primitive for writers that are NOT
// order-resolved owner-ledger contributors: the RDS/EC2/S3 posture and
// internet-exposure property writers SET/REMOVE reducer-owned properties on
// the SAME CloudResource nodes Gate resolves ownership for, but every scope
// observes the same posture fact for the same resource, so there is no
// "winner" to converge to. LockOnlyGate acquires the IDENTICAL per-uid
// pg_advisory_xact_lock key Gate uses (postgres.GraphNodeOwnerStore.LockUIDs,
// the same key derivation ResolveOwnedUIDs uses) across the posture writer's
// graph write, with no ledger upsert, so that write can never overlap a
// concurrent Gate-resolved base-property write on the same uid. A nil or
// db-less LockOnlyGate writes through unchanged, matching Gate.
// RDSPostureLockedWriter, EC2InternetExposureLockedWriter,
// EC2BlockDeviceKMSPostureLockedWriter, and S3InternetExposureLockedWriter
// adapt it to the four reducer posture/exposure node-writer consumer
// interfaces; Retract* is forwarded unwrapped (retraction targets a scope, not
// an explicit uid list, so there is nothing to lock ahead of it).
package graphowner
