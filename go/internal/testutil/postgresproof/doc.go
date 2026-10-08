// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package postgresproof creates isolated PostgreSQL databases for destructive
// live tests.
//
// OpenDisposableDatabase requires an explicit opt-in and a connection to the
// administrative postgres database. It creates a random database for one test,
// rejects application database DSNs before connecting, and force-drops only
// the generated database during cleanup.
//
// OpenIsolatedSchema gives one test its own schema in the database its DSN
// names: it installs pg_trgm in public under TrigramExtensionLockKey, creates
// prefix_<unix nanos>, runs the caller's schema setup on a pool whose
// search_path puts that schema first, and drops it when the test ends. It
// does not validate the DSN or require an opt-in; the live-postgres-readiness
// runner checks the proof DSNs it passes.
// DeferredPartitionProofDSN reads the DSN the deferred-partition and
// activation-obligation proofs share, or skips.
//
// ListSchemaObjects returns the sorted definition-level inventory of one
// schema, shared by the isolated-schema guards so their fingerprints cannot
// drift apart; FirstSchemaObjects caps a failure listing.
package postgresproof
