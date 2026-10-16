// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package poison detects and bounds-recovers the dead-letter/poison class in
// fact_work_items: rows with status 'dead_letter' whose scope has no
// strictly-newer scope_generations row (#4740).
//
// Store.CountDeadLetters is a read-only aggregate over the dead_letter
// subset; Store.RecoverDeadLetters re-enqueues a bounded batch to pending,
// gated by a per-item poison_recovery_attempts budget carried in the JSONB
// payload (no schema column). The recovery UPDATE re-verifies
// status = 'dead_letter' at write time, so a row a concurrent worker reclaims
// between the candidate snapshot and the row lock is skipped, never
// clobbered. The default operational posture is surface-only; bounded
// auto-retry runs only when the reducer operator opts in
// (ESHU_POISON_LIVENESS_AUTO_RETRY_ENABLED, wired in cmd/reducer).
package poison
