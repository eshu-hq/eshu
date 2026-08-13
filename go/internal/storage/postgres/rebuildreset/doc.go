// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rebuildreset clears the Postgres dedup state that would otherwise stop
// a graph rebuild-from-facts at source-local structure (#4594).
//
// Eshu's graph is a projection: throw it away, keep Postgres, and a refinalize
// replays every active generation to rebuild it. That worked for source-local
// structure and stopped there, because three pieces of Postgres state outlive a
// graph wipe and each one tells the pipeline the work is already done.
//
//  1. Succeeded reducer work items. A re-projection re-derives every reducer
//     intent, but the enqueue is ON CONFLICT (work_item_id) DO NOTHING and the
//     ids are stable, so each one collides with its succeeded row and is dropped.
//     The projector's intent_enqueue log line reports enqueued_count 0 and the
//     domain never runs again.
//  2. Shared projection intents with completed_at set. The partition workers
//     drain only completed_at IS NULL, and the upsert's COALESCE deliberately
//     refuses to reopen a completed row.
//  3. Graph projection phase rows, which assert that canonical nodes are
//     committed. After a wipe that assertion is false, and it is the worst kind
//     of false: the edge Cypher is MATCH-only, so work admitted on a stale
//     readiness answer matches nothing, writes nothing, and still acks succeeded.
//
// Both dedup guards stay exactly as they are. They are correct for ordinary
// operation, where every shard drain, reopen, and retry depends on completed
// work staying completed. What this package adds is a reset scoped to the
// generations a refinalize is actually rebuilding, issued once by the recovery
// path, in the same transaction as the projector re-enqueue.
//
// Apply is the entry point; Counts reports what it cleared. ScopePredicate is
// exported because the caller's projector re-enqueue renders the same predicate,
// so the enqueue and the resets cannot select different scopes.
//
// Why the reducer rows are DELETED rather than reset to pending: a pending row
// is claimable immediately, before the projector re-run that owns its inputs has
// committed anything. In ordinary operation a reducer intent exists only because
// the projector already emitted it, after the canonical node write; resetting to
// pending breaks that causality and lets a handler write into a wiped graph and
// ack succeeded, which is the same silent-incompleteness defect this package
// exists to fix. Deleting restores first-ingest semantics: the work exists again
// only once its producer has run. It also avoids a second trap --
// reset-to-pending violates
// fact_work_items_container_image_identity_v2_status_check, which ties status to
// container_image_identity_v2_authorized_status, so a blind status rewrite fails
// on exactly the rows that carry that coupled column family.
package rebuildreset
