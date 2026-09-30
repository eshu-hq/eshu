// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

const (
	// EventPostgresStoreError names the structured log record emitted once per
	// bounded Postgres driver failure (#7253). The client-facing error for the
	// same failure is a fixed per-class string; this record carries the detail.
	EventPostgresStoreError = "postgres.store.error"

	// LogKeyPostgresStoreOperation names the driver call that failed: connect,
	// prepare, query, exec, rows, begin, commit, rollback, ping, reset_session,
	// or close. The set is closed.
	LogKeyPostgresStoreOperation = "postgres_store.operation"
	// LogKeyPostgresStoreSQLState carries the server's SQLSTATE when the failure
	// was a server error, and is empty otherwise.
	LogKeyPostgresStoreSQLState = "postgres_store.sqlstate"
	// LogKeyPostgresStoreStatementHead carries the whitespace-collapsed SQL text
	// truncated to a bounded length. Eshu's store statements bind values with $N
	// placeholders, so it names the statement shape and carries no value.
	LogKeyPostgresStoreStatementHead = "postgres_store.statement_head"
	// LogKeyPostgresStoreError carries the driver's own error text, truncated to
	// a bounded length. It is operator-only: the client-facing error never
	// carries it, and it can name the connection target, a relation, a column, a
	// constraint, or echo a bound value.
	LogKeyPostgresStoreError = "postgres_store.error"
)
