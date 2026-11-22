// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/infra"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// infraInventoryBackfillRunnerFor builds the runner that records the infra
// read model backfill marker (#11196). The reducer owns it because the API
// path that used to run it was removed (a012da4557) and the reducer already
// hosts the read model's reconcile loop. There is no disable switch: the
// marker is what lets readers leave the graph path, and every replica may run
// it because the per-repository derive lock and the idempotent marker insert
// make concurrent passes safe (docs/internal/design/11196-infra-backfill-reducer-owner.md).
func infraInventoryBackfillRunnerFor(
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) *infra.BackfillRunner {
	return &infra.BackfillRunner{
		Backfiller: postgresInfraInventoryBackfiller{database: database, instruments: instruments, logger: logger},
		Logger:     logger,
	}
}

// postgresInfraInventoryBackfiller adapts inventory.RunBackground to the
// maintenance runner's port. RunBackground retries a failed attempt with a
// doubling backoff and counts every attempt in
// eshu_dp_infra_inventory_backfill_runs_total.
type postgresInfraInventoryBackfiller struct {
	database    db.ExecQueryer
	instruments *telemetry.Instruments
	logger      *slog.Logger
}

// BackfillInfraInventory runs the backfill until the marker exists or ctx ends.
func (b postgresInfraInventoryBackfiller) BackfillInfraInventory(ctx context.Context) error {
	return inventory.RunBackground(ctx, b.database, inventory.BackgroundOptions{
		Logger:      b.logger,
		Instruments: b.instruments,
	})
}
