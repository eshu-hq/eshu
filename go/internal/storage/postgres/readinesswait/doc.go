// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package readinesswait is the Postgres implementation of
// crossscope.ReadinessWaitLedger (#6785).
//
// The CAN_PERFORM and USES cross-scope edge handlers commit ready edges first
// and then wait, bounded, for missing endpoints. [Store] keeps one row per
// (scope_id, domain) in reducer_readiness_waits (migration 109): the
// first-defer anchor that survives supersession of the per-generation queue
// row, the capped missing set and its fingerprint, the last partial commit,
// and when the missing set settled. Every statement is a single-row
// primary-key read, upsert, or delete; the reducer's (scope, domain) claim
// fence means at most one live worker writes a key.
package readinesswait
