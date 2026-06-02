// Package storageeval defines evidence contracts for storage migration gates.
//
// The package is intentionally pure: it does not open Postgres, call NornicDB,
// write graph state, expose API or MCP routes, enqueue reducer work, or decide
// canonical truth. It validates comparison records that future shadow readers
// produce while Postgres remains the production owner.
//
// Shadow-read comparisons are passing evidence only when a bounded Postgres
// baseline and bounded NornicDB shadow result agree on digest, freshness, truth
// label, capability, and fallback behavior. Shadow-write comparisons are
// passing evidence only when the Postgres fact ledger and NornicDB shadow fact
// writer agree on stable fact identity, idempotency key, active generation,
// schema version, tombstone state, digest, and rollback behavior. Missing,
// stale, divergent, truncated, unsupported, or fallback-truth shadow output is
// rejected so storage migration work cannot silently promote incomplete read
// models or fact families.
package storageeval
