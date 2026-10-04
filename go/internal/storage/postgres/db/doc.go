// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package db holds the shared database contracts for the Postgres storage
// layer: the row cursor (Rows), the read and write adapter surfaces (Queryer,
// Executor, ExecQueryer), and the transaction surface (Transaction, Beginner,
// ReadOnlyRepeatableReadBeginner), and the additive guarded read surfaces
// (Row, RowQueryer, ReadTransaction, ReadSnapshotBeginner, ReadSnapshotSet,
// ReadSnapshotSetBeginner, ReadStore).
//
// ReadTransaction has no Exec or raw transaction method. A guarded runtime
// implementation owns the connection and snapshot lifecycle behind this leaf.
//
// The package is a dependency leaf on purpose. It imports only the Go
// standard library, so domain stores can depend on these contracts without
// importing the postgres root package. If a store moved to a subpackage while
// these types stayed in root, the store would import root; once root also
// references that store, that is an import cycle. Hoisting the contracts here
// first removes that risk for every later domain move under #6693.
//
// The leaf also holds three shared statement-argument builders (CleanIDs,
// IDPlaceholders, IDArgs): pure stdlib string/args shaping with no SQL text
// and no I/O, hoisted byte-identically from the webhook trigger store under
// #6693 so the webhook and incident families share one implementation.
//
// Two InstrumentedDB plumbing pieces hoisted here under #6693's
// prerequisite-hoists table: SearchIndexTermCopyUnsupportedError, the typed
// driver-capability error for the PostgreSQL COPY protocol; and
// WithQuerySummary / QuerySummaryFromContext, the bounded read-label context
// plumbing InstrumentedDB and StatusStore use to attribute a labeled read on
// the postgres.query span.
//
// ErrReaderStale, ErrReaderUnavailable, and ReaderRetryAfterSeconds are the
// shared guarded-reader failure identities and retry hint: runtime/postgres
// returns the errors and the query layer maps a stale replica, or an
// unavailable reader that also hit a deadline (a pool-wait, dial, or
// identity-check timeout inside the replay window), to a retryable 503 without importing each other (#7523).
//
// ReaderStage, StageTimings, WithStageTimings, and StageTimingsFrom are the
// per-request accounting of guarded-reader stage time (#7545): the query layer
// attaches an accumulator to the context of one read, runtime/postgres adds the
// borrow, identity, replay, and business-query durations it observes, and the
// caller reads the sums afterward. The values are sums, so a caller that runs
// reader operations concurrently inside one accumulator scope can see the sum of
// stages exceed wall time; the impact-findings route runs its reads one after
// another, so it cannot. Each stage carries a SUM and an observation
// count because a stage can run more than once per request. The accumulator is
// two atomic counters per stage with no lock and no growth, and it carries no
// SQL text, error text, or identifier.
//
// The concrete adapters (SQLDB, SQLTx, SQLQueryer), the schema bootstrap and
// migration ledger, and the advisory-lock machinery stay in the root package
// with the types they guard. SQLDB.withSchemaBootstrapLock satisfies the
// package-private schemaBootstrapLocker contract asserted by
// applyBootstrapDefinitions, so moving SQLDB without the lock implementation
// would silently degrade bootstrap to unlocked DDL; the lock implementation
// in turn shares unexported helpers and ledger types with root tests. See
// docs/internal/design/storage-collector-tree.md for the recorded trap.
package db
