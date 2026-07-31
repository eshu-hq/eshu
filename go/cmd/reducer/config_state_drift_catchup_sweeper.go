// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// startConfigStateDriftCatchUpSweeper launches the config_state_drift
// catch-up sweep (issue #5593 P1-1) as a best-effort background loop bound to
// ctx, mirroring startSearchDocumentSweeper's shape exactly. It periodically
// re-enqueues one config_state_drift reducer intent per active
// state_snapshot:* scope, converging any generation the ingester's runtime
// delta-trigger (ProjectorQueue.ConfigStateDriftTrigger) failed to enqueue --
// a best-effort, un-retried call by design -- within a small bounded
// interval instead of waiting for a new terraform apply or an
// operator-initiated bootstrap-index re-run. See
// projector.ConfigStateDriftCatchUpSweeper's doc comment for why this is safe
// to run forever and is NOT the redrive mechanism issue #5593's review
// history removed.
//
// Enqueue is idempotent (ON CONFLICT DO NOTHING by domain+scope_id+
// generation_id), so the sweeper needs no lease and concurrent reducer
// replicas converge on the same inserts.
func startConfigStateDriftCatchUpSweeper(ctx context.Context, db *sql.DB, instruments *telemetry.Instruments, logger *slog.Logger) {
	execQ := postgres.SQLDB{DB: db}
	store := postgres.NewIngestionStore(execQ)
	sweeper := projector.ConfigStateDriftCatchUpSweeper{
		Active:      store,
		Intents:     postgres.NewReducerQueue(execQ, "reducer-config-state-drift-catchup-sweeper", time.Minute),
		Instruments: instruments,
		Logger:      logger,
	}
	go func() {
		if err := sweeper.Run(ctx); err != nil && ctx.Err() == nil && logger != nil {
			logger.Error("config state drift catch-up sweeper stopped", "error", err)
		}
	}()
}
