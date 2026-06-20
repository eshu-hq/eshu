package sandbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// postgresReadOnlyExecutor implements Executor for Postgres using a read-only
// transaction as defense-in-depth. It is constructed by NewPostgresReadOnlyExecutor
// or NewPostgresReadOnlyExecutorWithCostGate.
//
// Security note: this executor operates as the THIRD layer of defense, after:
//  1. normalize: masks comments, strings, and dollar-quotes; rejects control chars.
//  2. Validate: denylist-based keyword check; whole-word matching.
//  3. Read-only transaction (this layer): the database server enforces that no
//     write statement can commit, even if an adversarial query bypassed layers 1–2.
//
// When constructed with a non-zero CostGateConfig (via
// NewPostgresReadOnlyExecutorWithCostGate), this executor also implements Layer
// 3.5 cost gating: EXPLAIN (FORMAT JSON) runs inside the SAME read-only
// transaction that executes the query, bounded by the same caps.Timeout context.
// The cost check is reject-before-execute — the actual query only runs if the
// plan is within budget.
//
// This executor MUST NOT be enabled in production until the security review
// referenced by issues #1755, #1900, and #1902 has been completed and signed off.
// The Guard is DEFAULT-OFF; the read-only transaction is defense-in-depth AFTER
// validation, not a substitute for it.
type postgresReadOnlyExecutor struct {
	db  *sql.DB
	cfg CostGateConfig
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

// NewPostgresReadOnlyExecutorWithCostGate returns an Executor that runs SQL
// queries in a Postgres read-only transaction and enforces the cost gate inline
// within that same transaction. The cfg controls forbidden-operator checks; the
// Caps passed at Exec time supply the MaxPlanCost and MaxEstimatedRows thresholds.
//
// When all limits are zero and cfg has no ForbiddenPlanOperators, the executor
// behaves identically to NewPostgresReadOnlyExecutor — no EXPLAIN is issued.
func NewPostgresReadOnlyExecutorWithCostGate(db *sql.DB, cfg CostGateConfig) Executor {
	return &postgresReadOnlyExecutor{db: db, cfg: cfg}
}

// execWithPlanCheck runs EXPLAIN (FORMAT JSON) and then the query inside the
// SAME read-only Postgres transaction, both bounded by tctx. This is the
// production in-tx cost-gate path.
//
// The method is unexported; CostGateExecutor.Exec detects this interface to
// route the SQL path through in-tx plan checking instead of calling the
// SQLExplainer separately.
func (e *postgresReadOnlyExecutor) execWithPlanCheck(
	ctx context.Context, query string, caps Caps, cfg CostGateConfig,
) (int, error) {
	// Apply the caps timeout once for both EXPLAIN and query execution so
	// neither can run beyond the configured wall-clock limit.
	tctx, cancel := context.WithTimeout(ctx, caps.Timeout)
	defer cancel()

	tx, err := e.db.BeginTx(tctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, err
	}
	// Always roll back: the transaction is read-only so this is a no-op on the
	// database side, but it is required to release the connection back to the pool.
	defer func() { _ = tx.Rollback() }()

	// Layer 3.5 — cost gate runs in the SAME tx/session as query execution.
	// RLS SET LOCAL, search_path, and statement_timeout are all session-local, so
	// the plan we check here is exactly the plan that will execute below.
	if caps.MaxPlanCost > 0 || caps.MaxEstimatedRows > 0 || len(cfg.ForbiddenPlanOperators) > 0 {
		// EXPLAIN (FORMAT JSON) uses the same tctx so a slow or blocked EXPLAIN
		// is cancelled after caps.Timeout, not left to hang indefinitely.
		row := tx.QueryRowContext(tctx, "EXPLAIN (FORMAT JSON) "+query)
		var raw string
		if err = row.Scan(&raw); err != nil {
			// Fail closed: if EXPLAIN cannot run, reject rather than allow.
			return 0, fmt.Errorf("cost gate: plan check failed: %w", err)
		}

		summary, checkErr := parsePlanSummary([]byte(raw), cfg.ForbiddenPlanOperators)
		if checkErr != nil {
			return 0, checkErr
		}
		if summary.ForbiddenOperator != "" {
			return 0, fmt.Errorf("%w: forbidden plan operator %s", ErrPlanBudgetExceeded, summary.ForbiddenOperator)
		}
		if caps.MaxPlanCost > 0 && summary.TotalCost > caps.MaxPlanCost {
			return 0, fmt.Errorf("%w: total cost %.2f exceeds budget %.2f",
				ErrPlanBudgetExceeded, summary.TotalCost, caps.MaxPlanCost)
		}
		if caps.MaxEstimatedRows > 0 && summary.EstimatedRows > caps.MaxEstimatedRows {
			return 0, fmt.Errorf("%w: estimated rows %.0f exceeds budget %.0f",
				ErrPlanBudgetExceeded, summary.EstimatedRows, caps.MaxEstimatedRows)
		}
	}

	// Layer 3 — run the actual query in the same tx.
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

// Exec executes the query against Postgres inside a read-only transaction.
//
// For DialectCypher, Exec returns (0, error) immediately — Cypher execution
// requires a graph backend client that is not wired in v1.
//
// For DialectSQL, Exec:
//  1. Opens a read-only transaction (defense-in-depth after validation).
//  2. Wraps ctx with caps.Timeout so the EXPLAIN and query are cancelled if
//     either runs long (both bounded by the same timeout context).
//  3. If a cost gate is configured (via NewPostgresReadOnlyExecutorWithCostGate
//     or non-zero caps limits), runs EXPLAIN (FORMAT JSON) in the open
//     transaction before executing the query; rejects if over-budget.
//  4. Runs tx.QueryContext and scans up to caps.MaxRows rows.
//  5. Rolls back unconditionally (read-only; no writes to lose).
//
// The returned rowCount is the number of rows actually scanned, which is at
// most caps.MaxRows. If the query would return more rows they are silently
// truncated; the count still reflects what was scanned.
func (e *postgresReadOnlyExecutor) Exec(ctx context.Context, dialect Dialect, query string, caps Caps) (int, error) {
	if dialect == DialectCypher {
		return 0, errors.New("cypher execution is not wired in v1")
	}
	// Always route through execWithPlanCheck, which handles both the cost-gate
	// check (when limits are configured) and the query execution in one tx.
	return e.execWithPlanCheck(ctx, query, caps, e.cfg)
}
