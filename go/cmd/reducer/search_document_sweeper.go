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
)

// startSearchDocumentSweeper launches the curated search-document projection
// sweeper (design 430) as a best-effort background loop bound to ctx. It
// periodically enqueues DomainEshuSearchDocument intents for repository
// generations that have indexed content but no projection yet. Enqueue is
// idempotent (ON CONFLICT DO NOTHING by scope+generation+domain), so the sweeper
// needs no lease and concurrent reducers converge on the same inserts.
func startSearchDocumentSweeper(ctx context.Context, db *sql.DB, logger *slog.Logger) {
	execQ := postgres.SQLDB{DB: db}
	sweeper := projector.SearchDocumentProjectionSweeper{
		Pending: postgres.NewEshuSearchDocumentPendingStore(execQ),
		Intents: postgres.NewReducerQueue(execQ, "reducer-search-document-sweeper", time.Minute),
		Logger:  logger,
	}
	go func() {
		if err := sweeper.Run(ctx); err != nil && ctx.Err() == nil && logger != nil {
			logger.Error("eshu search document sweeper stopped", "error", err)
		}
	}()
}
