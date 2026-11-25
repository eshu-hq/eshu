// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
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
	// The tick collapses the staging queue before admission, so the plans
	// below are drawn over the collapsed window, not an empty log.
	if _, err := collapseWinnersStaging(ctx, tx, 0); err != nil {
		return "", "", fmt.Errorf("collapse staging for explain: %w", err)
	}
	if _, err := admitWinnersDirtyWindow(ctx, tx, defaultWinnersChunkMaxDirtyRows); err != nil {
		return "", "", err
	}
	for _, stmt := range winnersKeyedBuildIdentsStmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return "", "", fmt.Errorf("build identifiers for explain: %w", err)
		}
	}
	// The tick persists the probe to the narrow table before the
	// active-facts build reads it; mirror runKeyedOrFull or the EXPLAIN
	// below errors on the missing relation.
	if _, err := tx.ExecContext(ctx, buildWinnersKeyedNarrowSQL); err != nil {
		return "", "", fmt.Errorf("build narrow probe for explain: %w", err)
	}
	if _, err := tx.ExecContext(ctx, analyzeWinnersKeyedNarrowSQL); err != nil {
		return "", "", fmt.Errorf("analyze narrow probe for explain: %w", err)
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

// ExplainWinnersKeyedSettleForTest runs the keyed tick's setup (admit window,
// identifier expansion, chunk) inside a transaction it rolls back, and returns
// the EXPLAIN plans of the scope-remainder convert SELECT and the
// applied-delete statement: the two consumers of the per-tick scope expansion
// the capacity lane (#10839) bounds. Plain EXPLAIN executes nothing, so the
// dirty log and the winners table are unchanged afterwards. The setup runs the
// production statements (including the tick's temp indexes), so the plans are
// the plans the tick gets.
func ExplainWinnersKeyedSettleForTest(ctx context.Context, database *sql.DB) (convertPlan, deletePlan, fallbackPlan string, err error) {
	tx, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		return "", "", "", fmt.Errorf("begin settle explain transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if claimed, err := claimSupplyChainImpactWinnersResweep(ctx, tx); err != nil || !claimed {
		return "", "", "", fmt.Errorf("claim resweep lock for settle explain: claimed=%v err=%v", claimed, err)
	}
	// The tick collapses the staging queue before admission, so the plans
	// below are drawn over the collapsed window, not an empty log.
	if _, err := collapseWinnersStaging(ctx, tx, 0); err != nil {
		return "", "", "", fmt.Errorf("collapse staging for settle explain: %w", err)
	}
	if _, err := admitWinnersDirtyWindow(ctx, tx, defaultWinnersChunkMaxDirtyRows); err != nil {
		return "", "", "", fmt.Errorf("admit window for settle explain: %w", err)
	}
	for _, stmt := range winnersKeyedBuildIdentsStmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return "", "", "", fmt.Errorf("build identifiers for settle explain: %w", err)
		}
	}
	var empty SupplyChainImpactWinnersStore
	if _, err := buildWinnersKeyedChunk(ctx, tx, empty.chunkBounds()); err != nil {
		return "", "", "", fmt.Errorf("build chunk for settle explain: %w", err)
	}
	explain := func(stmt string) (string, error) {
		rows, err := tx.QueryContext(ctx, "EXPLAIN "+stmt)
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
	if convertPlan, err = explain(convertWinnersKeyedScopeRemainderSelectSQL); err != nil {
		return "", "", "", fmt.Errorf("explain keyed scope remainder convert: %w", err)
	}
	if deletePlan, err = explain(deleteWinnersKeyedAppliedDirtySQL); err != nil {
		return "", "", "", fmt.Errorf("explain keyed applied delete: %w", err)
	}
	if fallbackPlan, err = explain(deleteWinnersKeyedAllDirtySQL); err != nil {
		return "", "", "", fmt.Errorf("explain keyed fallback delete: %w", err)
	}
	return convertPlan, deletePlan, fallbackPlan, nil
}

