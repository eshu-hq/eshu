// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package wait is the Postgres implementation of
// crossscope.ReadinessWaitLedger (#6785).
//
// The CAN_PERFORM and USES cross-scope edge handlers commit ready edges first
// and then wait, bounded, for missing endpoints. [Store] keeps one row per
// (scope_id, domain) in reducer_readiness_waits (migration 110): the
// first-defer anchor that survives supersession of the per-generation queue
// row, the capped missing set and its fingerprint, the last partial commit,
// and when the missing set settled. Every statement is a single-row
// primary-key read, upsert, or update; the reducer's (scope, domain) claim
// fence means at most one live worker writes a key, and the row's
// anchor_epoch drops writes from a lease-expired straggler that read the row
// before an anchor reset or a clear. A clear keeps the row as a tombstone so
// its epoch keeps fencing; rows are bounded by two per scope.
package wait
