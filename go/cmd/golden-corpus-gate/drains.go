// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib driver for database/sql

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// SQL for the B-7(a) drain gate. The status set and completed_at semantics match
// the reducer/projector queue contract (see go/internal/storage/postgres):
//
//   - fact_work_items residual: any row not in a clean terminal status. A
//     'dead_letter' or 'failed' row counts as residual on purpose — a drained
//     pipeline has no dead letters.
//   - shared_projection_intents nonterminal: completed_at IS NULL. Per B-13
//     (#3859), the repo_dependency domain is the primary gate, so its subset is
//     reported separately.
const (
	factWorkItemsResidualSQL = `
SELECT count(*) FROM fact_work_items
WHERE status NOT IN ('succeeded', 'superseded')`

	sharedIntentsNonterminalSQL = `
SELECT count(*) FROM shared_projection_intents
WHERE completed_at IS NULL`

	repoDependencyNonterminalSQL = `
SELECT count(*) FROM shared_projection_intents
WHERE completed_at IS NULL AND projection_domain = 'repo_dependency'`
)

// drainQuerier reads the current queue residuals. Defined here where it is
// consumed so tests can fake it without a database.
type drainQuerier interface {
	Counts(ctx context.Context) (DrainCounts, error)
}

// sqlDrainQuerier reads drain counts from Postgres.
type sqlDrainQuerier struct {
	db *sql.DB
}

// openDrainQuerier opens a Postgres connection from the environment using the
// same loader the services under test use.
func openDrainQuerier(ctx context.Context, getenv func(string) string) (*sqlDrainQuerier, func(), error) {
	db, err := runtimecfg.OpenPostgres(ctx, getenv)
	if err != nil {
		return nil, nil, fmt.Errorf("open postgres: %w", err)
	}
	return &sqlDrainQuerier{db: db}, func() { _ = db.Close() }, nil
}

func (q *sqlDrainQuerier) scalar(ctx context.Context, query string) (int64, error) {
	var n int64
	if err := q.db.QueryRowContext(ctx, query).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (q *sqlDrainQuerier) Counts(ctx context.Context) (DrainCounts, error) {
	fact, err := q.scalar(ctx, factWorkItemsResidualSQL)
	if err != nil {
		return DrainCounts{}, fmt.Errorf("fact_work_items residual: %w", err)
	}
	intents, err := q.scalar(ctx, sharedIntentsNonterminalSQL)
	if err != nil {
		return DrainCounts{}, fmt.Errorf("shared_projection_intents nonterminal: %w", err)
	}
	repoDep, err := q.scalar(ctx, repoDependencyNonterminalSQL)
	if err != nil {
		return DrainCounts{}, fmt.Errorf("repo_dependency nonterminal: %w", err)
	}
	return DrainCounts{
		FactWorkItemsResidual:     fact,
		SharedIntentsNonterminal:  intents,
		RepoDependencyNonterminal: repoDep,
	}, nil
}

// pollUntilDrained polls q until both queues are within the snapshot bounds or
// the context/timeout expires. It always returns the last observed counts so the
// caller can report the residual even on a timeout. drained reports whether the
// drain bounds were met.
func pollUntilDrained(
	ctx context.Context,
	q drainQuerier,
	a DrainAssertions,
	timeout, poll time.Duration,
) (counts DrainCounts, drained bool, err error) {
	deadline := time.Now().Add(timeout)
	for {
		counts, err = q.Counts(ctx)
		if err != nil {
			return counts, false, err
		}
		if counts.Drained(a) {
			return counts, true, nil
		}
		if !time.Now().Before(deadline) {
			return counts, false, nil
		}
		select {
		case <-ctx.Done():
			return counts, false, ctx.Err()
		case <-time.After(poll):
		}
	}
}