// ExplainWinnersKeyedConvertSlowWindowForTest runs the keyed tick's setup
// (admit window, identifier expansion, a chunk of chunkIdents identifiers) in
// a transaction it rolls back, and returns the EXPLAIN (ANALYZE, BUFFERS)
// plans of the scope-remainder convert SELECT and the applied delete: the two
// consumers of the per-tick scope expansion the capacity lane (#10839)
// bounds. The convert arm is read-only, so ANALYZE executes nothing visible;
// the delete runs its data-modifying CTEs, which the rollback discards on the
// disposable harness database. The setup runs the production statements
// (including the tick's temp indexes) with a caller-chosen chunk size, so the
// plans are the plans the tick gets at the seeded alignment.
func ExplainWinnersKeyedConvertSlowWindowForTest(ctx context.Context, database *sql.DB, chunkIdents int64) (convertPlan, deletePlan string, err error) {
	tx, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		return "", "", fmt.Errorf("begin slow-window explain transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if claimed, err := claimSupplyChainImpactWinnersResweep(ctx, tx); err != nil || !claimed {
		return "", "", fmt.Errorf("claim resweep lock for slow-window explain: claimed=%v err=%v", claimed, err)
	}
	// The tick collapses the staging queue before admission, so the plans
	// below are drawn over the collapsed window, not an empty log.
	if _, err := collapseWinnersStaging(ctx, tx, 0); err != nil {
		return "", "", fmt.Errorf("collapse staging for slow-window explain: %w", err)
	}
	if _, err := admitWinnersDirtyWindow(ctx, tx, defaultWinnersChunkMaxDirtyRows); err != nil {
		return "", "", err
	}
	for _, stmt := range winnersKeyedBuildIdentsStmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return "", "", fmt.Errorf("build identifiers for slow-window explain: %w", err)
		}
	}
	// Explain-only: admit the whole caller-chosen chunk regardless of the
	// scanned-copies budget, so the plans below cover the seeded alignment
	// the caller asked for.
	if _, err := buildWinnersKeyedChunk(ctx, tx, winnersChunkBounds{
		dirtyRows:     defaultWinnersChunkMaxDirtyRows,
		identifiers:   chunkIdents,
		scannedCopies: 1 << 62,
	}); err != nil {
		return "", "", fmt.Errorf("build chunk for slow-window explain: %w", err)
	}
	explainAnalyze := func(stmt string) (string, error) {
		rows, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+stmt)
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
	if convertPlan, err = explainAnalyze(convertWinnersKeyedScopeRemainderSelectSQL); err != nil {
		return "", "", fmt.Errorf("explain keyed scope remainder convert: %w", err)
	}
	if deletePlan, err = explainAnalyze(deleteWinnersKeyedAppliedDirtySQL); err != nil {
		return "", "", fmt.Errorf("explain keyed applied delete: %w", err)
	}
	return convertPlan, deletePlan, nil
}

// TestConvertWinnersKeyedScopeRemainderSelectUsesMaterializedChunkApplied pins
// the fixed-cost convert shape (#10839): the scope-applied test is a
// once-per-tick MATERIALIZED set probed by (scope_id, generation_id), never a
// correlated EXISTS that re-probes the chunk per expansion row. The shim
// measured this marker as the contract: without it the planner inlines the
// set and can flip back to the 24.8M-probe nested-loop shape. The old shape
// has no chunk_applied reference at all, so this fails on the pre-fix tree.
func TestConvertWinnersKeyedScopeRemainderSelectUsesMaterializedChunkApplied(t *testing.T) {
	arm := convertWinnersKeyedScopeRemainderSelectSQL
	if !strings.Contains(arm, "WITH chunk_applied AS MATERIALIZED") {
		t.Error("convert arm does not precompute the chunk-applied scope set once per tick: the correlated per-row chunk re-probe is back")
	}
	if !strings.Contains(arm, "FROM chunk_applied AS ca") {
		t.Error("convert arm does not probe the once-per-tick chunk-applied set: the correlated per-row chunk re-probe is back")
	}
	if strings.Contains(arm, "AS applied") {
		t.Error("convert arm still carries the correlated applied-scope EXISTS: the per-expansion-row chunk re-probe is back")
	}
}
