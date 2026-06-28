// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package storageeval defines evidence contracts for storage migration gates.
//
// The package is intentionally pure: it does not open Postgres, call NornicDB,
// write graph state, expose API or MCP routes, enqueue reducer work, or decide
// canonical truth. It validates comparison records that future proof runners
// produce while Postgres remains the production owner.
//
// Shadow-read comparisons are passing evidence only when a bounded Postgres
// baseline and bounded NornicDB shadow result agree on digest, freshness, truth
// label, capability, and fallback behavior. Shadow-write comparisons are
// passing evidence only when the Postgres fact ledger and NornicDB shadow fact
// writer agree on stable fact identity, idempotency key, active generation,
// schema version, tombstone state, digest, and rollback behavior. Queue
// substrate decisions are passing evidence only when they separate queue
// ownership from storage ownership and compare candidates against claim, lease,
// fencing, retry, dead-letter, backpressure, crash-recovery, fair-scheduling,
// proof-scenario, and observability requirements. Backup/restore proofs are
// passing evidence only when a clean restore records NornicDB and Eshu versions,
// artifact integrity, state-class consistency checks, count and digest parity,
// fallback, rollback, required failure scenarios, and restore observability.
// Missing, stale, divergent, truncated, unsupported, graph-only, or
// fallback-truth shadow output is rejected so storage migration work cannot
// silently promote incomplete read models, fact families, durable state classes,
// or queue/workflow substrates. Hosted-growth Postgres proofs are accepted only
// when aggregate relation sizes, read/write latency, reducer queue drain
// behavior, migration and rollback scenarios, stale rows, retry/dead-letter
// rows, active claims, fact-family growth, index bloat, graph-write pressure,
// hot query plans, retention lag, active-generation reads, changed-since
// retained-window semantics, evidence-bound recommendations, and
// operator-facing observability are all covered.
package storageeval
