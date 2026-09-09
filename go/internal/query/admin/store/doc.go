// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package store holds the Postgres admin store (Issue #6060, lane B): the
// durable work-item, dead-letter, input-invalid-fact, decision, replay-event,
// and backfill reads/writes behind the admin Store port, plus the replay
// idempotency ledger. The port and every row/filter model stay in the parent
// admin package; this package only implements them.
package store
