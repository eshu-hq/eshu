// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// startReducerStaleGenerationSweeper launches the periodic
// stale-generation sweep (#10863) as a best-effort background loop bound
// to ctx, mirroring startSearchDocumentSweeper's shape. The sweep
// terminalizes unleased older-generation reducer rows out of band; the
// claims that used to run it inline now only fence those rows read-only
// at hand-out, so a stopped or slow sweeper never blocks or corrupts
// claiming — stale rows simply stay pending (and fenced) until the next
// pass. The statement is bounded and SKIP LOCKED, so every replica runs
// one without a lease.
func startReducerStaleGenerationSweeper(ctx context.Context, db *sql.DB, instruments *telemetry.Instruments, logger *slog.Logger) {
	queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "reducer-stale-generation-sweeper", time.Minute)
	queue.Instruments = instruments
	sweeper := postgres.ReducerStaleGenerationSweeper{Queue: queue, Logger: logger}
	go func() {
		if err := sweeper.Run(ctx); err != nil && ctx.Err() == nil && logger != nil {
			logger.Error("reducer stale-generation sweeper stopped", "error", err)
		}
	}()
}
