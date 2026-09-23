// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package fake provides a shared, reusable db.ExecQueryer test double for
// go/internal/storage/postgres and its subpackages.
//
// Every postgres subpackage needs a stand-in for the narrow db.ExecQueryer /
// db.ReadOnlyRepeatableReadBeginner contract so its tests can assert on the
// SQL and arguments a code path issues, and stage canned responses without a
// live database. Before this package existed, that fake lived as an
// unexported type duplicated per test file (root's fakeExecQueryer,
// go/internal/storage/postgres/work_queue_lifecycle_test.go), because Go test
// files cannot be imported across packages. Package fake breaks that
// duplication: it is ordinary, non-test code, so any package under
// go/internal/storage/postgres can import it from its own tests.
//
// ExecQueryer carries no built-in knowledge of any Eshu query string. Its
// predecessor routed specific queries (deferred-backfill scans, workflow
// coordinator status reads, and others) by matching the caller package's
// private SQL constants — logic ExecQueryer deliberately does not
// reproduce, because a shared fake cannot see another package's private
// constants and should not accumulate every caller's domain knowledge
// anyway. Instead, ExecQueryer exposes an injectable Routes slice: each
// caller stages the query-shape-to-response logic its own tests need, and
// ExecQueryer applies it before falling back to a FIFO QueryResponses queue.
//
// ExecQueryer, Rows, and Result are safe for concurrent use, matching the
// concurrency contract of the storage adapters they stand in for (see
// go/internal/storage/postgres/content_writer_batch.go, whose parallel
// upsert batches issue concurrent ExecContext calls against the same fake).
package fake
