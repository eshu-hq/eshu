// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// infraReadModelState is what the run can observe about the Postgres infra
// read model (#6793's infra_resource_entities). Installed is false before that
// read model exists (the graph path serves the routes), so the gate works on
// both sides of it without importing its package.
type infraReadModelState struct {
	Installed     bool
	MarkerPresent bool
	DirtyRepos    int
}

// ready reports whether the sweep may start, and why not when it may not.
// Readers trust the read model only once the backfill marker exists and no
// repository is marked dirty; until then they stay on the graph, and a sweep
// would measure the old path while looking like a measurement of the new one.
func (s infraReadModelState) ready() (bool, string) {
	switch {
	case !s.Installed:
		return true, "infra read model not installed (routes are served from the graph)"
	case !s.MarkerPresent:
		return false, "infra read model installed but the backfill marker is not recorded yet"
	case s.DirtyRepos > 0:
		return false, fmt.Sprintf("infra read model has %d repositories marked dirty (readers stay on the graph)", s.DirtyRepos)
	}
	return true, "infra read model ready (backfill marker recorded, no dirty repositories)"
}

// waitForInfraReadModel polls read until the read model is ready, logging what
// it sees, and fails with the last reason when timeout passes: a fence that
// never clears is a finding, not something to sweep through.
func waitForInfraReadModel(ctx context.Context, read func(context.Context) (infraReadModelState, error), timeout, poll time.Duration, log io.Writer) error {
	deadline := time.Now().Add(timeout)
	var lastReason string
	for {
		state, err := read(ctx)
		if err != nil {
			return fmt.Errorf("read infra read model state: %w", err)
		}
		ready, reason := state.ready()
		if reason != lastReason {
			_, _ = fmt.Fprintf(log, "read-api-latency-gate: %s\n", reason)
			lastReason = reason
		}
		if ready {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("infra read model not ready after %s: %s", timeout, reason)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

// readInfraReadModelState reads the read model's install/backfill/fence state
// from Postgres. The tables are looked up with to_regclass, so this is safe on
// a database where migration 109 has not been applied.
func readInfraReadModelState(ctx context.Context, pool *pgxpool.Pool) (infraReadModelState, error) {
	var state infraReadModelState
	if err := pool.QueryRow(ctx, "SELECT to_regclass('infra_resource_entity_backfill_markers') IS NOT NULL AND to_regclass('infra_resource_entity_dirty_repos') IS NOT NULL").Scan(&state.Installed); err != nil {
		return infraReadModelState{}, fmt.Errorf("check infra read model tables: %w", err)
	}
	if !state.Installed {
		return state, nil
	}
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM infra_resource_entity_backfill_markers)").Scan(&state.MarkerPresent); err != nil {
		return infraReadModelState{}, fmt.Errorf("check infra backfill marker: %w", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM infra_resource_entity_dirty_repos").Scan(&state.DirtyRepos); err != nil {
		return infraReadModelState{}, fmt.Errorf("count dirty repositories: %w", err)
	}
	return state, nil
}

// analyzeInfraReadModel refreshes planner statistics on the derived table once
// the backfill has filled it: 1M rows just arrived in one pass, and production
// has autovacuum's ANALYZE by the time reads matter. A no-op when the table does
// not exist.
func analyzeInfraReadModel(ctx context.Context, pool *pgxpool.Pool) error {
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('infra_resource_entities') IS NOT NULL").Scan(&exists); err != nil {
		return fmt.Errorf("check infra_resource_entities: %w", err)
	}
	if !exists {
		return nil
	}
	if _, err := pool.Exec(ctx, "ANALYZE infra_resource_entities"); err != nil {
		return fmt.Errorf("analyze infra_resource_entities: %w", err)
	}
	return nil
}

// infraReadModelTimeout bounds how long the sweep waits for the API's startup
// backfill to derive the seeded content and clear the fence.
const infraReadModelTimeout = 10 * time.Minute

// awaitInfraReadModel opens a short-lived pool, waits until the infra read model
// is ready (a no-op where it is not installed), and refreshes its statistics. It
// reports whether the read model is installed, so the caller can also assert the
// API actually serves from it.
func awaitInfraReadModel(ctx context.Context, dsn string, log io.Writer) (bool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return false, fmt.Errorf("connect postgres for the infra read model check: %w", err)
	}
	defer pool.Close()

	read := func(ctx context.Context) (infraReadModelState, error) { return readInfraReadModelState(ctx, pool) }
	if err := waitForInfraReadModel(ctx, read, infraReadModelTimeout, 2*time.Second, log); err != nil {
		return false, err
	}
	state, err := read(ctx)
	if err != nil {
		return false, err
	}
	if err := analyzeInfraReadModel(ctx, pool); err != nil {
		return false, err
	}
	return state.Installed, nil
}
