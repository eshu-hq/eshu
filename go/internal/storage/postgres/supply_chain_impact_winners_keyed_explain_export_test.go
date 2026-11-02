// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ExplainWinnersKeyedForTest runs one keyed tick's statements inside a
// transaction it rolls back, and returns the EXPLAIN (ANALYZE, BUFFERS) plan of
// the active_facts build and of the apply statement. The dirty log and the
// winners table are unchanged afterwards, so the scale proof can attach the plans
// of the exact tick it then times.
func ExplainWinnersKeyedForTest(ctx context.Context, database *sql.DB, materializedAt any) (activeFactsPlan, applyPlan string, err error) {
	tx, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		return "", "", fmt.Errorf("begin explain transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if claimed, err := claimSupplyChainImpactWinnersResweep(ctx, tx); err != nil || !claimed {
		return "", "", fmt.Errorf("claim resweep lock for explain: claimed=%v err=%v", claimed, err)
	}
	if _, err := consumeWinnersDirtyLog(ctx, tx); err != nil {
		return "", "", err
	}
	for _, stmt := range []string{buildWinnersKeyedIdentsSQL, analyzeWinnersKeyedIdentsSQL} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return "", "", fmt.Errorf("build identifiers for explain: %w", err)
		}
	}
	explain := func(stmt string, args ...any) (string, error) {
		rows, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+stmt, args...)
		if err != nil {
			return "", err
		}
		defer func() { _ = rows.Close() }()
		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return "", err
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n"), rows.Err()
	}
	if activeFactsPlan, err = explain(buildWinnersKeyedActiveFactsSQL); err != nil {
		return "", "", fmt.Errorf("explain keyed active_facts build: %w", err)
	}
	for _, stmt := range []string{addWinnersKeyedOperatorFactsSQL, analyzeWinnersKeyedActiveFactsSQL} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return "", "", fmt.Errorf("finish keyed active_facts for explain: %w", err)
		}
	}
	if applyPlan, err = explain(applyWinnersKeyedSQL, materializedAt); err != nil {
		return "", "", fmt.Errorf("explain keyed apply: %w", err)
	}
	return activeFactsPlan, applyPlan, nil
}
