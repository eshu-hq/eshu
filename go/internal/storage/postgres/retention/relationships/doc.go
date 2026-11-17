// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package relationshipretention removes the relationship evidence, candidates
// and generation rows of generations nothing keeps alive any more, in small
// committed batches (#10823). It is the `retention/relationships/` leaf of
// storage/postgres.
//
// Rule A is the only retention key. A generation's rows are removable when it
// has no scope_generations row, is no scope's ingestion_scopes
// active_generation_id, and has no `active` relationship_generations row. The
// reopen fingerprint reads only resolved rows joined to an active generation
// row, which Rule A never selects, so it is unchanged. resolved_relationships
// rows are not touched here: they stay until their own deleter lands with its
// single registered statement.
//
// [TakeCensus] lists the orphan generations, one read-only statement per table
// under a local statement_timeout, and counts distinct generations per table
// for the orphan gauge; the count saturates at [CensusListLimit]. [Sweep] runs
// the census and, only when [Options.Deletes] holds (off by default), deletes
// the listed generations in batches of [BatchGenerations] under a row cap
// ([DefaultRowCap]) and a deadline; the first batch always runs.
//
// Every batch is one transaction in a fixed order. It sets a 500 ms local
// lock_timeout (below the server's 1 s deadlock_timeout, never zero), takes a
// per-generation advisory try lock (class [LockClass]), classifies the ids and
// locks their generation rows FOR UPDATE SKIP LOCKED, then deletes evidence,
// candidates and the generation row last, each delete taking only the ids
// classification claimed and re-asserting all three Rule A predicates in its
// own WHERE. There is no session lock, no pass-long transaction and no global
// single flight: a second reducer replica skips a generation the first holds.
// A 55P03 aborts the batch, counts every generation of it as lock_timeout and
// the pass goes on; a deadlock or any other error ends the pass visibly.
//
// The one lock-order cycle is against the #10820 repository purge, which
// deletes evidence, candidates and the generation row in the same order but
// reaches the generation row last while this sweep holds it first; the
// sweeper's lock_timeout breaks that cycle, so the purge never sees a 40P01.
//
// This package must not import the parent postgres package.
package relationshipretention
