// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// isLockTimeoutError reports SQLSTATE 55P03, the lock-timeout signal the
// 1s LOCK TABLE wait and the advisory wait raise instead of waiting out
// the deadline.
func isLockTimeoutError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "55P03"
}

// lockTimeoutSetting renders a duration as a Postgres lock_timeout value.
func lockTimeoutSetting(d time.Duration) string {
	if d <= 0 {
		return "0"
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

// retryJitter sleeps a jittered 100 to 500 ms between fence restarts,
// salted by the clock so concurrent fencers do not retry in lockstep.
func retryJitter(opts FenceOptions) time.Duration {
	span := int64(opts.RetryMaxSleep - opts.RetryMinSleep)
	if span <= 0 {
		return opts.RetryMinSleep
	}
	return opts.RetryMinSleep + time.Duration(opts.Now().UnixNano()%(span+1))
}

// generationPair is one non-superseded (scope_id, generation_id) pair the
// step-4 re-read materializes for the tuned 7c binding.
type generationPair struct {
	scopeID      string
	generationID string
}

// generationPairs carries the materialized step-4 set.
type generationPairs []generationPair

func (p generationPairs) scopeIDs() []string {
	ids := make([]string, 0, len(p))
	for _, pair := range p {
		ids = append(ids, pair.scopeID)
	}
	return ids
}

func (p generationPairs) generationIDs() []string {
	ids := make([]string, 0, len(p))
	for _, pair := range p {
		ids = append(ids, pair.generationID)
	}
	return ids
}

// scanResolveScopes scans (scope_id, partition_key) resolution rows into
// ordered scope ids.
func scanResolveScopes(rows db.Rows) ([]string, error) {
	defer func() { _ = rows.Close() }()
	var scopeIDs []string
	for rows.Next() {
		var scopeID, partitionKey string
		if err := rows.Scan(&scopeID, &partitionKey); err != nil {
			return nil, fmt.Errorf("retirement fence resolve scopes scan: %w", err)
		}
		scopeIDs = append(scopeIDs, scopeID)
	}
	return scopeIDs, rows.Err()
}

// scanRepoCounts runs a (partition_key, count) precheck query and scans
// the per-repository counts.
func scanRepoCounts(ctx context.Context, queryer db.Queryer, query string, repoIDs []string) (map[string]int64, error) {
	rows, err := queryer.QueryContext(ctx, query, repoIDs)
	if err != nil {
		return nil, fmt.Errorf("retirement fence precheck: %w", err)
	}
	defer func() { _ = rows.Close() }()
	counts := make(map[string]int64)
	for rows.Next() {
		var repoID string
		var count int64
		if err := rows.Scan(&repoID, &count); err != nil {
			return nil, fmt.Errorf("retirement fence precheck scan: %w", err)
		}
		counts[repoID] = count
	}
	return counts, rows.Err()
}

// scanScopeIDs scans ordered scope ids.
func scanScopeIDs(ctx context.Context, tx db.Transaction, query string, repoIDs []string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, repoIDs)
	if err != nil {
		return nil, fmt.Errorf("retirement fence lock scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var scopeIDs []string
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return nil, fmt.Errorf("retirement fence lock scopes scan: %w", err)
		}
		scopeIDs = append(scopeIDs, scopeID)
	}
	return scopeIDs, rows.Err()
}

// scanGenerationPairs materializes the step-4 non-superseded pair set.
func scanGenerationPairs(ctx context.Context, tx db.Transaction, query string, scopeIDs []string) (generationPairs, error) {
	rows, err := tx.QueryContext(ctx, query, scopeIDs)
	if err != nil {
		return nil, fmt.Errorf("retirement fence read generations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var pairs generationPairs
	for rows.Next() {
		var pair generationPair
		if err := rows.Scan(&pair.scopeID, &pair.generationID); err != nil {
			return nil, fmt.Errorf("retirement fence read generations scan: %w", err)
		}
		pairs = append(pairs, pair)
	}
	return pairs, rows.Err()
}

// scanUnderLockBudgets recounts generations and 7d-predicate reducer rows
// per repository over the locked scopes, so an under-lock
// request_too_large refusal carries the same PerRepo breakdown the
// precheck refusal promises. Both sums cover exactly the locked scope
// set the refusal reports on.
func scanUnderLockBudgets(
	ctx context.Context,
	tx db.Transaction,
	scopeIDs []string,
) (map[string]FenceRepoBudget, int64, int64, error) {
	budgets := make(map[string]FenceRepoBudget)
	var generations, reducerRows int64
	for _, query := range []struct {
		text    string
		reducer bool
	}{
		{text: fenceUnderLockGenerationsQuery},
		{text: fenceUnderLockReducerRowsQuery, reducer: true},
	} {
		rows, err := tx.QueryContext(ctx, query.text, scopeIDs)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("retirement fence under-lock budget: %w", err)
		}
		for rows.Next() {
			var repoID string
			var count int64
			if err := rows.Scan(&repoID, &count); err != nil {
				_ = rows.Close()
				return nil, 0, 0, fmt.Errorf("retirement fence under-lock budget scan: %w", err)
			}
			budget := budgets[repoID]
			if query.reducer {
				budget.ReducerRows = count
				reducerRows += count
			} else {
				budget.Generations = count
				generations += count
			}
			budgets[repoID] = budget
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, 0, 0, fmt.Errorf("retirement fence under-lock budget: %w", err)
		}
	}
	return budgets, generations, reducerRows, nil
}

// scanSingleCount scans one count row.
func scanSingleCount(ctx context.Context, tx db.Transaction, query string, scopeIDs []string) (int64, error) {
	rows, err := tx.QueryContext(ctx, query, scopeIDs)
	if err != nil {
		return 0, fmt.Errorf("retirement fence count: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("retirement fence count read: %w", err)
		}
		return 0, fmt.Errorf("retirement fence count: no row returned")
	}
	var count int64
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("retirement fence count scan: %w", err)
	}
	return count, rows.Err()
}

// hasRows reports whether the bounded existence query matches.
func hasRows(ctx context.Context, queryer db.Queryer, query string, scopeIDs []string) (bool, error) {
	rows, err := queryer.QueryContext(ctx, query, scopeIDs)
	if err != nil {
		return false, fmt.Errorf("retirement fence drain: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	return true, rows.Err()
}

// hasTxRows reports whether the bounded existence query matches inside
// the fence transaction.
func hasTxRows(ctx context.Context, tx db.Transaction, query string, scopeIDs []string) (bool, error) {
	rows, err := tx.QueryContext(ctx, query, scopeIDs)
	if err != nil {
		return false, fmt.Errorf("retirement fence recheck: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	return true, rows.Err()
}

// execCount executes a fence write and reports affected rows.
func execCount(ctx context.Context, tx db.Transaction, query string, args ...any) (int64, error) {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("retirement fence rows affected: %w", err)
	}
	return affected, nil
}
