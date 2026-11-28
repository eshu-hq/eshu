// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// jitOffSQL turns just-in-time compilation off for the rest of the current
// transaction. The third argument is "is local": the setting reverts at COMMIT or
// ROLLBACK and never reaches the session or the pooled connection.
const jitOffSQL = `SELECT set_config('jit', 'off', true)`

// wantsJITOff reports whether the route's one statement runs under a
// transaction-local jit = off. The measured pair (#10880, retired evidence
// record) showed jit = off at least 20 percent faster for the supplement
// reads, in both corpus states: the planner estimates a large
// probe count, JIT compiles the statement, and the compile costs more than the
// probes it speeds up. That pair measured the V statements only; the image route
// takes the wrapper by arbiter ruling with no measured pair of its own (its QA
// median is a B3 return line). Through the Go stores the V saving is per plan state
// (#10880 (retired evidence record)): a custom plan (the first
// five executions of a statement on a connection) is 82 percent faster for the list
// and both counts, and the list's generic plan is 68 percent faster; the counts'
// generic plan costs 2.7 percent less than jit_above_cost on the lab corpus, is not
// compiled, and the wrapper is neutral there (within 1 percent), a state a larger
// corpus can change. The winners and grant routes are unchanged.
func (r ReadRoute) wantsJITOff() bool {
	return r == RouteWinnersSupplement || r == RouteSupportsImage
}

// sqlBeginner is the transaction surface a plain *sql.DB offers; the live tests
// and any caller without the guarded read port read through it.
type sqlBeginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// withJITOff opens a short read-only transaction, sets jit off for it, and
// returns the queryers that run the statement inside it, plus end, which commits
// (and rolls back if the commit fails) so the setting is gone before the caller
// returns. end must run after every row is scanned. reader is the guarded read
// port and database the plain connection; the guarded port wins when both are set.
// Neither being a transaction source (a unit-test fake) runs the statement
// unchanged, so the only code path without the setting is one with no database.
func withJITOff(
	ctx context.Context,
	reader any,
	database any,
	plainRows db.Queryer,
	plainRow db.RowQueryer,
) (db.Queryer, db.RowQueryer, func(), error) {
	if beginner, ok := reader.(db.ReadSnapshotBeginner); ok {
		tx, err := beginner.BeginReadOnlySnapshot(ctx)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("begin jit-off read transaction: %w", err)
		}
		if err := setJITOff(ctx, tx); err != nil {
			_ = tx.Rollback()
			return nil, nil, nil, err
		}
		// end commits a read-only transaction. A failed commit has no data to lose
		// (the statement already returned its rows) and nothing to retry, so it rolls
		// back and is not reported: the read's own error path carries failures.
		return tx, tx, func() {
			if err := tx.Commit(); err != nil {
				_ = tx.Rollback()
			}
		}, nil
	}
	if beginner, ok := database.(sqlBeginner); ok {
		tx, err := beginner.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return nil, nil, nil, fmt.Errorf("begin jit-off read transaction: %w", err)
		}
		rows := legacyAggregateQueryer{database: tx}
		if err := setJITOff(ctx, rows); err != nil {
			_ = tx.Rollback()
			return nil, nil, nil, err
		}
		return rows, rows, func() {
			if err := tx.Commit(); err != nil {
				_ = tx.Rollback()
			}
		}, nil
	}
	return plainRows, plainRow, func() {}, nil
}

func setJITOff(ctx context.Context, tx db.RowQueryer) error {
	var applied string
	if err := tx.QueryRowContext(ctx, jitOffSQL).Scan(&applied); err != nil {
		return fmt.Errorf("set jit off for the read transaction: %w", err)
	}
	return nil
}
