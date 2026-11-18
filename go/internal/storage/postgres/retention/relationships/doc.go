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
// ([DefaultRowCap]) and a deadline; the first batch always runs. A batch is also
// bounded in rows: each of its two picking deletes takes at most [BatchRows].
//
// Every batch is one transaction in a fixed order, and it never waits for a row
// lock. It sets a 500 ms local lock_timeout (never zero; it bounds only the one
// wait the batch cannot skip, a relation-level lock such as DDL), takes a
// per-generation advisory try lock (class [LockClass]), classifies the ids and
// locks their generation rows FOR UPDATE SKIP LOCKED, then deletes evidence and
// candidates (each statement picks up to [BatchRows] rows of the claimed
// generations FOR UPDATE SKIP LOCKED, re-asserting all three Rule A predicates,
// and deletes exactly those rows), deletes the generation row last, only for a
// generation with no evidence or candidate row left, and settles which claimed
// generations still have a row. If neither picking delete stopped at its row
// limit, another session holds what is left: the generation is counted lock_busy,
// keeps its generation row and is retried by a later pass, and the rest of the
// batch commits. If one did, the generation is only larger than the batch: it
// runs again in the same pass, in its own transaction, until the pass budget ends.
// There is no session lock, no pass-long transaction and no
// global single flight: a second reducer replica skips a generation the first
// holds. A 55P03 aborts the batch, counts every generation of it as lock_timeout
// and the pass goes on; a deadlock or any other error ends the pass visibly.
//
// The lock orders cross the #10820 repository purge: it deletes evidence,
// candidates and the generation row in the order this batch does, but it reaches
// the generation row last while this batch holds it first. A lock timeout cannot
// break that cycle (the purge's own deadlock check can fire first, and it did,
// 700 and 800 ms after the purge queued, in the design that relied on one), so
// the batch is wait-free instead: the only wait edge left is the purge waiting
// for the batch, which ends when the batch commits or rolls back.
//
// This package must not import the parent postgres package.
package relationshipretention
