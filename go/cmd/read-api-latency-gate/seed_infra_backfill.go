// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	pgstore "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// infraBackfillStandIn is the value the latency report's identity block
// carries in infra_backfill, and the label the seed log line uses. It says the
// infra read model marker this run measured behind was recorded by the gate
// itself, not by a product runtime.
const infraBackfillStandIn = "gate_stand_in"

// infraBackfiller is the part of inventory.Backfiller the stand-in needs.
type infraBackfiller interface {
	Run(ctx context.Context) (inventory.BackfillResult, error)
}

// runInfraBackfillStandIn runs the product's infra read model backfill and
// proves it left the read model ready: installed, marker recorded, and no
// repository marked dirty. It is the seed-phase stand-in for the backfill the
// removed in-tree API used to start (a012da4557): no product runtime runs it
// today (#11196), so without this step the gate's infra read model fence
// never clears and awaitInfraReadModel times out.
//
// It calls the product Backfiller rather than writing the marker or copying
// the derive SQL, so the measured read model is the one the product derives.
// awaitInfraReadModel and assertInfraServedFromReadModel stay unchanged as the
// guard that checks the same outcome again from outside, once the core serves.
func runInfraBackfillStandIn(
	ctx context.Context,
	backfiller infraBackfiller,
	readState func(context.Context) (infraReadModelState, error),
	log io.Writer,
) error {
	_, _ = fmt.Fprintf(log, "read-api-latency-gate: infra_backfill=%s: running inventory.Backfiller after the seed because no product runtime runs it (the in-tree API that did was removed in a012da4557; product issue #11196); infra route numbers describe an install that holds the marker\n",
		infraBackfillStandIn)
	result, err := backfiller.Run(ctx)
	if err != nil {
		return fmt.Errorf("run infra read model backfill: %w", err)
	}
	state, err := readState(ctx)
	if err != nil {
		return fmt.Errorf("read infra read model state after the backfill: %w", err)
	}
	if !state.Installed {
		return fmt.Errorf("infra read model is not installed after the backfill: the seeded database lacks the read model tables")
	}
	if ready, reason := state.ready(); !ready {
		return fmt.Errorf("infra read model not ready after the backfill: %s", reason)
	}
	_, _ = fmt.Fprintf(log, "read-api-latency-gate: infra read model backfilled (repositories=%d rows_inserted=%d already_complete=%t duration=%s)\n",
		result.Repositories, result.RowsInserted, result.AlreadyComplete, result.Duration)
	return nil
}

// backfillSeededInfraReadModel wires runInfraBackfillStandIn to the seeded
// Postgres: the product Backfiller over a database/sql handle (it takes
// db.ExecQueryer, not pgxpool), and the gate's own state reader over pool. The
// handle closes before the graph seed so no extra connection lingers through
// the sweep.
func backfillSeededInfraReadModel(ctx context.Context, postgresDSN string, pool *pgxpool.Pool, log io.Writer) error {
	db, err := sql.Open("pgx", postgresDSN)
	if err != nil {
		return fmt.Errorf("open postgres for the infra read model backfill: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres for the infra read model backfill: %w", err)
	}
	backfiller := inventory.Backfiller{
		DB:     pgstore.SQLDB{DB: db},
		Logger: slog.New(slog.NewTextHandler(log, nil)),
	}
	return runInfraBackfillStandIn(ctx, backfiller,
		func(ctx context.Context) (infraReadModelState, error) { return readInfraReadModelState(ctx, pool) }, log)
}
