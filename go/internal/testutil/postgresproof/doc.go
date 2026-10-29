// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package postgresproof creates isolated PostgreSQL databases for destructive
// live tests.
//
// OpenDisposableDatabase requires an explicit opt-in and a connection to the
// administrative postgres database. It creates a random database from template0
// with a public search path for one test,
// rejects application database DSNs before connecting, and force-drops only
// the generated database during cleanup.
//
// OpenDisposableBootstrapDatabase opens a disposable database the way
// OpenDisposableDatabase does and runs the caller's apply (usually the
// production bootstrap) on it before returning, so migrations with
// public-qualified SECURITY DEFINER bodies install into public. The caller
// passes the administrative DSN and the explicit opt-in; neither is derived.
//
// OpenIsolatedSchema gives one test its own schema in the database its DSN
// names: it installs pg_trgm in public under TrigramExtensionLockKey, creates
// prefix_<unix nanos>, runs the caller's schema setup on a pool whose
// search_path puts that schema first, and drops it when the test ends. It is
// subset-DDL-only since the #9299 lane: a full production bootstrap applies
// public-qualified SECURITY DEFINER bodies that an isolated schema cannot
// host, so full bootstraps must use OpenDisposableBootstrapDatabase. It
// does not validate the DSN or require an opt-in; the live-postgres-readiness
// runner checks the proof DSNs it passes.
// DeferredPartitionProofDSN reads the DSN the deferred-partition and
// activation-obligation proofs share, or skips.
//
// ListSchemaObjects returns the sorted definition-level inventory of one
// schema, shared by the isolated-schema guards so their fingerprints cannot
// drift apart; FirstSchemaObjects caps a failure listing.
//
// RaisePoolForConcurrentClaimers sizes a proof pool so concurrent claim
// loops never serialize behind one connection: one pinned connection per
// claimer plus spare connections for seeding, assertions, and cleanup,
// kept idle at the same count; it returns the limit it set.
package postgresproof
