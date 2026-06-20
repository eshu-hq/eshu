package sandbox

import (
	"context"
	"database/sql"
	"errors"
)

// postgresReadOnlyExecutor implements Executor for Postgres using a read-only
// transaction as defense-in-depth. It is constructed by NewPostgresReadOnlyExecutor.
//
// Security note: this executor operates as the THIRD layer of defense, after:
//  1. normalize: masks comments, strings, and dollar-quotes; rejects control chars.
//  2. Validate: denylist-based keyword check; whole-word matching.
//  3. Read-only transaction (this layer): the database server enforces that no
//     write statement can commit, even if an adversarial query bypassed layers 1–2.
//
// This executor MUST NOT be enabled in production until the security review
// referenced by issues #1755, #1900, and #1902 has been completed and signed off.
// The Guard is DEFAULT-OFF; the read-only transaction is defense-in-depth AFTER
// validation, not a substitute for it.
type postgresReadOnlyExecutor struct {
	db *sql.DB
}

// NewPostgresReadOnlyExecutor returns an Executor that runs SQL queries in a
// Postgres read-only transaction (sql.TxOptions{ReadOnly: true}).
//
// Cypher execution is NOT wired in v1: calling Exec with DialectCypher returns
// an error immediately without touching db.
//
// The db handle may be nil at construction time; the executor will panic only
// when Exec is called with DialectSQL against a nil db.
func NewPostgresReadOnlyExecutor(db *sql.DB) Executor {
	return &postgresReadOnlyExecutor{db: db}
}

// Exec executes the query against Postgres inside a read-only transaction.
//
// For DialectCypher, Exec returns (0, error) immediately — Cypher execution
// requires a graph backend client that is not wired in v1.
//
// For DialectSQL, Exec:
//  1. Opens a read-only transaction (defense-in-depth after validation).
//  2. Wraps ctx with caps.Timeout so the query is cancelled if it runs long.
//  3. Runs tx.QueryContext and scans up to caps.MaxRows rows.
//  4. Rolls back unconditionally (read-only; no writes to lose).
//
// The returned rowCount is the number of rows actually scanned, which is at
// most caps.MaxRows. If the query would return more rows they are silently
// truncated; the count still reflects what was scanned.
func (e *postgresReadOnlyExecutor) Exec(ctx context.Context, dialect Dialect, query string, caps Caps) (int, error) {
	if dialect == DialectCypher {
		return 0, errors.New("cypher execution is not wired in v1")
	}

	tx, err := e.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, err
	}
	// Always roll back: the transaction is read-only so this is a no-op on the
	// database side, but it is required to release the connection back to the pool.
	defer func() { _ = tx.Rollback() }()

	tctx, cancel := context.WithTimeout(ctx, caps.Timeout)
	defer cancel()

	rows, err := tx.QueryContext(tctx, query)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	count := 0
	for rows.Next() && count < caps.MaxRows {
		count++
	}
	if err = rows.Err(); err != nil {
		return count, err
	}
	return count, nil
}
