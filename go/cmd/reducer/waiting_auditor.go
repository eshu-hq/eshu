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

// startReducerWaitingAuditor launches the periodic waiting-set-aside audit
// (#10876) as a best-effort background loop bound to ctx, mirroring
// startReducerStaleGenerationSweeper's shape. The audit reopens parked waits
// whose provider already committed, reconciles wait rows whose queue row
// moved on through another path, and records the per-reason waiting gauges;
// parked rows stay correct through their 10-minute backstop when the audit
// is stopped or slow, so a stopped auditor never blocks or corrupts
// claiming. The pass is bounded and idempotent, so every replica runs one
// without a lease; the batch SELECT's SKIP LOCKED locks release at
// statement end (autocommit), so overlapping passes stay benign.
func startReducerWaitingAuditor(ctx context.Context, db *sql.DB, instruments *telemetry.Instruments, logger *slog.Logger) {
	queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "reducer-waiting-auditor", time.Minute)
	queue.Instruments = instruments
	auditor := postgres.ReducerWaitingAuditor{Queue: queue, Logger: logger}
	go func() {
		if err := auditor.Run(ctx); err != nil && ctx.Err() == nil && logger != nil {
			logger.Error("reducer waiting auditor stopped", "error", err)
		}
	}()
}
