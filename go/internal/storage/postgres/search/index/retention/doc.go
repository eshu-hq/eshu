// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package indexretention pre-deletes a superseded generation's persisted
// search index rows (eshu_search_index_terms and eshu_search_index_documents)
// in small committed transactions before generation retention prunes the
// generation (#7427). It is the `search/index/retention/` leaf of
// storage/postgres.
//
// Before it, those rows went only through the scope_generations foreign-key
// cascade inside the retention prune transaction, while that transaction
// held the generation's ingestion_scopes row lock. The terms cascade ran on
// the hash-partitioned parent, so its cost followed the whole leaf partition,
// not the generation. Drain removes the rows first, outside any retention
// row lock. Migration 191 (#7427 PR3) then dropped the terms generation
// foreign key, so no terms statement runs under that lock and Drain and
// SweepOrphans are the only removers of a pruned generation's terms; the
// documents and stats generation keys stay.
//
// Drain walks one table's primary key in keyset pages of BatchRows rows, each
// page its own transaction, then re-sweeps from the start of the keyspace
// until a from-start page is empty. Every batch statement re-checks, in its
// own snapshot, that the generation is superseded, not the scope's active
// generation, and has no claimed, running, or retrying work item; an
// ineligible generation loses no row. Every batch and the retention prune
// take the same per-generation advisory lock,
// pg_try_advisory_xact_lock(LockClass, hashtext(generation_id)), and never
// wait for it, so a batch and the cascade never delete one generation's rows
// at once. A batch sets lock_timeout below deadlock_timeout and yields to a
// writer instead of waiting.
//
// Pass drains a retention pass's candidates oldest first under one Budget (a
// row cap and a deadline; the first batch always runs) and records which
// generations drained; the retention store admits only those to its prune
// and calls TryLockGenerations inside its prune transaction before its
// re-lock. Rows deleted by committed batches stay deleted whatever happens
// next; that is safe because superseded is terminal and every reader joins
// the scope's active generation.
//
// TakeCensus and SweepOrphans find and delete orphan rows (#7427 PR2): search
// index rows whose (scope_id, generation_id) pair scope_generations no longer
// holds. The census walks each terms leaf's and the documents table's
// primary-key prefix with a loose index scan; SweepOrphan drains one orphan
// generation with Drain's batches, but each batch deletes only while the pair
// is still missing and the scope's active pointer does not name it. The
// retention runner calls SweepOrphans after its pass, outside any retention
// transaction.
//
// SweepSuperseded removes the index rows of still-existing superseded
// generations whose facts the history hold keeps (#8231): terms and documents
// in committed keyset batches plus the generation's stats row, oldest first,
// each batch re-checking in its own snapshot that the generation row exists,
// is terminal superseded, is not the scope's live active generation, and has
// no live work. A NULL active pointer or a missing generation row deletes
// nothing. The runner calls it after the orphan sweep, at most once per poll
// interval, only when its superseded-sweep policy is enabled (off by
// default), and still outside any retention transaction. Facts are never
// touched: a swept generation's index rebuilds on demand by re-running the
// search-document projection from the held facts, never eagerly.
//
// This package must not import the parent postgres package.
package indexretention
